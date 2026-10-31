package app

import (
	"context"
	"errors"
	"time"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"

	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

// ContributionExecutionResult reports candidate execution, never delivery success.
type ContributionExecutionResult struct {
	Provenance      *ContributionRunProvenance        `json:"provenance,omitempty"`
	Available       bool                              `json:"available"`
	Reason          string                            `json:"reason"`
	RequiredRole    string                            `json:"required_role,omitempty"`
	RequiredCharter *reference.RevisionRef            `json:"required_charter,omitempty"`
	Execution       *QueueExecutionView               `json:"execution,omitempty"`
	Candidate       *orchestration.ContributionRecord `json:"candidate,omitempty"`
}

// ExecuteContributionAs owns preparation, exact binding and processing for the
// already sealed intake. It does not publish a Graph or acquire new approvals.
func (s *Service) ExecuteContributionAs(ctx context.Context, subject core.Subject, key string) (result ContributionExecutionResult, err error) {
	c, view, err := s.ReadContributionAs(ctx, subject, key)
	if err != nil {
		return result, err
	}
	runCtx, finish, registerErr := s.beginContributionRun(ctx, c)
	if registerErr != nil {
		return result, registerErr
	}
	defer finish()
	return s.executeContributionInRun(runCtx, subject, key, c, view)
}

// executeContributionInRun executes only inside an already registered owner.
// The journey keeps that same owner through publication and final disposition;
// the standalone candidate API retains its original candidate-only lifetime.
func (s *Service) executeContributionInRun(ctx context.Context, subject core.Subject, key string, c contribution.Contract, view QueueExecutionView) (result ContributionExecutionResult, err error) {
	var work *orchestration.WorkRequest
	result.Execution = &view
	lv, err := s.GetLoopViewAs(ctx, subject, c.Loop.ID, c.Loop.Revision)
	if err != nil || lv.Revision.Digest != c.Loop.Digest {
		return result, ErrDenied
	}
	if lv.Revision.Contribution == nil || lv.Revision.Contribution.Managed == nil {
		result.Reason = "intake_only_unqualified_profile"
		return result, nil
	}
	result.Available = true
	if expected := ContributionExpectedService(ctx); expected != nil && *expected != CurrentContributionServiceIdentity() {
		result.Reason = "candidate_service_mismatch"
		return result, nil
	}
	result.Provenance, err = s.ReadContributionRunProvenanceAs(ctx, subject, key)
	if err != nil {
		return result, err
	}
	if s.QueueWorker == nil {
		result.Reason = "contribution_controller_unavailable"
		return result, nil
	}
	// Durable attempts and active controller ownership are replay fences, including
	// the binding-to-claim interval. Never replace a spent execution with a new one.
	if len(view.Attempts) > 0 || (!view.Projection.State.IsPreparation() && view.Projection.State != queue.StateQueued) {
		result.Reason = "existing_execution"
		if record, readErr := s.ReadContributionExecutionAs(ctx, subject, key); readErr == nil {
			result.Candidate = &record
			result.Reason = record.State
		} else if !orchestration.IsContributionNotFound(readErr) {
			return result, readErr
		}
		return result, nil
	}
	if lv.Lifecycle.State != loop.LifecycleActive || lv.Lifecycle.ActiveDigest != c.Loop.Digest || !s.Now().Before(c.Deadline) {
		return result, ErrDenied
	}
	// Independent prerequisites are checked before any clean role session or
	// worktree is created. Run consent never substitutes for provisioning.
	for i, role := range c.Roles {
		result.RequiredRole = []string{"controller", "builder", "tester"}[i]
		ref := role.Charter
		result.RequiredCharter = &ref
		agent, e := s.FleetRepository.LatestAgentRevision(ctx, role.Agent.ID)
		if e != nil || agent.Digest != role.Agent.Digest || agent.Charter != role.Charter || agent.Runtime != role.Runtime || agent.Lifecycle != registry.LifecycleEnabled || agent.Ownership.OwnerID != c.OwnerID {
			return result, ErrDenied
		}
		verified, e := s.hasVerifiedReceipt(role.Charter.Digest)
		if e != nil {
			result.Reason = "provisioning_receipt_unavailable"
			return result, nil
		}
		if !verified {
			result.Reason = "decision_delivery_unavailable"
			if s.ContributionDecisionDelivery == nil {
				return result, nil
			}
			status, deliveryErr := s.ContributionDecisionDelivery(ctx, subject, key, result.RequiredRole)
			if deliveryErr != nil {
				return result, deliveryErr
			}
			result.Reason = "decision_delivery_" + status
			if status != "approved" {
				return result, nil
			}
			// Transport output is not provisioning authority. Re-read ordinary
			// canonical state and then repeat normal fresh role admission below.
			verified, e = s.hasVerifiedReceipt(role.Charter.Digest)
			if e != nil || !verified || s.requirePrincipal(subject) != nil {
				result.Reason = "provisioning_receipt_missing"
				return result, nil
			}
		}
		charter, e := s.GetCharter(role.Charter.ID, role.Charter.Revision)
		if e != nil || charter.Digest != role.Charter.Digest {
			result.Reason = "exact_charter_unavailable"
			return result, nil
		}
		decision, e := s.Select(charter, subject, "", core.Environment{Name: "local"})
		if e != nil {
			result.Reason = "session_selection_" + decision.Reason
			return result, nil
		}
	}
	result.RequiredRole, result.RequiredCharter = "", nil
	readiness, e := s.ReadContributionReadinessAs(ctx, subject, ContributionReadinessInput{Loop: c.Loop, Scope: c.Effect})
	if e != nil {
		return result, e
	}
	for _, status := range []DoerPrerequisiteStatus{readiness.Profile, readiness.Native, readiness.Remote, readiness.RemoteAuthentication} {
		if status.State == "blocked" {
			result.Reason = status.Reason
			return result, nil
		}
	}
	for _, role := range readiness.Roles {
		for _, status := range []DoerPrerequisiteStatus{role.Reference, role.Selection, role.Provider} {
			if status.State == "blocked" {
				result.RequiredRole = role.Role
				result.Reason = status.Reason
				return result, nil
			}
		}
	}
	if err = s.retainContributionRunProvenance(c); err != nil {
		return result, err
	}
	result.Provenance, err = s.ReadContributionRunProvenanceAs(ctx, subject, key)
	if err != nil {
		return result, err
	}
	// Persist entry into effectful preparation before a session can be launched.
	_, preparation, _, prepErr := s.ObserveContributionContinuityAs(ctx, subject, key)
	if prepErr != nil {
		return result, prepErr
	}
	if preparation.Sequence == 0 {
		preparation, prepErr = s.RecordContributionContinuityAs(ctx, subject, c.SessionID, key, ContributionContinuityInput{ContractDigest: c.Digest, Kind: "initialize"})
		if prepErr != nil {
			return result, prepErr
		}
	}
	_, prepErr = s.RecordContributionContinuityAs(ctx, subject, c.SessionID, key, ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: preparation.Sequence, ExpectedDigest: preparation.Digest, Kind: "checkpoint", Stage: "prepare", Candidate: c.RunID, Evidence: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: c.Key, Digest: c.Digest}})
	if prepErr != nil {
		return result, prepErr
	}
	input := QueueLoopInput{Agent: c.Controller, Loop: c.Loop, IdempotencyKey: c.Key}
	prepared, err := s.prepareQueuedLoop(ctx, subject, input, lv, QueueLoopResult{RequestID: c.Key, QueueItemID: c.QueueItemID}, c.RunID, &work)
	if err != nil {
		work = nil
		return result, err
	}
	result.Reason, result.Execution = prepared.Reason, prepared.Execution
	if work == nil {
		return result, nil
	}
	// The registered service context owns this run, not the HTTP caller.
	_, err = s.ProcessQueueItemAs(ctx, subject, *work)
	if errors.Is(err, orchestration.ErrContributionCandidatePending) {
		err = nil
	}
	_, observed, readErr := s.ReadContributionAs(ctx, subject, key)
	if readErr != nil {
		return result, errors.Join(err, readErr)
	}
	result.Execution = &observed
	result.Reason = "processed"
	if record, e := s.ReadContributionExecutionAs(ctx, subject, key); e == nil {
		result.Candidate = &record
		result.Reason = record.State
	} else if !orchestration.IsContributionNotFound(e) {
		return result, errors.Join(err, e)
	}
	return result, err
}

