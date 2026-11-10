package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/registry"
	hermesruntime "github.com/berryhill/aegis/internal/runtime/hermes"
)

// The immutable intent survives all preparation failures, sessions and controls.
// Planner calls use a profile-derived sub-budget of the existing 128 runtime calls,
// not a per-login/per-retry allowance. Each actual call is reserved separately.
type contributionEngineeringIntent struct {
	Contract      contribution.Contract `json:"contract"`
	ReservedTurns uint32                `json:"reserved_turns"` // legacy intent fence; zero for journaled intents
	TurnLimit     uint32                `json:"turn_limit"`
}

func engineeringIntentID(owner, key string) string {
	return "engineering-" + strings.TrimPrefix(core.Digest(struct{ Owner, Key string }{owner, key}), "sha256:")
}

func (s *Service) prepareContributionEngineering(ctx context.Context, subject core.Subject, pending contribution.Contract, revision loop.LoopRevision, template loop.VerifiedImplementation) (out contribution.Contract, err error) {
	id := engineeringIntentID(subject.PrincipalID, pending.Key)
	var intent contributionEngineeringIntent
	if e := s.Store.Load("contribution-engineering-intents", id, &intent); e == nil {
		if intent.Contract.PayloadDigest != pending.PayloadDigest {
			return out, ErrConflict
		}
		pending = intent.Contract
	} else if !errors.Is(e, os.ErrNotExist) {
		return out, e
	} else {
		limit, budgetErr := contribution.EngineeringPlanningAllowance(template)
		if budgetErr != nil {
			return out, budgetErr
		}
		intent = contributionEngineeringIntent{Contract: pending, TurnLimit: limit}
		if e = s.Store.Create("contribution-engineering-intents", id, intent); e != nil {
			return out, e
		}
	}
	head, e := s.ReadContributionEngineeringAs(ctx, subject, pending.Key)
	if e != nil {
		return out, e
	}
	if intent.TurnLimit == 0 {
		return out, errors.New("legacy engineering preparation outcome unknown; inference will not be replayed")
	}
	if head.Control.Stop || !s.Now().Before(pending.Deadline) {
		return out, ErrDenied
	}
	// Recovery only follows an observed adapter return AND exact session cleanup.
	// A process crash between reservation and return is deliberately not replayable.
	if !head.Quiescent || head.ReturnedTurns != head.ReservedTurns {
		return out, errors.New("engineering preparation outcome unknown; reconciliation required")
	}
	if e = s.Store.Load("contribution-engineering-prepared", id, &out); e == nil {
		if out.PayloadDigest != pending.PayloadDigest || out.Preparation == nil || out.Preparation.Validate(out.Source, *revision.Contribution.Engineering, out.TemplateDigest, out.Base) != nil {
			return contribution.Contract{}, ErrDenied
		}
		return s.applyEngineeringDelivery(out, head.Control.Delivery)
	} else if !errors.Is(e, os.ErrNotExist) {
		return out, e
	}
	if head.ReservedTurns >= head.TurnLimit {
		return out, errors.New("engineering preparation runtime budget exhausted")
	}
	role := pending.Roles[1]
	agent, e := s.FleetRepository.LatestAgentRevision(ctx, role.Agent.ID)
	if e != nil || agent.Revision != role.Agent.Revision || agent.Digest != role.Agent.Digest || agent.Charter != role.Charter || agent.Runtime != role.Runtime || agent.Lifecycle != registry.LifecycleEnabled || agent.Ownership.OwnerID != subject.PrincipalID {
		return out, ErrDenied
	}
	verified, e := s.hasVerifiedReceipt(role.Charter.Digest)
	if e != nil {
		return out, e
	}
	if !verified {
		if s.ContributionDecisionDelivery == nil {
			return out, errors.New("engineering provisioning decision delivery unavailable")
		}
		status, deliveryErr := s.ContributionDecisionDelivery(ctx, subject, pending.Key, "builder")
		if deliveryErr != nil {
			return out, deliveryErr
		}
		verified, e = s.hasVerifiedReceipt(role.Charter.Digest)
		if status != "approved" || e != nil || !verified || s.requirePrincipal(subject) != nil {
			return out, fmt.Errorf("engineering provisioning decision: %s", status)
		}
	}
	charter, e := s.GetCharter(role.Charter.ID, role.Charter.Revision)
	if e != nil || charter.Digest != role.Charter.Digest {
		return out, ErrDenied
	}
	selected, e := s.Select(charter, subject, "", core.Environment{Name: "local"})
	if e != nil || selected.Selected == nil {
		return out, ErrDenied
	}
	ctx, cancel := context.WithDeadline(ctx, pending.Deadline)
	defer cancel()
	mandate, _, e := s.PreviewSessionAs(ctx, subject, role.Charter.ID, role.Charter.Revision, "", core.Environment{Name: "local"})
	if e != nil {
		return out, e
	}
	if len(mandate.Tools) != 0 || len(mandate.Capabilities) != 0 || len(mandate.Scopes.Credentials) != 0 {
		return out, ErrDenied
	}
	var prepProvider, prepModel string
	if revision.Contribution != nil {
		prepProvider, prepModel = revision.Contribution.RoleLane("prepare")
	}
	prepLane, e := mandate.Hermes.ResolveLane(prepProvider, prepModel)
	if e != nil {
		return out, ErrDenied
	}
	// This dispatch owns preparation, not session launch. Its actual terminal
	// outcome is independent of the successful launch required by AttemptTurn.
	requested := s.Now()
	operation := execution.LaunchContract{OwnerID: pending.OwnerID, Mandate: mandate, ParentDispatch: execution.Dispatch{ID: fmt.Sprintf("%s-prepare-%d", id, head.Sequence+1), State: execution.StateRequested, RequestedAt: requested}}
	if e = s.retainEngineeringDispatch(id, operation); e != nil {
		return out, e
	}
	s.contributionEngineeringMu.Lock()
	head, e = s.engineeringStatus(id, intent)
	if e == nil {
		next := head
		next.Phase = "session-start"
		next.Outcome = "in_progress"
		next.Quiescent = false
		head, e = s.appendEngineeringStatus(head, next)
	}
	s.contributionEngineeringMu.Unlock()
	if e != nil {
		return out, e
	}
	var session core.Session
	defer func() {
		quiescent := false
		if session.ID != "" {
			stop, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			cleanup := s.endSession(stop, session.ID, "terminated", "engineering_preparation_complete", true)
			cancel()
			err = errors.Join(err, cleanup)
			retained, re := s.GetSession(session.ID)
			quiescent = cleanup == nil && re == nil && retained.Status != "running" && !processMatches(retained.RuntimePID, retained.ProcessStart)
		} else { // Only a typed pre-effect failure can attest zero session effects.
			var failure *sessionStartFailure
			quiescent = errors.As(err, &failure) && failure.RetrySafe
		}
		finished := s.Now()
		operation.ParentDispatch.FinishedAt = &finished
		operation.ParentDispatch.State = execution.StateFailed
		operation.ParentDispatch.Reason = "preparation_failed"
		if err == nil {
			operation.ParentDispatch.State = execution.StateSucceeded
			operation.ParentDispatch.Reason = "preparation_completed"
		} else if ctx.Err() != nil {
			operation.ParentDispatch.State = execution.StateCancelled
			operation.ParentDispatch.Reason = "preparation_interrupted"
		}
		err = errors.Join(err, s.retainEngineeringDispatch(id, operation))
		s.contributionEngineeringMu.Lock()
		defer s.contributionEngineeringMu.Unlock()
		current, re := s.engineeringStatus(id, intent)
		if re == nil {
			next := current
			next.Quiescent = quiescent
			next.Outcome = operation.ParentDispatch.Reason
			if !quiescent || next.ReturnedTurns != next.ReservedTurns {
				next.Outcome = "interrupted_unknown"
			}
			_, re = s.appendEngineeringStatus(current, next)
		}
		err = errors.Join(err, re)
	}()
	session, e = s.StartSessionAs(ctx, subject, mandate.ID)
	if e != nil {
		return out, e
	}
	authority, e := s.authorityContextForSession(ctx, session.ID)
	if e != nil {
		return out, e
	}
	operation.AuthorityContext = authority
	operation.ParentDispatch.AuthorityContextID = authority.ID
	started := session.StartedAt
	operation.ParentDispatch.StartedAt = &started
	operation.ParentDispatch.State = execution.StateStarted
	if e = s.retainEngineeringDispatch(id, operation); e != nil {
		return out, e
	}
	// Parent success means the real clean-session dispatch completed, NOT that
	// inference or preparation succeeded. Admission verifies its canonical session.
	finished := s.Now()
	launch := execution.LaunchContract{OwnerID: pending.OwnerID, Mandate: mandate, AuthorityContext: authority, ParentDispatch: execution.Dispatch{ID: session.ID + "-engineering-launch", AuthorityContextID: authority.ID, State: execution.StateSucceeded, RequestedAt: requested, StartedAt: &started, FinishedAt: &finished, Reason: "clean_session_started"}}
	if e = s.Store.Create("contribution-engineering-launches", authority.ID, launch); e != nil {
		return out, e
	}
	admission := engineeringPreparationAdmission{s, subject, intent, id}
	request := hermesruntime.AttemptTurnRequest{Launch: launch, Admission: admission, AttemptID: fmt.Sprintf("%s-%d", id, head.Sequence), ParentAttemptID: launch.ParentDispatch.ID, StateRoot: s.Store.Root(), Model: prepLane.Model, Provider: prepLane.Provider, Bounds: hermesruntime.AttemptBounds{InputBytes: hermesruntime.MaxAttemptInputBytes, OutputBytes: hermesruntime.MaxAttemptOutputBytes, Duration: hermesruntime.MaxAttemptDuration}}
	receipt, e := (orchestration.HermesPatchProposer{Adapter: engineeringTurnRecorder{s, intent, id}, Request: request}).PrepareEngineering(ctx, pending.Source, *revision.Contribution.Engineering, template, revision.Contribution.Managed.RepositoryPath, pending.Base)
	if e != nil {
		return out, e
	}
	if receipt.Validate(pending.Source, *revision.Contribution.Engineering, pending.TemplateDigest, pending.Base) != nil {
		return out, ErrDenied
	}
	if d, e := admission.CheckRuntimeAdmission(ctx, launch, s.Now()); e != nil || !d.Allowed {
		return out, ErrDenied
	}
	spec := receipt.Scope
	pending.Preparation = &receipt
	pending.Outcome, pending.Acceptance = spec.Outcome, spec.Acceptance
	pending.Exclusions = append(append([]string{}, revision.Contribution.Exclusions...), spec.Exclusions...)
	pending.Worktree = contribution.EngineeringWorktree(template.Workspace, pending.RunID)
	writable, e := loop.EngineeringWritableSelection(spec.WritableFiles, *revision.Contribution.Engineering, revision.Contribution.Managed)
	if e != nil {
		return out, e
	}
	resolved, e := orchestration.ResolveContributionEngineeringContext(ctx, template, spec.Outcome, spec.Acceptance, pending.Worktree, revision.Contribution.Managed.RepositoryPath, writable, spec.ContextFiles)
	if e != nil {
		return out, e
	}
	pending.Engineering = &resolved
	out, e = contribution.Seal(pending)
	if e != nil {
		return out, e
	}
	if _, e = contribution.BindLoop(revision, out); e != nil {
		return contribution.Contract{}, e
	}
	if e = s.Store.Create("contribution-engineering-prepared", id, out); e != nil {
		return contribution.Contract{}, e
	}
	head, e = s.ReadContributionEngineeringAs(ctx, subject, pending.Key)
	if e != nil {
		return contribution.Contract{}, e
	}
	return s.applyEngineeringDelivery(out, head.Control.Delivery)
}

