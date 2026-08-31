package managergateway

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	managerdomain "github.com/berryhill/aegis/internal/manager"
)

type closeAuditFixture struct {
	ctxErr error
	err    error
}

func (a *closeAuditFixture) AppendAudit(ctx context.Context, _ core.AuditEvent) error {
	a.ctxErr = ctx.Err()
	return a.err
}
func (*closeAuditFixture) AuditEvents() ([]core.AuditEvent, error) { return nil, nil }
func (*closeAuditFixture) VerifyAudit() error                      { return nil }

func TestServiceDetectsExistingConversationalSession(t *testing.T) {
	now := time.Now().UTC()
	svc := &Service{sessions: map[string]session{
		"active": {expires: now.Add(time.Minute), subject: core.Subject{ExpiresAt: now.Add(time.Minute)}, runtime: &conversation{}},
	}}
	if !svc.hasActiveConversation(now) {
		t.Fatal("active conversational session was not reserved")
	}
	svc.sessions["active"] = session{expires: now.Add(-time.Second), runtime: &conversation{}}
	if svc.hasActiveConversation(now) {
		t.Fatal("expired conversational session retained admission")
	}
}

func TestServiceRetiresExpiredConversationBeforeAdmission(t *testing.T) {
	now := time.Now().UTC()
	runtimeCtx, cancel := context.WithCancel(context.Background())
	runtime := &conversation{ctx: runtimeCtx, cancel: cancel, failures: make(chan error, 1)}
	svc := &Service{
		app: &app.Service{Config: config.Config{Manager: config.Manager{CleanupTimeout: time.Second}}},
		sessions: map[string]session{
			"expired": {expires: now.Add(-time.Second), subject: core.Subject{ExpiresAt: now.Add(-time.Second)}, runtime: runtime},
		},
	}
	if err := svc.closeExpiredConversations(now); err != nil {
		t.Fatalf("retire expired conversation: %v", err)
	}
	if _, exists := svc.sessions["expired"]; exists {
		t.Fatal("expired conversation remained registered")
	}
	if runtime.ctx.Err() == nil {
		t.Fatal("expired conversation runtime remained active")
	}
}

func TestRuntimeFailureDegradesSessionWithoutRevokingDeterministicAuthority(t *testing.T) {
	now := time.Now().UTC()
	subject := core.Subject{ID: "subject", PrincipalID: "principal", ExpiresAt: now.Add(time.Minute)}
	runtimeCtx, cancel := context.WithCancel(context.Background())
	runtime := &conversation{ctx: runtimeCtx, cancel: cancel, failures: make(chan error, 1)}
	runtime.active.Store(true)
	entry := session{
		id: "mgr", token: sha256.Sum256([]byte("capability")), subject: subject,
		issuedAt: now, expires: now.Add(time.Minute), mode: "conversational", runtime: runtime,
	}
	svc := &Service{
		app:      &app.Service{Config: config.Config{Manager: config.Manager{CleanupTimeout: time.Second}}},
		now:      func() time.Time { return now },
		ctx:      context.Background(),
		sessions: map[string]session{entry.id: entry},
	}
	done := make(chan struct{})
	go func() {
		svc.watchSession(entry)
		close(done)
	}()
	runtime.failures <- errors.New("hermes exited")
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runtime failure did not settle manager session")
	}

	current, err := svc.authenticate(subject, entry.id, "capability")
	if err != nil {
		t.Fatalf("runtime failure revoked deterministic manager authority: %v", err)
	}
	if current.mode != "degraded" || current.runtime != nil || current.reason != "manager_runtime_failed" {
		t.Fatalf("session did not become typed degraded state: %+v", current)
	}
}

func TestTurnFailureDegradesSessionWithoutRevokingDeterministicAuthority(t *testing.T) {
	now := time.Now().UTC()
	subject := core.Subject{ID: "subject", PrincipalID: "principal", ExpiresAt: now.Add(time.Minute)}
	tokenValue := "turn-failure-session-token-value"
	token := sha256.Sum256([]byte(tokenValue))
	runtimeCtx, cancel := context.WithCancel(context.Background())
	runtime := &conversation{ctx: runtimeCtx, cancel: cancel, failures: make(chan error, 1)}
	runtime.active.Store(true)
	entry := session{
		id: "session", token: token, subject: subject,
		issuedAt: now, expires: subject.ExpiresAt, mode: "conversational", runtime: runtime,
	}
	cfg := config.Defaults()
	cfg.Manager.CleanupTimeout = time.Second
	svc := &Service{
		app:      &app.Service{Config: cfg},
		ctx:      context.Background(),
		now:      func() time.Time { return now },
		sessions: map[string]session{entry.id: entry},
	}

	if _, err := svc.Turn(context.Background(), subject, entry.id, tokenValue, "hello"); !errors.Is(err, ErrTurnRuntimeUnavailable) {
		t.Fatalf("turn error=%v, want runtime unavailable", err)
	}
	current, err := svc.authenticate(subject, entry.id, tokenValue)
	if err != nil {
		t.Fatalf("deterministic session authority was revoked: %v", err)
	}
	if current.mode != "degraded" || current.runtime != nil || current.reason != managerdomain.ReasonRuntimeFailed {
		t.Fatalf("turn failure did not degrade session: %+v", current)
	}
}

