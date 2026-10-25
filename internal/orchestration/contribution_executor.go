package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/registry"
	hermesruntime "github.com/berryhill/aegis/internal/runtime/hermes"
)

// ContributionRole is a separately issued session/mandate, never a substituted
// root Queue runtime binding. Only the owning application supplies this port.
type ContributionRole struct {
	Agent     registry.AgentRevision
	Launch    execution.LaunchContract
	Admission execution.AdmissionChecker
	Close     func(context.Context) error
}
type contributionAdmissionFunc func(context.Context, execution.LaunchContract, time.Time) (execution.AdmissionDecision, error)

func (f contributionAdmissionFunc) CheckRuntimeAdmission(ctx context.Context, launch execution.LaunchContract, at time.Time) (execution.AdmissionDecision, error) {
	return f(ctx, launch, at)
}

type ContributionRoleResolver func(context.Context, core.Subject, contribution.Contract, int) (ContributionRole, error)

func (w *QueueWorker) SetContributionRoleResolver(r ContributionRoleResolver) {
	w.contributionRoles = r
}

// Candidate acceptance is an intermediate product result. #378 consumes this
// chain; only #379 may join it to whole-contribution success/disposition.
var ErrContributionCandidatePending = errors.New("contribution candidate retained; delivery and final disposition not executed")

