package app

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/runtime/hermes"
	"github.com/berryhill/aegis/internal/store"
	"strings"
	"time"
)

// Probe is a separate explicit inference diagnostic, not a Loop execution.
// Exact definitions select approved zero-authority runtime/model configuration;
// no task inputs, prompt, path, source, model or authority override is accepted.
type ControllerCodexProbeInput struct {
	Agent          reference.RevisionRef `json:"agent"`
	Loop           reference.RevisionRef `json:"loop"`
	IdempotencyKey string                `json:"idempotency_key"`
}
type ControllerCodexProbeResult struct {
	ID          string                          `json:"id"`
	Input       ControllerCodexProbeInput       `json:"input"`
	Status      string                          `json:"status"`
	Reason      string                          `json:"reason"`
	Diagnostic  hermes.ProviderFormatDiagnostic `json:"diagnostic"`
	PrincipalID string                          `json:"principal_id"`
}

func controllerProviderProbeID(principal, key string) string {
	return "provider-probe-" + strings.TrimPrefix(core.Digest(struct{ Principal, Key string }{principal, key}), "sha256:")
}

func (s *Service) providerProbeAllowed(ctx context.Context, sub core.Subject, in ControllerCodexProbeInput) error {
	cfg, e := s.effectiveControllerCodexConfig()
	if e != nil || cfg.Credentials.ControllerCodexLogin == nil {
		return ErrDenied
	}
	if s.nativeConnectionAdmission(sub, cfg.Credentials.ControllerCodexLogin.ProfileHome) != nil {
		return ErrDenied
	}
	if in.Agent.Validate() != nil || in.Loop.Validate() != nil || len(in.IdempotencyKey) < 1 || len(in.IdempotencyKey) > 128 {
		return ErrDenied
	}
	for _, c := range in.IdempotencyKey {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return ErrDenied
		}
	}
	agent, e := s.hostApprovalAgent(ctx, in.Agent)
	if e != nil {
		return ErrDenied
	}
	candidate, e := s.FleetRepository.GetLoopRevision(ctx, in.Loop.ID, in.Loop.Revision)
	if e != nil || candidate.Digest != in.Loop.Digest || !s.doerEphemeralPreparationAllowed(ctx, sub, candidate, agent) {
		return ErrDenied
	}
	charter, e := s.GetCharter(agent.Charter.ID, agent.Charter.Revision)
	if e != nil {
		return ErrDenied
	}
	d, e := s.Select(charter, sub, "", core.Environment{Name: "local"})
	if e != nil || d.Selected == nil || d.Selected.Hermes.ProviderAuthentication == nil || (d.Selected.Hermes.Provider != "codex" && d.Selected.Hermes.Provider != "openai-codex") {
		return ErrDenied
	}
	return nil
}