func TestManagerReadinessReflectsCredentialAuthorityState(t *testing.T) {
	for _, test := range []struct {
		reason string
		open   bool
		want   string
	}{
		{reason: "", open: true, want: "ready"},
		{reason: managerdomain.ReasonRuntimeFailed, open: true, want: "ready"},
		{reason: managerdomain.ReasonModelAbsent, open: false, want: "unavailable"},
		{reason: managerdomain.ReasonAuthorityUnavailable, open: false, want: "unavailable"},
		{reason: managerdomain.ReasonAuthorityInvalid, open: true, want: "invalid"},
	} {
		if got := managerReadiness(session{reason: test.reason}, test.open)["authority"]; got != test.want {
			t.Fatalf("reason=%q authority=%q want=%q", test.reason, got, test.want)
		}
	}
}

func TestManagerTurnContextEnforcesConfiguredDeadline(t *testing.T) {
	ctx, cancel := managerTurnContext(context.Background(), 10*time.Millisecond)
	defer cancel()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("manager turn exceeded configured deadline")
	}
}

func TestBindTurnContextCancelsWithRuntimeLifetime(t *testing.T) {
	runtimeCtx, stopRuntime := context.WithCancel(context.Background())
	requestCtx, stopRequest := context.WithCancel(context.Background())
	turnCtx, release := bindTurnContext(requestCtx, runtimeCtx)
	defer release()

	stopRuntime()
	select {
	case <-turnCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("turn continued after runtime mandate cancellation")
	}
	stopRequest()
}

func TestBindTurnContextCancelsWithRequest(t *testing.T) {
	runtimeCtx, stopRuntime := context.WithCancel(context.Background())
	defer stopRuntime()
	requestCtx, stopRequest := context.WithCancel(context.Background())
	turnCtx, release := bindTurnContext(requestCtx, runtimeCtx)
	defer release()

	stopRequest()
	select {
	case <-turnCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("turn continued after request cancellation")
	}
}

func TestServiceCloseUsesOwnedContextAndJoinsCleanupErrors(t *testing.T) {
	now := time.Now().UTC()
	subject := core.Subject{ID: "subject", PrincipalID: "principal", ExpiresAt: now.Add(time.Minute)}
	runtimeErr := errors.New("runtime close failed")
	auditErr := errors.New("audit close failed")
	runtime := &conversation{}
	runtime.closeOnce = sync.Once{}
	runtime.closeOnce.Do(func() { runtime.closeErr = runtimeErr })
	audit := &closeAuditFixture{err: auditErr}
	service := &app.Service{Config: config.Config{Principal: config.Principal{ID: "principal"}, Manager: config.Manager{CleanupTimeout: time.Second}}, Audit: audit}
	svc := &Service{app: service, now: func() time.Time { return now }, sessions: map[string]session{"mgr": {id: "mgr", token: sha256.Sum256([]byte("capability")), subject: subject, expires: now.Add(time.Minute), runtime: runtime}}}
	requestCtx, cancel := context.WithCancel(context.Background())
	cancel()
	err := svc.Close(requestCtx, subject, "mgr", "capability")
	if !errors.Is(err, runtimeErr) || !errors.Is(err, auditErr) {
		t.Fatalf("close errors not joined: %v", err)
	}
	if audit.ctxErr != nil {
		t.Fatalf("audit inherited canceled request: %v", audit.ctxErr)
	}
	if _, exists := svc.sessions["mgr"]; exists {
		t.Fatal("session capability was not revoked")
	}
	if !errors.Is(svc.cleanupErr, runtimeErr) || !svc.hasActiveConversation(now) {
		t.Fatal("incomplete runtime cleanup did not block replacement conversation admission")
	}
}

func TestConversationCloseUnloadsOnlyAegisOwnedExternalRunner(t *testing.T) {
	const model = "exact:model"
	digest := "sha256:" + strings.Repeat("a", 64)
	for _, test := range []struct {
		name      string
		ownership managerdomain.ModelCleanupOwnership
		want      int
	}{
		{name: "owned", ownership: managerdomain.ModelCleanupAegisOwned, want: 1},
		{name: "shared", ownership: managerdomain.ModelCleanupShared},
		{name: "unknown", ownership: managerdomain.ModelCleanupUnknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			unloads := 0
			running := true
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case "/api/ps":
					models := []any{}
					if running {
						models = append(models, map[string]any{"name": model, "digest": digest})
					}
					_ = json.NewEncoder(writer).Encode(map[string]any{"models": models})
				case "/api/generate":
					unloads++
					running = false
					_ = json.NewEncoder(writer).Encode(map[string]any{"done": true})
				default:
					http.NotFound(writer, request)
				}
			}))
			defer server.Close()
			client, err := managerdomain.NewOllamaClient(server.URL, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			runtime := &conversation{ollama: client, model: model, modelDigest: digest, modelCleanup: test.ownership}
			if err = runtime.Close(context.Background(), managerdomain.EndUserExit); err != nil {
				t.Fatal(err)
			}
			if unloads != test.want {
				t.Fatalf("unloads=%d want=%d", unloads, test.want)
			}
		})
	}
}
