package managergateway

import (
	"context"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
)

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