func (s *Service) ProbeControllerCodexAs(ctx context.Context, sub core.Subject, in ControllerCodexProbeInput) (out ControllerCodexProbeResult, err error) {
	if e := s.providerProbeAllowed(ctx, sub, in); e != nil {
		return out, e
	}
	id := controllerProviderProbeID(sub.PrincipalID, in.IdempotencyKey)
	out = ControllerCodexProbeResult{ID: id, Input: in, Status: "in_progress", Reason: "diagnostic_reserved", PrincipalID: sub.PrincipalID}
	// Durable create-only fence: interruption never automatically repeats inference.
	if e := s.Store.Create("controller-provider-probes", id, out); e != nil {
		var previous ControllerCodexProbeResult
		if s.Store.Load("controller-provider-probes", id, &previous) != nil || previous.Input != in || previous.PrincipalID != sub.PrincipalID {
			return out, ErrConflict
		}
		return previous, nil
	}
	defer func() {
		if err != nil {
			out.Status = "failed"
			out.Reason = "probe_preparation_or_cleanup_failed"
		}
		if e := s.Store.Save("controller-provider-probes", id, out); e != nil {
			err = e
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	agent, e := s.hostApprovalAgent(ctx, in.Agent)
	if e != nil {
		return out, e
	}
	// Same checked zero-authority ephemeral policy as Run, without Queue, Graph,
	// host-write scope, activation, implementation, budget or evidence operations.
	mandate, _, e := s.previewSessionAs(ctx, sub, agent.Charter.ID, agent.Charter.Revision, "", core.Environment{Name: "local"}, true)
	if e != nil {
		return out, e
	}
	session, e := s.StartSessionAs(ctx, sub, mandate.ID)
	if e != nil {
		return out, e
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if e := s.endSession(cleanup, session.ID, "terminated", "provider_format_probe_complete", true); e != nil {
			err = ErrDenied
		}
	}()
	authority, e := s.authorityContextForSession(ctx, session.ID)
	if e != nil {
		return out, e
	}
	now := s.Now().UTC()
	dispatch := execution.Dispatch{ID: store.ID("provider-probe-dispatch"), AuthorityContextID: authority.ID, State: execution.StateSucceeded, RequestedAt: session.StartedAt, StartedAt: &session.StartedAt, FinishedAt: &now, Reason: "clean_diagnostic_session_started"}
	if e := s.Store.Create("controller-provider-probe-dispatches", dispatch.ID, dispatch); e != nil {
		return out, e
	}
	launch := execution.LaunchContract{OwnerID: agent.Ownership.OwnerID, Mandate: mandate, AuthorityContext: authority, ParentDispatch: dispatch}
	result, e := s.Hermes.AttemptTurn(ctx, hermes.AttemptTurnRequest{Launch: launch, Admission: providerProbeAdmission{s, sub, in, dispatch.ID}, AttemptID: store.ID("provider-probe-turn"), ParentAttemptID: dispatch.ID, StateRoot: s.Store.Root(), Input: hermes.ProviderFormatPrompt, Model: mandate.Hermes.Model, Provider: mandate.Hermes.Provider, Bounds: hermes.AttemptBounds{InputBytes: 1024, OutputBytes: 4096, Duration: 60 * time.Second}, ProviderFormatProbe: true})
	out.Status = "failed"
	out.Reason = "probe_runtime_failed"
	if hermes.IsAttemptFailureReason(result.Attempt.Reason) {
		out.Reason = result.Attempt.Reason
	}
	out.Diagnostic = result.Diagnostic
	if e == nil && result.Attempt.State == execution.StateSucceeded {
		out.Status = "completed"
		out.Reason = "diagnostic_turn_completed_not_acceptance"
	}
	// Never return native output, SDK text, arbitrary errors or a completion claim.
	if e = s.audit(ctx, core.AuditEvent{Type: "controller.provider-format-probe", SubjectID: sub.ID, PrincipalID: sub.PrincipalID, AgentID: agent.AgentID, Outcome: out.Status, Reason: out.Reason}); e != nil {
		return out, e
	}
	return out, nil
}

type providerProbeAdmission struct {
	service    *Service
	subject    core.Subject
	input      ControllerCodexProbeInput
	dispatchID string
}

func (p providerProbeAdmission) CheckRuntimeAdmission(ctx context.Context, launch execution.LaunchContract, at time.Time) (execution.AdmissionDecision, error) {
	deny := execution.AdmissionDecision{CheckedAt: at}
	s := p.service
	if e := s.providerProbeAllowed(ctx, p.subject, p.input); e != nil {
		return deny, ErrDenied
	}
	a, e := s.Authority.GetAuthorityContext(ctx, launch.AuthorityContext.ID)
	if e != nil || a.Digest != launch.AuthorityContext.Digest || a.SubjectID != p.subject.ID || s.validateMandate(launch.Mandate) != nil {
		return deny, ErrDenied
	}
	var dispatch execution.Dispatch
	if s.Store.Load("controller-provider-probe-dispatches", p.dispatchID, &dispatch) != nil || core.Digest(dispatch) != core.Digest(launch.ParentDispatch) {
		return deny, ErrDenied
	}
	session, e := s.GetSession(a.SessionID)
	if e != nil || session.Mandate.ID != launch.Mandate.ID || session.Status != "running" || !processMatches(session.RuntimePID, session.ProcessStart) {
		return deny, ErrDenied
	}
	admission, e := s.AuthorityCommands.AuthorityAdmission(ctx, a.ID, a.Digest, at)
	if e != nil || !admission.Admitted {
		return deny, errors.Join(ErrDenied, e)
	}
	return execution.AdmissionDecision{Allowed: true, AuthorityContextID: a.ID, AuthorityContextDigest: a.Digest, CheckedAt: at}, nil
}