// ContributionExecutionStatusAs derives availability and state from exact
// immutable profile and durable execution readback; it never launches work.
func (s *Service) ContributionExecutionStatusAs(ctx context.Context, subject core.Subject, key string) (ContributionExecutionResult, error) {
	c, view, err := s.ReadContributionAs(ctx, subject, key)
	if err != nil {
		return ContributionExecutionResult{}, err
	}
	lv, err := s.GetLoopViewAs(ctx, subject, c.Loop.ID, c.Loop.Revision)
	if err != nil || lv.Revision.Digest != c.Loop.Digest {
		return ContributionExecutionResult{}, ErrDenied
	}
	r := ContributionExecutionResult{Execution: &view, Reason: "intake_only_unqualified_profile"}
	r.Provenance, err = s.ReadContributionRunProvenanceAs(ctx, subject, key)
	if err != nil {
		return r, err
	}
	if lv.Revision.Contribution != nil && lv.Revision.Contribution.Managed != nil {
		r.Available = true
		r.Reason = "awaiting_execution"
		if len(view.Attempts) > 0 {
			r.Reason = "execution_attempted"
			record, e := s.ReadContributionExecutionAs(ctx, subject, key)
			if e == nil {
				r.Candidate = &record
				r.Reason = record.State
			} else if !orchestration.IsContributionNotFound(e) {
				return r, e
			}
		}
	}
	return r, nil
}

