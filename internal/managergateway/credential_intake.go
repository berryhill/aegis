package managergateway

import (
	"context"
	"errors"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/credentials"
)

// Domain contract: the model cannot approve, receive values, or write custody.
// One authenticated session owns one bounded metadata-only operation. Review
// freezes metadata, explicit approval enables intake, and consumption precedes
// every write (including writes whose final audit/readback fails). No automatic
// retry is safe after an uncertain transport result. Session removal/restart
// invalidates all outstanding operations; no values are stored in this state.
const ProtectedIntakeProtocol = "aegis.credential-intake.v1"
const ProtectedIntakeHeader = "X-Aegis-Protected-Intake"
const MaximumProtectedValueBytes = 1 << 20

type CredentialIntakeRequest struct {
	OperationID string `json:"operation_id"`
	Action      string `json:"action"`
	Reference   string `json:"reference,omitempty"`
	Kind        string `json:"kind,omitempty"`
}

type CredentialIntakeHandoff struct {
	Protocol    string    `json:"protocol"`
	OperationID string    `json:"operation_id"`
	SessionID   string    `json:"session_id"`
	PrincipalID string    `json:"principal_id"`
	ExpiresAt   time.Time `json:"expires_at"`
	Stage       string    `json:"stage"`
	Reference   string    `json:"reference,omitempty"`
	Kind        string    `json:"kind,omitempty"`
}

type credentialIntake struct{ handoff CredentialIntakeHandoff }

func intakeResult(h CredentialIntakeHandoff) TurnResult {
	return TurnResult{Kind: "credential_protected_intake", Origin: TurnOriginAuthoritative,
		Message: "Aegis protected credential creation: collect non-secret metadata, review and explicitly approve before value intake. No credential has been created.",
		Intake:  &h, Data: map[string]any{"created": false, "model_bypassed": true, "protected_intake_available": true}}
}

func (s *Service) intakeAuthority(ctx context.Context, subject core.Subject) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.app.RequirePrincipal(subject); err != nil {
		return err
	}
	if s.app.CredentialAuthority == nil {
		return app.ErrCredentialUnavailable
	}
	if _, err := s.app.CredentialAuthority.Status(ctx); err != nil {
		return app.ErrCredentialUnavailable
	}
	return nil
}

