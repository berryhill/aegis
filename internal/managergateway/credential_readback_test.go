package managergateway

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/credentials"
	"testing"
)

type readbackFault struct {
	credentials.Repository
	fault string
	reads int
}

func (r *readbackFault) Metadata(ctx context.Context, id string) (credentials.SecretRecord, error) {
	r.reads++
	record, err := r.Repository.Metadata(ctx, id)
	if r.fault == "metadata" {
		return record, errors.New("private read failure")
	}
	if r.fault == "mismatch" {
		record.Kind = "different"
	}
	return record, err
}
func (r *readbackFault) History(ctx context.Context, id string, limit int) ([]credentials.SecretVersionMetadata, error) {
	if r.fault == "history" {
		return nil, errors.New("private history failure")
	}
	return r.Repository.History(ctx, id, limit)
}
func (r *readbackFault) BindingCount(ctx context.Context, id string) (int, error) {
	if r.fault == "bindings" {
		return 0, errors.New("private count failure")
	}
	return r.Repository.BindingCount(ctx, id)
}
func TestCredentialCreationIndependentlyReloadsExactMetadata(t *testing.T) {
	for _, fault := range []string{"", "metadata", "mismatch", "history", "bindings"} {
		t.Run(fault, func(t *testing.T) {
			var repo *readbackFault
			s, subject, token := intakeServiceWithRepository(t, func(r credentials.Repository) credentials.Repository {
				repo = &readbackFault{Repository: r, fault: fault}
				return repo
			})
			h := preparedIntake(t, s, subject, token)
			result, err := s.ConsumeCredentialIntake(context.Background(), subject, h.SessionID, token, h.OperationID, []byte("synthetic-value"))
			if err != nil || repo.reads != 1 {
				t.Fatalf("no independent read: %d %v", repo.reads, err)
			}
			if fault == "" {
				if result.Kind != "credential_created" || result.Data["metadata_verified"] != true {
					t.Fatal("success not verified")
				}
			} else {
				if result.Kind != "credential_creation_partial" || result.Data["metadata_verified"] != false || result.Data["audit_verified"] != true {
					t.Fatalf("false confirmation: %+v", result)
				}
			}
			records, err := repo.Repository.List(context.Background(), "", 100)
			if err != nil || len(records) != 1 {
				t.Fatal("commit missing")
			}
			if _, err = s.ConsumeCredentialIntake(context.Background(), subject, h.SessionID, token, h.OperationID, []byte("synthetic-value")); err == nil {
				t.Fatal("replay allowed")
			}
		})
	}
}
