package managergateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/credentials"
	credentialbolt "github.com/berryhill/aegis/internal/credentials/bbolt"
	"github.com/berryhill/aegis/internal/store"
)

func intakeService(t *testing.T) (*Service, core.Subject, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	state, err := store.Open(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(root, "key")
	if err = credentials.CreateHostKey(key, "test-key"); err != nil {
		t.Fatal(err)
	}
	custody, err := credentials.LoadFileCustodian(key)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := credentialbolt.Open(context.Background(), filepath.Join(root, "authority.db"), "test", custody)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close(); custody.Close() })
	cfg := config.Defaults()
	cfg.Principal.ID = "principal"
	cfg.StateDir = state.Root()
	cfg.Audit.CheckpointDir = state.CheckpointRoot()
	application := app.New(cfg, state, nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	application.CredentialAuthority = credentials.NewAuthority(repo, custody)
	subject := core.Subject{ID: "test-subject", PrincipalID: "principal", ExpiresAt: time.Now().Add(time.Hour)}
	material := make([]byte, 32)
	_, _ = rand.Read(material)
	token := hex.EncodeToString(material)
	clear(material)
	service, err := New(context.Background(), application)
	if err != nil {
		t.Fatal(err)
	}
	service.sessions["test-session"] = session{id: "test-session", token: sha256.Sum256([]byte(token)), subject: subject, expires: subject.ExpiresAt, mode: "degraded"}
	return service, subject, token
}

func preparedIntake(t *testing.T, s *Service, subject core.Subject, token string) CredentialIntakeHandoff {
	t.Helper()
	result, err := s.TurnWithProtectedIntake(context.Background(), subject, "test-session", token, "can we add a secret for a test?", true)
	if err != nil || result.Intake == nil {
		t.Fatalf("begin failed: %v", err)
	}
	h := *result.Intake
	for _, input := range []CredentialIntakeRequest{{OperationID: h.OperationID, Action: "review", Reference: "disposable", Kind: "opaque"}, {OperationID: h.OperationID, Action: "approve"}} {
		result, err = s.CredentialIntake(context.Background(), subject, h.SessionID, token, input)
		if err != nil {
			t.Fatal(err)
		}
	}
	return *result.Intake
}

func TestProtectedIntakeRequiresExactApprovalAndConsumesOnce(t *testing.T) {
	s, subject, token := intakeService(t)
	for _, phrase := range []string{"can we add a secret for a test?", "can we add a secret as a test"} {
		unsupported, err := s.Turn(context.Background(), subject, "test-session", token, phrase)
		if err != nil || unsupported.Intake != nil || unsupported.Kind != "credential_creation_guidance" {
			t.Fatal("unsupported client admitted")
		}
	}
	h := preparedIntake(t, s, subject, token)
	if _, err := s.CredentialIntake(context.Background(), subject, h.SessionID, token, CredentialIntakeRequest{OperationID: h.OperationID, Action: "review", Reference: "substituted", Kind: "opaque"}); err == nil {
		t.Fatal("metadata changed after approval")
	}
	random := make([]byte, 32)
	_, _ = rand.Read(random)
	value := []byte(hex.EncodeToString(random))
	clear(random)
	defer clear(value)
	var wg sync.WaitGroup
	successes := make(chan TurnResult, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := s.ConsumeCredentialIntake(context.Background(), subject, h.SessionID, token, h.OperationID, append([]byte(nil), value...))
			if err == nil {
				successes <- result
			}
		}()
	}
	wg.Wait()
	close(successes)
	if len(successes) != 1 {
		t.Fatalf("successes=%d want 1", len(successes))
	}
	result := <-successes
	raw, _ := json.Marshal(result)
	if containsBytes(raw, value) || result.Data["created"] != true {
		t.Fatal("invalid metadata-only receipt")
	}
	records, err := s.app.CredentialAuthority.List(context.Background(), "", 100)
	if err != nil || len(records) != 1 || records[0].Reference != "disposable" {
		t.Fatal("canonical readback failed")
	}
	// Plaintext must not enter retained application/audit state.
	err = filepath.WalkDir(s.app.Config.StateDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		body, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		if containsBytes(body, value) {
			t.Error("protected value found in application state")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func containsBytes(haystack, needle []byte) bool {
	// Keep canaries out of failure formatting.
	return len(needle) > 0 && bytes.Contains(haystack, needle)
}

func TestProtectedIntakeNegativePathsDoNotWrite(t *testing.T) {
	for _, scenario := range []string{"unapproved", "wrong-session", "wrong-token", "wrong-operation", "expired", "cancelled", "revoked", "empty", "oversize", "wrong-principal", "cancelled-context"} {
		t.Run(scenario, func(t *testing.T) {
			s, subject, token := intakeService(t)
			h := preparedIntake(t, s, subject, token)
			id, operation := h.SessionID, h.OperationID
			value := []byte{1, 2, 3}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch scenario {
			case "oversize":
				value = make([]byte, MaximumProtectedValueBytes+1)
			case "wrong-principal":
				subject.PrincipalID = "other-principal"
			case "cancelled-context":
				cancel()
			case "unapproved":
				current := s.sessions[id]
				current.intake.handoff.Stage = "review"
				s.sessions[id] = current
			case "wrong-session":
				id = "other"
			case "wrong-token":
				token = "invalid"
			case "wrong-operation":
				operation = "invalid"
			case "expired":
				s.now = func() time.Time { return h.ExpiresAt.Add(time.Second) }
			case "cancelled":
				if _, err := s.CredentialIntake(context.Background(), subject, id, token, CredentialIntakeRequest{OperationID: operation, Action: "cancel"}); err != nil {
					t.Fatal(err)
				}
			case "revoked":
				delete(s.sessions, id)
			case "empty":
				value = nil
			}
			if _, err := s.ConsumeCredentialIntake(ctx, subject, id, token, operation, value); err == nil {
				t.Fatal("invalid intake accepted")
			}
			records, err := s.app.CredentialAuthority.List(context.Background(), "", 100)
			if err != nil || len(records) != 0 {
				t.Fatal("denial wrote custody")
			}
		})
	}
}