func (s *Service) applyEngineeringDelivery(c contribution.Contract, target string) (contribution.Contract, error) {
	effect, e := contribution.RestrictDelivery(c.Effect, target)
	if e != nil {
		return contribution.Contract{}, e
	}
	c.Effect = effect
	return contribution.Seal(c)
}

type engineeringTurnRecorder struct {
	service *Service
	intent  contributionEngineeringIntent
	id      string
}

func (a engineeringTurnRecorder) AttemptTurn(ctx context.Context, r hermesruntime.AttemptTurnRequest) (hermesruntime.AttemptTurnResult, error) {
	s := a.service
	s.contributionEngineeringMu.Lock()
	head, e := s.engineeringStatus(a.id, a.intent)
	if e == nil && (head.ReservedTurns >= head.TurnLimit || head.Control.Stop) {
		e = ErrDenied
	}
	if e == nil {
		next := head
		next.ReservedTurns++
		next.Phase = "inspect"
		if strings.HasSuffix(r.AttemptID, "-scope") {
			next.Phase = "scope"
		}
		next.SessionID = r.Launch.AuthorityContext.SessionID
		head, e = s.appendEngineeringStatus(head, next)
	}
	s.contributionEngineeringMu.Unlock()
	if e != nil {
		return hermesruntime.AttemptTurnResult{}, e
	}
	result, runErr := s.Hermes.AttemptTurn(ctx, r) // returns only after owned child Wait
	// Private exact transcript custody, including rejected output; not public text.
	e = s.Store.Create("contribution-engineering-turns", fmt.Sprintf("%s-%d", a.id, head.ReservedTurns), struct {
		Input  string
		Result hermesruntime.AttemptTurnResult
	}{r.Input, result})
	s.contributionEngineeringMu.Lock()
	defer s.contributionEngineeringMu.Unlock()
	current, re := s.engineeringStatus(a.id, a.intent)
	if re == nil && e == nil {
		next := current
		next.ReturnedTurns++
		next.Turn = &result.Attempt
		_, re = s.appendEngineeringStatus(current, next)
	}
	return result, errors.Join(runErr, e, re)
}

