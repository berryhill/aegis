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
	s.loopRunMu.Lock()
	var work *orchestration.WorkRequest
	defer func() {
		if work == nil {
			s.loopRunMu.Unlock()
		}
	}()
	c, view, err := s.ReadContributionAs(ctx, subject, key)
	if err != nil {
		return result, err
	}
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
	if s.QueueWorker == nil {
		result.Reason = "contribution_controller_unavailable"
		return result, nil
	}
	// Durable attempts and active controller ownership are replay fences, including
	// the binding-to-claim interval. Never replace a spent execution with a new one.
	if s.loopRunWorkers[c.QueueItemID] || len(view.Attempts) > 0 || (!view.Projection.State.IsPreparation() && view.Projection.State != queue.StateQueued) {
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
			result.Reason = "provisioning_receipt_missing"
			return result, nil
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
	if s.loopRunWorkers == nil {
		s.loopRunWorkers = make(map[string]bool)
	}
	s.loopRunWorkers[c.QueueItemID] = true
	s.loopRunMu.Unlock()
	defer func() { s.loopRunMu.Lock(); delete(s.loopRunWorkers, c.QueueItemID); s.loopRunMu.Unlock() }()
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
	mandate, _, err := s.PreviewSessionAs(ctx, subject, agent.Charter.ID, agent.Charter.Revision, "", core.Environment{Name: "local"})
	if err != nil {
		return orchestration.ContributionRole{}, err
	}
	if mandate.CharterDigest != agent.Charter.Digest || len(mandate.Tools) != 0 || len(mandate.Capabilities) != 0 || len(mandate.Scopes.Credentials) != 0 {
		return orchestration.ContributionRole{}, deny
	}
	session, err := s.StartSessionAs(ctx, subject, mandate.ID)
	if err != nil {
		return orchestration.ContributionRole{}, err
	}
	closeRole := func(stop context.Context) error {
		return s.endSession(stop, session.ID, "stopped", "contribution_role_finished", true)
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
