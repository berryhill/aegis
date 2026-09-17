package managergateway

import (
	"context"
	"errors"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
)

type brokenCredentialAudit struct{ app.AuditAuthority }

func (brokenCredentialAudit) AppendAudit(context.Context, core.AuditEvent) error {
	return errors.New("private backend diagnostic")
}

func TestCredentialPostCommitAuditFailureReceiptAndNoReplay(t *testing.T) {
	s, subject, token := intakeService(t)
	h := preparedIntake(t, s, subject, token)
	s.app.Audit = brokenCredentialAudit{s.app.Audit}
	result, err := s.ConsumeCredentialIntake(context.Background(), subject, h.SessionID, token, h.OperationID, []byte("synthetic-value"))
	if err != nil || result.Kind != "credential_creation_partial" || result.Data["created"] != true || result.Data["audit_verified"] != false || result.Data["metadata_verified"] != true {
		t.Fatalf("missing sanitized partial receipt: %+v %v", result, err)
	}
	if _, err := s.ConsumeCredentialIntake(context.Background(), subject, h.SessionID, token, h.OperationID, []byte("synthetic-value")); err == nil {
		t.Fatal("replay accepted")
	}
	records, err := s.app.ListCredentialsAs(context.Background(), subject)
	if err != nil || len(records) != 1 || records[0].CurrentVersion != 1 || result.Data["record_id"] != records[0].ID {
		t.Fatal("exact single persisted record missing")
	}
}

func TestCredentialFollowupIsBoundedAndControlIsAuthoritative(t *testing.T) {
	s, subject, token := intakeService(t)
	result, err := s.TurnWithProtectedIntake(context.Background(), subject, "test-session", token, "let’s amke another one", true)
	if err != nil || result.Kind != "credential_context_required" || result.Intake != nil {
		t.Fatalf("ambiguous followup not safely denied: %+v %v", result, err)
	}
	preparedIntake(t, s, subject, token)
	result, err = s.TurnWithProtectedIntake(context.Background(), subject, "test-session", token, "let’s amke another one", true)
	if err != nil || result.Intake == nil {
		t.Fatalf("bounded followup failed: %+v %v", result, err)
	}
	result, err = s.Turn(context.Background(), subject, "test-session", token, "secret.propose_create")
	if err != nil || result.Origin != TurnOriginAuthoritative || result.Kind != "credential_control_guidance" {
		t.Fatalf("control reached model: %+v %v", result, err)
	}
}
