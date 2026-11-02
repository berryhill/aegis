package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
)

func supervisorShutdownFixture(t *testing.T) (*Service, core.Session, core.AuthorityContext) {
	t.Helper()
	s := testService(t)
	ctx := context.Background()
	cc, err := core.Canonicalize(testCharter(s.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Store.SaveCharter(cc); err != nil {
		t.Fatal(err)
	}
	if err = s.Store.Save("receipts", "ready", core.Receipt{ID: "ready", CharterDigest: cc.Digest, Status: "verified"}); err != nil {
		t.Fatal(err)
	}
	mandate, _, err := s.PreviewSession(ctx, "office", 1, "principal", core.Environment{Name: "local"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.StartSession(ctx, mandate.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Hermes.Terminate(context.Background(), session.RuntimeSessionID, false) })
	authority, err := s.authorityContextForSession(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The fixture runtime has exited, while canonical lifecycle reconciliation is
	// still pending: exactly the regular pass that must be handed to shutdown.
	if err = s.Hermes.Terminate(ctx, session.RuntimeSessionID, false); err != nil {
		t.Fatal(err)
	}
	return s, session, authority
}

// Reproduce owned cancellation during a real regular-pass terminal-lock wait.
// No listener, provider, external identity, or production test hook is involved.
func TestSupervisorCancellationDuringReconciliationCompletesShutdown(t *testing.T) {
	s, session, authority := supervisorShutdownFixture(t)
	ctx := context.Background()
	unlock, err := s.lockSessionTerminal(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if unlock != nil {
			unlock()
		}
	}()
	owned, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Supervise(owned) }()
	waitTerminalContenders(t, s, session.ID, 2)
	cancel()
	unlock()
	unlock = nil
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("owned cancellation bypassed shutdown reconciliation: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("supervisor did not join shutdown")
	}
	terminal, err := s.GetSession(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if terminal.Status != "terminated" || terminal.EndReason != "control_plane_shutdown" || terminal.EndedAt == nil {
		t.Fatalf("shutdown did not retain canonical terminal state: status=%s reason=%s", terminal.Status, terminal.EndReason)
	}
	projection, err := s.AuthorityCommands.CurrentAuthorityProjection(ctx, authority.ID)
	if err != nil {
		t.Fatal(err)
	}
	if projection.State != core.AuthorityStateRevoked {
		t.Fatal("shutdown left authority active")
	}
	if processMatches(session.RuntimePID, session.ProcessStart) {
		t.Fatal("shutdown retained owned runtime")
	}
	s.sessionTerminalMu.Lock()
	remaining := len(s.sessionTerminalLocks)
	s.sessionTerminalMu.Unlock()
	if remaining != 0 {
		t.Fatal("shutdown retained terminal lock")
	}
}

type supervisorAuthorityRead struct {
	core.AuthorityRepository
	read func(context.Context) ([]core.AuthorityContext, error)
}

func (r supervisorAuthorityRead) ListAuthorityContexts(ctx context.Context) ([]core.AuthorityContext, error) {
	return r.read(ctx)
}

func TestSupervisorCancellationPreservesOtherFailures(t *testing.T) {
	for _, joined := range []bool{false, true} {
		t.Run(map[bool]string{false: "unrelated", true: "joined"}[joined], func(t *testing.T) {
			s, session, _ := supervisorShutdownFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			failure := errors.New("controlled authority read failure")
			expected := error(failure)
			if joined {
				expected = errors.Join(context.Canceled, failure)
			}
			s.Authority = supervisorAuthorityRead{AuthorityRepository: s.Authority, read: func(context.Context) ([]core.AuthorityContext, error) { cancel(); return nil, expected }}
			if err := s.Supervise(ctx); err != expected {
				t.Fatalf("independent failure replaced: %v", err)
			}
			current, err := s.GetSession(session.ID)
			if err != nil {
				t.Fatal(err)
			}
			if current.Status != "running" {
				t.Fatal("failed reconciliation claimed terminal cleanup")
			}
		})
	}
}

func TestSupervisorShutdownFailureIsNotSuccess(t *testing.T) {
	s, session, authority := supervisorShutdownFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	failure := errors.New("controlled shutdown authority read failure")
	reads := 0
	s.Authority = supervisorAuthorityRead{AuthorityRepository: s.Authority, read: func(readCtx context.Context) ([]core.AuthorityContext, error) {
		reads++
		if reads == 1 {
			cancel()
			return nil, ctx.Err()
		}
		if readCtx.Err() != nil {
			t.Fatal("shutdown reused cancelled context")
		}
		return nil, failure
	}}
	if err := s.Supervise(ctx); err != failure {
		t.Fatalf("shutdown failure suppressed: %v", err)
	}
	if reads != 2 {
		t.Fatal("missing fresh shutdown reconciliation")
	}
	current, err := s.GetSession(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := s.AuthorityCommands.CurrentAuthorityProjection(context.Background(), authority.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != "running" || projection.State != core.AuthorityStateActive {
		t.Fatal("failed shutdown fabricated cleanup")
	}
}
