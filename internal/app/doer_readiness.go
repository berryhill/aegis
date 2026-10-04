package app

import (
	"context"
	"errors"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

// DoerPrerequisiteStatus distinguishes a denial from a check not yet performed.
// Reasons are stable controller diagnostics, never raw helper/runtime errors.
type DoerPrerequisiteStatus struct {
	State  string `json:"state"` // ready, blocked, or not_checked
	Reason string `json:"reason,omitempty"`
}

type DoerCandidateReadinessInput struct {
	Agent     reference.RevisionRef `json:"agent"`
	Candidate loop.LoopRevision     `json:"candidate"`
}

// DoerCandidateReadiness is an observation, not publication, approval, a
// reservation, mandate, or runtime-effect authority. CanExecute means only that
// the candidate may request execution preparation; fresh admission still applies.
// Helper success qualifies protocol availability, NOT actual task eligibility.
type DoerCandidateReadiness struct {
	Agent               reference.RevisionRef  `json:"agent"`
	Candidate           reference.RevisionRef  `json:"candidate"`
	ContractDigest      string                 `json:"contract_digest,omitempty"`
	RequiredCharter     *reference.RevisionRef `json:"required_charter,omitempty"`
	CanAuthor           bool                   `json:"can_author"`
	CanExecute          bool                   `json:"can_execute"`
	Reason              string                 `json:"reason,omitempty"`
	RequiredAction      string                 `json:"required_action,omitempty"`
	Subject             DoerPrerequisiteStatus `json:"subject"`
	AgentReference      DoerPrerequisiteStatus `json:"agent_reference"`
	CanonicalCandidate  DoerPrerequisiteStatus `json:"canonical_candidate"`
	Charter             DoerPrerequisiteStatus `json:"charter"`
	Selection           DoerPrerequisiteStatus `json:"selection"`
	Model               DoerPrerequisiteStatus `json:"model"`
	ToolFree            DoerPrerequisiteStatus `json:"tool_free"`
	CredentialFree      DoerPrerequisiteStatus `json:"credential_free"`
	Receipt             DoerPrerequisiteStatus `json:"receipt"`
	Runtime             DoerPrerequisiteStatus `json:"runtime"`
	Controller          DoerPrerequisiteStatus `json:"controller"`
	Helper              DoerPrerequisiteStatus `json:"helper"`
	HelperQualification string                 `json:"helper_qualification"`
}

func newDoerReadiness() DoerCandidateReadiness {
	pending := DoerPrerequisiteStatus{State: "not_checked"}
	return DoerCandidateReadiness{Subject: pending, AgentReference: pending, CanonicalCandidate: pending, Charter: pending, Selection: pending, Model: pending, ToolFree: pending, CredentialFree: pending, Receipt: pending, Runtime: pending, Controller: pending, Helper: pending, HelperQualification: "protocol_availability_only_not_task_eligibility"}
}

func (r *DoerCandidateReadiness) block(status *DoerPrerequisiteStatus, reason string) {
	*status = DoerPrerequisiteStatus{State: "blocked", Reason: reason}
	r.Reason = reason
	diagnostic := QueueLoopResult{}
	diagnostic.blockedDoer(reason)
	r.RequiredAction = diagnostic.RequiredAction
}

// ReadDoerCandidateReadinessAs never authenticates a substitute subject or calls
// the local helper. Later prerequisites remain explicitly not_checked after a
// blocker, preserving queue preflight's foundational diagnostic ordering.
func (s *Service) ReadDoerCandidateReadinessAs(ctx context.Context, subject core.Subject, input DoerCandidateReadinessInput) (DoerCandidateReadiness, error) {
	return s.doerCandidateReadinessAs(ctx, subject, input, false)
}

// ProbeDoerCandidateReadinessAs additionally calls the bounded local helper
// protocol check. It does not judge the task, create authority, or reserve IDs.
func (s *Service) ProbeDoerCandidateReadinessAs(ctx context.Context, subject core.Subject, input DoerCandidateReadinessInput) (DoerCandidateReadiness, error) {
	return s.doerCandidateReadinessAs(ctx, subject, input, true)
}

func (s *Service) doerCandidateReadinessAs(ctx context.Context, subject core.Subject, input DoerCandidateReadinessInput, probe bool) (DoerCandidateReadiness, error) {
	r := newDoerReadiness()
	r.Agent = input.Agent
	ready := DoerPrerequisiteStatus{State: "ready"}
	// Do not require a Queue worker merely to author a registry-only definition.
	if ctx == nil || subject.ID == "" || subject.Kind == "" || subject.Issuer == "" || subject.Method == "" || subject.AuthenticatedAt.IsZero() || subject.ExpiresAt.IsZero() || !subject.AuthenticatedAt.Before(subject.ExpiresAt) || s.Now().Before(subject.AuthenticatedAt) || s.requirePrincipal(subject) != nil {
		r.block(&r.Subject, "authenticated_subject_required")
		return r, ErrDenied
	}
	r.Subject = ready
	if input.Agent.Validate() != nil {
		r.block(&r.AgentReference, "exact_latest_agent_required")
		return r, ErrDenied
	}
	if s.FleetRepository == nil || s.Fleet == nil {
		r.block(&r.AgentReference, "agent_registry_unavailable")
		return r, ErrFleetUnavailable
	}
	agent, err := s.FleetRepository.LatestAgentRevision(ctx, input.Agent.ID)
	if err != nil {
		r.block(&r.AgentReference, "exact_latest_agent_required")
		return r, err
	}
	if agent.AgentID != input.Agent.ID || agent.Revision != input.Agent.Revision || agent.Digest != input.Agent.Digest || agent.Lifecycle != registry.LifecycleEnabled {
		r.block(&r.AgentReference, "exact_latest_agent_required")
		return r, ErrDenied
	}
	r.AgentReference = ready
	candidate, _, err := loop.NewRevision(input.Candidate)
	if err != nil || input.Candidate.SchemaVersion != loop.DoerRevisionSchemaVersion || candidate.Digest != input.Candidate.Digest || candidate.Doer == nil {
		r.block(&r.CanonicalCandidate, "canonical_doer_candidate_required")
		return r, ErrDenied
	}
	r.Candidate = reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: candidate.LoopID, Revision: candidate.Revision, Digest: candidate.Digest}
	r.ContractDigest, err = candidate.Doer.Digest()
	if err != nil {
		r.block(&r.CanonicalCandidate, "canonical_doer_candidate_required")
		return r, ErrDenied
	}
	r.CanonicalCandidate = ready
	// Pure construction validates stable ownership, without persisting a delegation.
	if _, err := orchestration.NewRegisteredAgentWorkspaceAuthority(subject.PrincipalID, input.Agent, agent.Ownership.OwnerID); err != nil {
		r.block(&r.AgentReference, "registered_agent_workspace_required")
		return r, ErrDenied
	}
	r.CanAuthor = true
	r.RequiredCharter = &agent.Charter
	if probe {
		probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		s.checkDoerExecutionReadiness(probeCtx, subject, candidate, agent, true, &r)
	} else {
		s.checkDoerExecutionReadiness(ctx, subject, candidate, agent, false, &r)
	}
	return r, nil
}

