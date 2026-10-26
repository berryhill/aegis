package orchestration

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/berryhill/aegis/internal/core"
	executiondomain "github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
)

// ErrNoContributionContinuation permits the original path only after a positive
// canonical read proves that no continuation was ever admitted.
var ErrNoContributionContinuation = errors.New("no contribution continuation")

var ErrContributionCustodyBusy = implementation.ErrWorkspaceBusy

// Read-only synchronization for finalizer winner reconciliation. The path is
// resolved from the exact retained owning contract, never selected by a caller.
func (w *QueueWorker) WaitContributionFinalization(ctx context.Context, subject core.Subject, queueID, run, contract string) error {
	if w == nil || w.repository == nil {
		return ErrWorkerDenied
	}
	owner, ok := w.repository.(fleet.ContributionRepository)
	if !ok {
		return ErrWorkerDenied
	}
	c, err := owner.GetContributionForQueue(ctx, queueID)
	if err != nil {
		return err
	}
	if c.OwnerID != subject.PrincipalID || c.QueueItemID != queueID || c.RunID != run || c.Digest != contract {
		return ErrWorkerDenied
	}
	return implementation.WaitWorkspaceCustody(ctx, c.Worktree)
}

// SetContributionFetch supplies a controller-owned fixture transport only. It is
// not a WorkRequest/model input. Nil preserves strict production HTTPS fetch;
// repository origin, branch and base checks remain mandatory in either case.
func (w *QueueWorker) SetContributionFetch(fetch func(context.Context, loop.ContributionExecutionPolicy) error) {
	w.contributionFetch = fetch
}

// AcceptedContributionCandidate exports intermediate evidence under the original
// controller authority. It never issues replacement sessions or renews authority.
func (w *QueueWorker) acceptedContributionCandidate(ctx context.Context, subject core.Subject, queueID string, revision loop.LoopRevision, consume func(*implementation.ManagedContribution, string, string, loop.ContributionExecutionPolicy) (implementation.ContributionCandidate, error)) (implementation.ContributionCandidate, error) {
	deny := fmt.Errorf("%w: exact current contribution handoff admission required", ErrWorkerDenied)
	if w == nil || w.repository == nil || w.service == nil || w.now == nil || w.implementation == nil || !filepath.IsAbs(w.implementation.config.GoBinary) || w.contributionRoles == nil {
		return implementation.ContributionCandidate{}, deny
	}
	_, sealed, err := w.contributionContract(ctx, queueID, revision)
	if err != nil {
		return implementation.ContributionCandidate{}, err
	}
	if revision.Contribution == nil || revision.Contribution.Managed == nil {
		return implementation.ContributionCandidate{}, deny
	}
	custody, ok := w.repository.(interface{ ImplementationStore() implementation.Store })
	if !ok || custody.ImplementationStore() == nil {
		return implementation.ContributionCandidate{}, deny
	}
	engine := &implementation.ManagedContribution{DB: custody.ImplementationStore(), GoBinary: w.implementation.config.GoBinary}
	record, err := engine.Read(sealed.RunID)
	if err != nil {
		return implementation.ContributionCandidate{}, err
	}
	engine.Admit = func(c context.Context, _ string) error {
		if c.Err() != nil || ctx.Err() != nil {
			return deny
		}
		_, current, err := w.contributionContract(c, queueID, revision)
		if err != nil || current.Digest != sealed.Digest {
			return deny
		}
		item, err := w.repository.GetQueueItem(c, queueID)
		if err != nil || item.GraphRunID != sealed.RunID || item.Submission.ID != sealed.SubmissionID || item.MaxAttempts != 1 {
			return fmt.Errorf("%w: queue", deny)
		}
		projection, err := w.repository.GetQueueProjection(c, queueID)
		if err != nil || projection.State != queue.StateClaimed || projection.Attempts != 1 || projection.ActiveClaimID == "" {
			return fmt.Errorf("%w: projection", deny)
		}
		claim, err := w.repository.GetClaim(c, projection.ActiveClaimID)
		if err != nil || claim.QueueItem != digestRef(item.ItemID, item.Digest) || claim.AttemptID != record.QueueAttempt {
			return fmt.Errorf("%w: claim", deny)
		}
		attempt, err := w.repository.GetAttempt(c, claim.AttemptID)
		if err != nil || attempt.QueueItem != claim.QueueItem || attempt.ClaimID != claim.ClaimID || attempt.GraphRunID != sealed.RunID || attempt.AttemptNumber != projection.Attempts {
			return fmt.Errorf("%w: attempt", deny)
		}
		execution, err := w.repository.GetLoopExecution(c, attempt.LoopExecutionID)
		if err != nil || execution.Loop != sealed.Loop || execution.Participant != sealed.Controller || execution.GraphRunID != sealed.RunID {
			return fmt.Errorf("%w: execution", deny)
		}
		agent, err := w.repository.GetAgentRevision(c, sealed.Controller.ID, sealed.Controller.Revision)
		if err != nil || agent.Digest != sealed.Controller.Digest || w.authorizeImplementationRevision(revision, agent) != nil {
			return fmt.Errorf("%w: host", deny)
		}
		authority, mandate, ready := w.service.resolveAuthority(c, subject, claim.Authority)
		if w.contributionCurrent != nil {
			current, admission, e := w.contributionCurrent(c, subject, sealed, claim)
			if !errors.Is(e, ErrNoContributionContinuation) {
				if e != nil || admission == nil || !agentMatchesAuthority(agent, current.AuthorityContext, current.Mandate) {
					return deny
				}
				decision, e := admission.CheckRuntimeAdmission(c, current, w.now())
				if e != nil {
					return e
				}
				return executiondomain.ValidateAdmission(current, decision, w.now())
			}
		}
		if !w.now().Before(claim.ExpiresAt) {
			return fmt.Errorf("%w: claim lease", deny)
		}
		if ready.State != ReadinessReady || authority.ID != claim.Authority.ID || authority.Digest != claim.Authority.Digest || !agentMatchesAuthority(agent, authority, mandate) {
			return fmt.Errorf("%w: authority", deny)
		}
		ready = w.service.Readiness(c, ReadinessRequest{Action: FleetActionRuntimeEffect, Subject: subject, Authority: claim.Authority, Agent: sealed.Controller, Loop: sealed.Loop})
		if ready.State != ReadinessReady {
			return fmt.Errorf("%w: readiness", deny)
		}
		return nil
	}
	if err := engine.Admit(ctx, "candidate-handoff"); err != nil {
		return implementation.ContributionCandidate{}, err
	}
	return consume(engine, sealed.RunID, sealed.Digest, *revision.Contribution.Managed)
}