type engineeringPreparationAdmission struct {
	service *Service
	subject core.Subject
	intent  contributionEngineeringIntent
	id      string
}

func (a engineeringPreparationAdmission) CheckRuntimeAdmission(ctx context.Context, launch execution.LaunchContract, at time.Time) (execution.AdmissionDecision, error) {
	deny := execution.AdmissionDecision{CheckedAt: at}
	s := a.service
	c := a.intent.Contract
	if ctx.Err() != nil || s.requireFleetPrincipal(a.subject) != nil || !s.Now().Before(c.Deadline) || !at.Before(c.Deadline) || s.validateMandateAt(launch.Mandate, at) != nil {
		return deny, ErrDenied
	}
	var stored contributionEngineeringIntent
	if s.Store.Load("contribution-engineering-intents", a.id, &stored) != nil || core.Digest(stored) != core.Digest(a.intent) {
		return deny, ErrDenied
	}
	status, e := s.ReadContributionEngineeringAs(ctx, a.subject, c.Key)
	if e != nil || status.Control.Stop {
		return deny, ErrDenied
	}
	revision, e := s.GetLoopViewAs(ctx, a.subject, c.Loop.ID, c.Loop.Revision)
	if e != nil || revision.Revision.Digest != c.Loop.Digest || revision.Lifecycle.State != loop.LifecycleActive || revision.Lifecycle.ActiveDigest != c.Loop.Digest || s.validateContributionAgents(ctx, a.subject, revision.Revision) != nil {
		return deny, ErrDenied
	}
	var expected execution.LaunchContract
	if s.Store.Load("contribution-engineering-launches", launch.AuthorityContext.ID, &expected) != nil || core.Digest(expected) != core.Digest(launch) {
		return deny, ErrDenied
	}
	canonical, e := s.Authority.GetAuthorityContext(ctx, launch.AuthorityContext.ID)
	if e != nil || canonical.Digest != launch.AuthorityContext.Digest || canonical.SubjectID != a.subject.ID || canonical.CharterDigest != c.Roles[1].Charter.Digest {
		return deny, ErrDenied
	}
	session, e := s.GetSession(canonical.SessionID)
	if e != nil || session.Status != "running" || session.Mandate.ID != launch.Mandate.ID || !session.StartedAt.Equal(*launch.ParentDispatch.StartedAt) || !processMatches(session.RuntimePID, session.ProcessStart) {
		return deny, ErrDenied
	}
	admitted, e := s.AuthorityCommands.AuthorityAdmission(ctx, canonical.ID, canonical.Digest, at)
	if e != nil || !admitted.Admitted {
		return deny, ErrDenied
	}
	return execution.AdmissionDecision{Allowed: true, AuthorityContextID: canonical.ID, AuthorityContextDigest: canonical.Digest, CheckedAt: at}, nil
}

var _ execution.AdmissionChecker = engineeringPreparationAdmission{}