// resolveContributionRole uses ordinary provisioning, deterministic stanza
// selection, independent mandates, clean session launch and admission. It never
// rewrites the immutable root Queue RuntimeBinding or inherits its authority.
func (s *Service) resolveContributionRole(ctx context.Context, subject core.Subject, c contribution.Contract, index int) (orchestration.ContributionRole, error) {
	deny := errors.New("independent contribution role denied")
	if index < 1 || index > 2 || subject.PrincipalID != c.OwnerID || s.requireFleetPrincipal(subject) != nil || !s.Now().Before(c.Deadline) {
		return orchestration.ContributionRole{}, deny
	}
	expected := c.Roles[index]
	agent, err := s.FleetRepository.LatestAgentRevision(ctx, expected.Agent.ID)
	if err != nil || agent.Digest != expected.Agent.Digest || agent.Charter != expected.Charter || agent.Runtime != expected.Runtime || agent.Lifecycle != registry.LifecycleEnabled || agent.Ownership.OwnerID != c.OwnerID {
		return orchestration.ContributionRole{}, deny
	}
	var continued *core.Session
	_, continuity, _, observeErr := s.ObserveContributionContinuityAs(ctx, subject, c.Key)
	if observeErr != nil {
		return orchestration.ContributionRole{}, observeErr
	}
	name := []string{"controller", "builder", "tester"}[index]
	if _, exists := continuity.RoleAuthorities[name]; exists {
		session, e := s.ResumeContributionRoleAs(ctx, subject, c.Key, name, ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: continuity.Sequence, ExpectedDigest: continuity.Digest})
		if e != nil {
			return orchestration.ContributionRole{}, e
		}
		continued = &session
	}
	var mandate core.Mandate
	if continued != nil {
		mandate = continued.Mandate
	} else {
		mandate, _, err = s.PreviewSessionAs(ctx, subject, agent.Charter.ID, agent.Charter.Revision, "", core.Environment{Name: "local"})
		if err != nil {
			return orchestration.ContributionRole{}, err
		}
	}
	if mandate.CharterDigest != agent.Charter.Digest || len(mandate.Tools) != 0 || len(mandate.Capabilities) != 0 || len(mandate.Scopes.Credentials) != 0 {
		return orchestration.ContributionRole{}, deny
	}
	var session core.Session
	if continued != nil {
		session = *continued
	} else {
		session, err = s.StartSessionAs(ctx, subject, mandate.ID)
	}
	if err != nil {
		return orchestration.ContributionRole{}, err
	}
	closeRole := func(stop context.Context) error {
		return s.closeContributionRole(stop, c, session, name)
	}
	authority, err := s.authorityContextForSession(ctx, session.ID)
	if err != nil {
		return orchestration.ContributionRole{}, errors.Join(err, closeRole(context.WithoutCancel(ctx)))
	}
	now := s.Now()
	launch := execution.LaunchContract{OwnerID: c.OwnerID, Mandate: mandate, AuthorityContext: authority, ParentDispatch: execution.Dispatch{ID: session.ID + "-contribution-dispatch", AuthorityContextID: authority.ID, State: execution.StateSucceeded, RequestedAt: now, StartedAt: &now, FinishedAt: &now}}
	// Append exact role provenance in controller-owned storage. This is not a new
	// authority grant, a reusable parent token, or an operator-carried binding file.
	if err = s.Store.Create("contribution-role", authority.ID, struct {
		Contract string
		Index    int
		Launch   execution.LaunchContract
	}{c.Digest, index, launch}); err != nil {
		return orchestration.ContributionRole{}, errors.Join(err, closeRole(context.WithoutCancel(ctx)))
	}
	// Actual effects require an already registered canonical role, before the
	// worker's first validation (the observer subsequently verifies this binding).
	if continued == nil {
		_, state, _, e := s.ObserveContributionContinuityAs(ctx, subject, c.Key)
		if e == nil {
			state, e = s.AttachContributionObservationAs(ctx, subject, session.ID, c.Key, ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: state.Sequence, ExpectedDigest: state.Digest})
		}
		if e == nil {
			_, e = s.AdmitContributionRoleAs(ctx, subject, session.ID, c.Key, name, ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: state.Sequence, ExpectedDigest: state.Digest})
		}
		if e != nil {
			return orchestration.ContributionRole{}, errors.Join(e, closeRole(context.WithoutCancel(ctx)))
		}
	}
	return orchestration.ContributionRole{Agent: agent, Launch: launch, Admission: contributionRoleAdmission{s, c, index}, Close: closeRole}, nil
}