// Shared execution checks intentionally retain preflight's ordering and reason
// codes. The caller performs its own immutable-reference and authority admission.
func (s *Service) checkDoerExecutionReadiness(ctx context.Context, subject core.Subject, candidate loop.LoopRevision, agent registry.AgentRevision, probe bool, r *DoerCandidateReadiness) {
	ready := DoerPrerequisiteStatus{State: "ready"}
	charter, err := s.GetCharter(agent.Charter.ID, agent.Charter.Revision)
	if err != nil || charter.Digest != agent.Charter.Digest {
		r.block(&r.Charter, "exact_charter_unavailable")
		return
	}
	r.Charter = ready
	selection, err := s.Select(charter, subject, "", core.Environment{Name: "local"})
	if err != nil || selection.Selected == nil {
		r.block(&r.Selection, "session_selection_"+selection.Reason)
		return
	}
	r.Selection = ready
	if selection.Selected.Hermes.Model == "" || selection.Selected.Hermes.Model == "none" {
		r.block(&r.Model, "doer_model_required")
		return
	}
	r.Model = ready
	if len(selection.Selected.Hermes.Toolsets) != 0 || len(selection.Selected.Grant.Tools) != 0 {
		r.block(&r.ToolFree, "doer_tool_free_authority_required")
		return
	}
	r.ToolFree = ready
	if len(selection.Selected.Scopes.Credentials) != 0 {
		r.block(&r.CredentialFree, "doer_tool_free_authority_required")
		return
	}
	r.CredentialFree = ready
	verified, err := s.hasVerifiedReceipt(agent.Charter.Digest)
	if err != nil {
		r.block(&r.Receipt, "provisioning_receipt_unavailable")
		return
	}
	if !verified {
		r.block(&r.Receipt, "provisioning_receipt_missing")
		return
	}
	r.Receipt = ready
	runtimeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	runtime, runtimeErr := s.Runtime(runtimeCtx)
	cancel()
	if runtimeErr != nil {
		r.block(&r.Runtime, "runtime_unavailable_or_unsupported")
		return
	}
	if runtimeSatisfies(runtime.Version, charter.Charter.Runtime.VersionConstraint) != nil {
		r.block(&r.Runtime, "runtime_version_unsupported")
		return
	}
	r.Runtime = ready
	if s.QueueWorker == nil {
		r.block(&r.Controller, "implementation_prerequisite_required")
		return
	}
	if !probe {
		if s.QueueWorker.ValidateLoopAdmission(candidate, agent) != nil {
			r.block(&r.Controller, "implementation_prerequisite_required")
			return
		}
		r.Controller = ready
		r.Reason = "helper_probe_required"
		return
	}
	err = s.QueueWorker.ValidateDoerAvailability(ctx, candidate, agent)
	if err != nil {
		if errors.Is(err, orchestration.ErrLocalLayaUnavailable) {
			r.Controller = ready
			r.block(&r.Helper, "local_laya_unavailable")
			return
		}
		r.block(&r.Controller, "implementation_prerequisite_required")
		return
	}
	r.Controller, r.Helper, r.CanExecute = ready, ready, true
}