// AcceptedContributionCandidate is diagnostic readback, not a custody lease.
func (w *QueueWorker) AcceptedContributionCandidate(ctx context.Context, subject core.Subject, queueID string, revision loop.LoopRevision) (implementation.ContributionCandidate, error) {
	return w.acceptedContributionCandidate(ctx, subject, queueID, revision, func(e *implementation.ManagedContribution, run, contract string, p loop.ContributionExecutionPolicy) (implementation.ContributionCandidate, error) {
		return e.AcceptedCandidate(run, contract, p)
	})
}

// ContributionQuiescenceGate must validate exact controller stop/epoch evidence.
// A nil role.Close result, idle map, lease expiry or readback is not stop proof.
type ContributionQuiescenceGate = implementation.ContributionQuiescenceGate

// WithAcceptedContributionCandidate holds cooperative kernel source custody for
// the entire synchronous action. It grants no commit, remote or stop authority.
func (w *QueueWorker) WithAcceptedContributionCandidate(ctx context.Context, subject core.Subject, queueID string, revision loop.LoopRevision, gate ContributionQuiescenceGate, action func(context.Context, ContributionCandidate) error) error {
	_, err := w.acceptedContributionCandidate(ctx, subject, queueID, revision, func(e *implementation.ManagedContribution, run, contract string, p loop.ContributionExecutionPolicy) (implementation.ContributionCandidate, error) {
		return implementation.ContributionCandidate{}, e.WithAcceptedCandidate(ctx, run, contract, p, gate, action)
	})
	return err
}

// Only the explicit immutable final-verification profile may select the
// terminal-last custody lifecycle. The generic handoff above is unchanged.
func (w *QueueWorker) WithFinalizableContributionCandidate(ctx context.Context, subject core.Subject, queueID string, revision loop.LoopRevision, gate ContributionQuiescenceGate, action, terminal func(context.Context, ContributionCandidate) error) error {
	if revision.Contribution == nil || revision.Contribution.FinalVerification != "aegis.contribution.final.v1" || terminal == nil {
		return ErrWorkerDenied
	}
	_, err := w.acceptedContributionCandidate(ctx, subject, queueID, revision, func(e *implementation.ManagedContribution, run, contract string, p loop.ContributionExecutionPolicy) (implementation.ContributionCandidate, error) {
		return implementation.ContributionCandidate{}, e.WithAcceptedCandidateFinalization(ctx, run, contract, p, gate, action, terminal)
	})
	return err
}