func (w *QueueWorker) processContribution(ctx context.Context, request WorkRequest, base WorkResult, runtime RuntimeRequest) (result WorkResult, resultErr error) {
	contract, sealed, err := w.contributionContract(ctx, request.QueueItemID, runtime.LoopRevision)
	if err != nil {
		return base, err
	}
	custody, ok := w.repository.(interface{ ImplementationStore() implementation.Store })
	if !ok || w.contributionRoles == nil || w.implementation == nil {
		return base, errors.New("managed contribution custody and role admission required")
	}
	ctx, cancel := context.WithDeadline(ctx, sealed.Deadline)
	defer cancel()
	var builder, active *ContributionRole
	roles := []*ContributionRole{}
	defer func() {
		for i := len(roles) - 1; i >= 0; i-- {
			if roles[i].Close != nil {
				stop, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				resultErr = errors.Join(resultErr, roles[i].Close(stop))
				cancel()
			}
		}
	}()
	admit := func(c context.Context, effect string) error {
		if _, _, err := w.contributionContract(c, request.QueueItemID, runtime.LoopRevision); err != nil {
			return err
		}
		projection, err := w.repository.GetQueueProjection(c, request.QueueItemID)
		if err != nil || projection.State != queue.StateClaimed || projection.ActiveClaimID != base.Claim.ClaimID {
			return errors.New("exact active contribution claim required")
		}
		controller, controllerAdmission := runtime.Launch, runtime.Admission
		if w.contributionCurrent != nil {
			current, admission, e := w.contributionCurrent(c, request.Subject, sealed, base.Claim)
			if !errors.Is(e, ErrNoContributionContinuation) {
				if e != nil || admission == nil {
					return ErrWorkerDenied
				}
				controller, controllerAdmission = current, admission
			} else if runtime.Launch.AuthorityContext.ID != base.Claim.Authority.ID || !w.now().Before(base.Claim.ExpiresAt) {
				return ErrWorkerDenied
			}
		} else if runtime.Launch.AuthorityContext.ID != base.Claim.Authority.ID || !w.now().Before(base.Claim.ExpiresAt) {
			if w.contributionCurrent == nil {
				return ErrWorkerDenied
			}
			current, _, e := w.contributionCurrent(c, request.Subject, sealed, base.Claim)
			if e != nil || current.AuthorityContext.Digest != runtime.Launch.AuthorityContext.Digest {
				return ErrWorkerDenied
			}
		}
		if err = w.authorizeImplementationRevision(runtime.LoopRevision, runtime.Participant); err != nil {
			return err
		}
		// Controller and child are independent conjunctive gates, never a union.
		if active != nil && active != builder && strings.HasPrefix(effect, "write:") {
			return errors.New("tester source edits denied")
		}
		return admitContributionEffects(c, controller, controllerAdmission, active, w.now())
	}
	var observer *ContributionExecutionObserver
	resolve := func(c context.Context, index int) (*ContributionRole, error) {
		role, err := w.contributionRoles(c, request.Subject, sealed, index)
		if err != nil {
			return nil, err
		}
		if err = validateContributionRole(role, sealed, index, w.now()); err != nil {
			if role.Close != nil {
				err = errors.Join(err, role.Close(context.WithoutCancel(c)))
			}
			return nil, err
		}
		if w.contributionContinuity != nil {
			if err = observer.Role(c, index, role.Launch.AuthorityContext.SessionID); err != nil {
				return nil, errors.Join(err, role.Close(context.WithoutCancel(c)))
			}
		}
		if observer != nil {
			close := role.Close
			role.Close = func(stop context.Context) error {
				if err := observer.StopRole(stop, role.Launch.AuthorityContext.SessionID); err != nil {
					return err
				}
				return close(stop)
			}
		}
		roles = append(roles, &role)
		return &role, nil
	}
	turnRequest := func(role *ContributionRole) hermesruntime.AttemptTurnRequest {
		admission := contributionAdmissionFunc(func(ctx context.Context, launch execution.LaunchContract, at time.Time) (execution.AdmissionDecision, error) {
			if err := admit(ctx, "runtime-prompt"); err != nil {
				return execution.AdmissionDecision{}, err
			}
			return role.Admission.CheckRuntimeAdmission(ctx, launch, at)
		})
		return hermesruntime.AttemptTurnRequest{Launch: role.Launch, Admission: admission, ParentAttemptID: role.Launch.ParentDispatch.ID, StateRoot: filepath.Join(filepath.Dir(filepath.Dir(w.implementation.root)), "runtime", "contribution"), Model: role.Launch.AuthorityContext.Authority.Hermes.Model, Provider: role.Launch.AuthorityContext.Authority.Hermes.Provider, Bounds: hermesruntime.AttemptBounds{InputBytes: hermesruntime.MaxAttemptInputBytes, OutputBytes: hermesruntime.MaxAttemptOutputBytes, Duration: hermesruntime.MaxAttemptDuration}}
	}
	candidateAccepted := false
	if w.contributionContinuity != nil {
		observer, err = w.contributionContinuity(ctx, request.Subject, sealed, base, runtime.Launch)
		if err != nil {
			return base, err
		}
		defer func() {
			resultErr = errors.Join(resultErr, observer.Close(context.WithoutCancel(ctx), candidateAccepted))
		}()
	}
	engine := &implementation.ManagedContribution{DB: custody.ImplementationStore(), GoBinary: w.implementation.config.GoBinary, StateRoot: filepath.Join(w.implementation.root, "contribution-evidence"), Repository: sealed.Repository, Admit: admit, Fetch: w.contributionFetch}
	if observer != nil {
		engine.Checkpoint = func(ctx context.Context, cp implementation.ContributionCheckpoint) error {
			if err := observer.Checkpoint(ctx, cp); err != nil {
				return err
			}
			if w.contributionCheckpoint != nil {
				return w.contributionCheckpoint(ctx, cp)
			}
			return nil
		}
		engine.ContinueBuilder = observer.ContinueBuilder
		engine.Native = func(c context.Context, cmd *exec.Cmd) error {
			session := runtime.Launch.AuthorityContext.SessionID
			if active != nil {
				session = active.Launch.AuthorityContext.SessionID
			}
			return observer.Native(c, session, cmd)
		}
	}
	engine.Build = func(c context.Context, input implementation.Request) ([]implementation.Edit, implementation.ContributionRoleIdentity, error) {
		if builder == nil {
			builder, err = resolve(c, 1)
			if err != nil {
				return nil, implementation.ContributionRoleIdentity{}, err
			}
		}
		active = builder
		var identity implementation.ContributionRoleIdentity
		input.InputObserver = func(observation implementation.InputObservation) error {
			if observation.Completed {
				identity = contributionIdentity(*builder, observation.RuntimeSessionID, observation.TurnID)
			}
			return nil
		}
		if observer != nil && observer.Turn != nil {
			if err := observer.Turn(c, input.PassID); err != nil {
				return nil, identity, err
			}
		}
		edits, err := (HermesPatchProposer{Adapter: w.implementation.adapter, Request: turnRequest(builder)}).Propose(c, input)
		return edits, identity, err
	}
	engine.Test = func(c context.Context, candidate implementation.ContributionCandidate, execute func(context.Context, implementation.ContributionRoleIdentity) error) (identity implementation.ContributionRoleIdentity, testErr error) {
		tester, err := resolve(c, 2)
		if err != nil {
			return implementation.ContributionRoleIdentity{}, err
		}
		active = tester
		defer func() {
			stop, cancel := context.WithTimeout(context.WithoutCancel(c), 5*time.Second)
			defer cancel()
			testErr = errors.Join(testErr, tester.Close(stop))
			// A failed revocation aborts the engine: the builder cannot resume.
			tester.Close = nil
			active = nil
		}()
		// A one-shot controller capability executes while this clean tester session
		// is live and freshly admitted. Runtime prose never determines acceptance.
		input, err := json.Marshal(struct {
			Instruction string
			Candidate   implementation.ContributionCandidate
		}{"Independently inspect this frozen candidate. No edits, shell, credentials, authority or acceptance verdict is available. Controller executes the immutable required behavior checks; narration cannot pass them.", candidate.TesterEvidence()})
		if err != nil {
			return implementation.ContributionRoleIdentity{}, err
		}
		turn := turnRequest(tester)
		turn.AttemptID = fmt.Sprintf("%s-tester-%d", sealed.RunID, candidate.Pass)
		turn.Input = string(input)
		turn.SessionCapability = func(capCtx context.Context, live execution.Turn) error {
			return execute(capCtx, contributionIdentity(*tester, live.RuntimeSessionID, live.ID))
		}
		if observer != nil && observer.Turn != nil {
			if err := observer.Turn(c, turn.AttemptID); err != nil {
				return identity, err
			}
		}
		result, err := w.implementation.adapter.AttemptTurn(c, turn)
		if err != nil {
			return implementation.ContributionRoleIdentity{}, err
		}
		return contributionIdentity(*tester, result.Attempt.RuntimeSessionID, result.Attempt.ID), nil
	}
	record, err := engine.Run(ctx, sealed.RunID, sealed.Digest, base.Attempt.AttemptID, sealed.Base, contract, *runtime.LoopRevision.Contribution.Managed)
	if err != nil {
		return base, err
	}
	if record.State != "candidate_accepted" {
		return base, errors.New("contribution candidate budget exhausted")
	}
	candidateAccepted = true
	// Durable candidate and criterion artifacts were stored before this return.
	// Never route them through v3 selected-file completion or mint final receipts.
	return base, ErrContributionCandidatePending
}
func validateContributionRole(r ContributionRole, c contribution.Contract, index int, at time.Time) error {
	if index < 1 || index > 2 || r.Admission == nil || r.Close == nil {
		return errors.New("exact role admission required")
	}
	expected := c.Roles[index]
	a := r.Agent
	authority := r.Launch.AuthorityContext
	if a.Digest != expected.Agent.Digest || a.AgentID != expected.Agent.ID || a.Charter != expected.Charter || a.Runtime != expected.Runtime || a.Ownership.OwnerID != c.OwnerID || authority.AgentID != a.Charter.ID || authority.CharterDigest != a.Charter.Digest || authority.SessionID == c.SessionID || len(authority.Authority.Tools) != 0 || len(authority.Authority.Credentials) != 0 || len(authority.Authority.Capabilities) != 0 {
		return errors.New("exact tool-free independent role required")
	}
	decision, err := r.Admission.CheckRuntimeAdmission(context.Background(), r.Launch, at)
	if err != nil {
		return err
	}
	return execution.ValidateAdmission(r.Launch, decision, at)
}
func contributionIdentity(role ContributionRole, runtime, turn string) implementation.ContributionRoleIdentity {
	a := role.Launch.AuthorityContext
	return implementation.ContributionRoleIdentity{AgentDigest: role.Agent.Digest, CharterDigest: role.Agent.Charter.Digest, AuthorityID: a.ID, AuthorityDigest: a.Digest, SessionID: a.SessionID, MandateID: a.MandateID, RuntimeSessionID: runtime, TurnID: turn}
}

// ContributionExecutionReadback is private owner-service evidence, not model
// assertions or a terminal-disposition API.
func (w *QueueWorker) ContributionExecutionReadback(run string) (implementation.ContributionRecord, error) {
	store, ok := w.repository.(interface{ ImplementationStore() implementation.Store })
	if !ok {
		return implementation.ContributionRecord{}, errors.New("contribution custody unavailable")
	}
	return (&implementation.ManagedContribution{DB: store.ImplementationStore()}).Read(run)
}

// Keep schema discovery distinct from executable capability qualification.
func managedContributionProfile(r loop.LoopRevision) error {
	if r.Contribution == nil || r.Contribution.Managed == nil {
		return errors.New("managed contribution execution profile required; selected-file fallback denied")
	}
	for _, step := range r.Steps {
		if step.Implementation != nil {
			return implementation.PreflightManaged(*step.Implementation, *r.Contribution.Managed)
		}
	}
	return errors.New("managed implementation contract required")
}
