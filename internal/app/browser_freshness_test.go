package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
)

// Trusted transports may carry a longer password identity, never longer
// sensitive authority. Non-browser subjects retain their existing expiry.
func TestBrowserPrincipalFreshnessIndependentOfIdentity(t *testing.T) {
	start := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	now := start
	cfg := config.Defaults()
	svc := &Service{Config: cfg, Now: func() time.Time { return now }}
	subject := core.Subject{PrincipalID: cfg.Principal.ID, Method: "password", AuthenticatedAt: start, ExpiresAt: start.Add(time.Hour)}
	for _, elapsed := range []time.Duration{5 * time.Minute, 15*time.Minute - time.Nanosecond, 15 * time.Minute, 16 * time.Minute, time.Hour - time.Nanosecond, time.Hour} {
		now = start.Add(elapsed)
		if err := svc.RequirePrincipalIdentity(subject); (err == nil) != (elapsed < time.Hour) {
			t.Fatalf("identity at %s: %v", elapsed, err)
		}
		if err := svc.RequirePrincipal(subject); (err == nil) != (elapsed < 15*time.Minute) {
			t.Fatalf("sensitive admission at %s: %v", elapsed, err)
		}
	}
	now = start
	assertNoRuntimeAuthority := func(sub core.Subject) {
		t.Helper()
		// No repository/runtime is configured: reaching either would panic.
		if _, _, err := svc.PreviewSessionAs(context.Background(), sub, "agent", 1, "", core.Environment{}); !errors.Is(err, ErrDenied) {
			t.Fatalf("preview admitted stale password: %v", err)
		}
		if _, err := svc.StartSessionAs(context.Background(), sub, "mandate"); !errors.Is(err, ErrDenied) {
			t.Fatalf("start admitted stale password: %v", err)
		}
		if err := svc.TerminateSessionAs(context.Background(), sub, "session", "test"); !errors.Is(err, ErrDenied) {
			t.Fatalf("termination admitted stale password: %v", err)
		}
	}
	now = start.Add(15 * time.Minute)
	assertNoRuntimeAuthority(subject)
	now = start
	for _, authenticatedAt := range []time.Time{{}, start.Add(time.Second)} {
		invalid := subject
		invalid.AuthenticatedAt = authenticatedAt
		assertNoRuntimeAuthority(invalid)
		if err := svc.RequirePrincipal(invalid); !errors.Is(err, ErrDenied) {
			t.Fatalf("invalid freshness accepted: %v", err)
		}
	}
	for _, principal := range []string{"", "other-principal"} {
		invalid := subject
		invalid.PrincipalID = principal
		if err := svc.RequirePrincipalIdentity(invalid); !errors.Is(err, ErrDenied) {
			t.Fatalf("wrong principal accepted: %v", err)
		}
	}
	local := subject
	local.Method = "unix-peer"
	local.ExpiresAt = start.Add(15 * time.Minute)
	now = local.ExpiresAt
	if err := svc.RequirePrincipalIdentity(local); !errors.Is(err, ErrDenied) {
		t.Fatalf("local subject expiry extended: %v", err)
	}
}