type contributionRoleAdmission struct {
	service  *Service
	contract contribution.Contract
	index    int
}

func (a contributionRoleAdmission) CheckRuntimeAdmission(ctx context.Context, launch execution.LaunchContract, at time.Time) (execution.AdmissionDecision, error) {
	decision := execution.AdmissionDecision{AuthorityContextID: launch.AuthorityContext.ID, AuthorityContextDigest: launch.AuthorityContext.Digest, CheckedAt: at}
	s := a.service
	if err := ctx.Err(); err != nil {
		return decision, err
	}
	if a.index < 0 || a.index >= len(a.contract.Roles) {
		return decision, ErrDenied
	}
	// Reload durable role, epoch, cancellation and stop controls for every
	// prompt, source effect and native command, not only the native wrapper.
	_, state, _, err := s.ObserveContributionContinuityAs(ctx, launch.Mandate.Subject, a.contract.Key)
	if err != nil {
		return decision, ErrDenied
	}
	name := []string{"controller", "builder", "tester"}[a.index]
	role, err := s.CheckContributionRoleAs(ctx, launch.Mandate.Subject, launch.AuthorityContext.SessionID, a.contract.Key, name, ContributionContinuityInput{ContractDigest: a.contract.Digest, ExpectedSequence: state.Sequence, ExpectedDigest: state.Digest})
	if err != nil || role.Authority.ID != launch.AuthorityContext.ID || role.Authority.Digest != launch.AuthorityContext.Digest || role.MandateID != launch.Mandate.ID {
		return decision, ErrDenied
	}
	expected := a.contract.Roles[a.index]
	if !at.Before(a.contract.Deadline) || launch.Mandate.CharterDigest != expected.Charter.Digest || s.validateMandateAt(launch.Mandate, at) != nil {
		return decision, ErrDenied
	}
	agent, err := s.FleetRepository.LatestAgentRevision(ctx, expected.Agent.ID)
	if err != nil || agent.Digest != expected.Agent.Digest || agent.Lifecycle != registry.LifecycleEnabled {
		return decision, ErrDenied
	}
	session, err := s.GetSession(launch.AuthorityContext.SessionID)
	if err != nil || session.Status != "running" || session.Mandate.ID != launch.Mandate.ID {
		return decision, ErrDenied
	}
	canonical, err := s.Authority.GetAuthorityContext(ctx, launch.AuthorityContext.ID)
	if err != nil || canonical.Digest != launch.AuthorityContext.Digest {
		return decision, ErrDenied
	}
	admitted, err := s.AuthorityCommands.AuthorityAdmission(ctx, canonical.ID, canonical.Digest, at)
	if err != nil {
		return decision, err
	}
	decision.Allowed = admitted.Admitted
	decision.Reason = admitted.ReasonCode
	return decision, nil
}

// ReadContributionExecutionAs is authenticated readback of intermediate private
// evidence. A candidate_accepted record is never whole-contribution success.
func (s *Service) ReadContributionExecutionAs(ctx context.Context, subject core.Subject, key string) (orchestration.ContributionRecord, error) {
	c, _, err := s.ReadContributionAs(ctx, subject, key)
	if err != nil {
		return orchestration.ContributionRecord{}, err
	}
	record, err := s.QueueWorker.ContributionExecutionReadback(c.RunID)
	if err == nil && (record.Contract != c.Digest || record.RunID != c.RunID) {
		return orchestration.ContributionRecord{}, ErrDenied
	}
	return record, err
}