func (s *Service) beginCredentialIntake(ctx context.Context, entry session) (TurnResult, error) {
	if err := s.intakeAuthority(ctx, entry.subject); err != nil {
		return TurnResult{}, err
	}
	operation, err := opaque(24)
	if err != nil {
		return TurnResult{}, errors.New("protected intake identity unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.sessions[entry.id]
	if !ok || current.token != entry.token || !s.now().Before(current.expires) {
		return TurnResult{}, app.ErrUnauthenticated
	}
	// A repeated initiating phrase does not create a second outstanding operation.
	if current.intake != nil && s.now().Before(current.intake.handoff.ExpiresAt) {
		return intakeResult(current.intake.handoff), nil
	}
	expires := s.now().Add(5 * time.Minute)
	if current.expires.Before(expires) {
		expires = current.expires
	}
	h := CredentialIntakeHandoff{Protocol: ProtectedIntakeProtocol, OperationID: operation, SessionID: current.id, PrincipalID: current.subject.PrincipalID, ExpiresAt: expires, Stage: "metadata"}
	current.intake = &credentialIntake{handoff: h}
	current.credentialContextUntil = expires
	s.sessions[entry.id] = current
	return intakeResult(h), nil
}

// CredentialIntake handles metadata only. No natural-language confirmation or
// caller-supplied subject, expiry, stage, or value is accepted by this protocol.
func (s *Service) CredentialIntake(ctx context.Context, subject core.Subject, id, token string, input CredentialIntakeRequest) (TurnResult, error) {
	entry, err := s.authenticate(subject, id, token)
	if err != nil {
		return TurnResult{}, err
	}
	if err = s.intakeAuthority(ctx, entry.subject); err != nil {
		return TurnResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.sessions[id]
	if !ok || current.token != entry.token || current.intake == nil {
		return TurnResult{}, app.ErrDenied
	}
	h := current.intake.handoff
	if input.OperationID != h.OperationID || !s.now().Before(h.ExpiresAt) {
		return TurnResult{}, app.ErrDenied
	}
	switch input.Action {
	case "review":
		if h.Stage != "metadata" || !credentials.ValidateIdentifier(input.Reference) || !credentials.ValidateIdentifier(input.Kind) {
			return TurnResult{}, app.ErrDenied
		}
		h.Reference, h.Kind, h.Stage = input.Reference, input.Kind, "review"
	case "approve":
		if h.Stage != "review" || input.Reference != "" || input.Kind != "" {
			return TurnResult{}, app.ErrDenied
		}
		h.Stage = "intake"
	case "cancel":
		if input.Reference != "" || input.Kind != "" {
			return TurnResult{}, app.ErrDenied
		}
		current.intake = nil
		s.sessions[id] = current
		return TurnResult{Kind: "credential_creation_cancelled", Origin: TurnOriginAuthoritative, Message: "Credential creation cancelled; no write was attempted.", Data: map[string]any{"created": false}}, nil
	default:
		return TurnResult{}, app.ErrDenied
	}
	current.intake = &credentialIntake{handoff: h}
	s.sessions[id] = current
	return intakeResult(h), nil
}

// ConsumeCredentialIntake receives only bounded raw bytes on the dedicated
// protected route, never the ordinary turn decoder. Callers must wipe their
// transport buffers. Holding the session lock serializes cancellation/close and
// consumption; the canonical service repeats principal admission before write.
func (s *Service) ConsumeCredentialIntake(ctx context.Context, subject core.Subject, id, token, operation string, value []byte) (TurnResult, error) {
	defer clear(value)
	entry, err := s.authenticate(subject, id, token)
	if err != nil {
		return TurnResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.sessions[id]
	if !ok || current.token != entry.token || current.intake == nil {
		return TurnResult{}, app.ErrDenied
	}
	h := current.intake.handoff
	if operation != h.OperationID || h.Stage != "intake" || !s.now().Before(h.ExpiresAt) {
		return TurnResult{}, app.ErrDenied
	}
	current.intake = nil
	s.sessions[id] = current
	ctx, cancel := context.WithDeadline(ctx, h.ExpiresAt)
	defer cancel()
	if len(value) == 0 || len(value) > MaximumProtectedValueBytes {
		return TurnResult{}, app.ErrDenied
	}
	if err = s.intakeAuthority(ctx, entry.subject); err != nil {
		return TurnResult{}, err
	}
	view, err := s.app.CreateCredentialAs(ctx, entry.subject, app.CreateCredentialInput{Reference: h.Reference, Kind: h.Kind, Value: value})
	if err != nil {
		var partial *app.CredentialCreationPartial
		if errors.As(err, &partial) {
			return TurnResult{Kind: "credential_creation_partial", Origin: TurnOriginAuthoritative, Message: "Credential persisted; audit or metadata confirmation is incomplete. Do not replay the value. Inspect metadata before any new operation.", Data: map[string]any{"created": true, "record_id": partial.RecordID, "reference": partial.Reference, "kind": partial.Kind, "operation_id": operation, "audit_verified": partial.AuditVerified, "metadata_verified": partial.MetadataVerified, "model_bypassed": true}}, nil
		}
		return TurnResult{}, errors.New("credential creation not confirmed; operation consumed; do not replay the value; inspect credential metadata before starting a new operation")
	}
	return TurnResult{Kind: "credential_created", Origin: TurnOriginAuthoritative, Message: "Credential created and metadata independently verified. Returning to the same conversation.", Data: map[string]any{"created": true, "record_id": view.ID, "reference": view.Reference, "kind": view.Kind, "operation_id": operation, "audit_verified": true, "metadata_verified": true, "model_bypassed": true}}, nil
}
