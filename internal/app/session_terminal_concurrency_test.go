package app

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/berryhill/aegis/internal/core"
)

// Ordinary cleanup and the owning Serve supervisor must not append two terminal
// authority commands or replace the first terminal session outcome.
func TestSessionTerminalConcurrentCleanup(t *testing.T) {
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
	start := make(chan struct{})
	results := make(chan error, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			<-start
			results <- s.endSession(ctx, session.ID, "stopped", "contribution_role_finished", true)
		})
	}
	close(start)
	wg.Wait()
	close(results)
	successes := 0
	for e := range results {
		if e == nil {
			successes++
		} else if !errors.Is(e, ErrConflict) {
			t.Fatalf("cleanup returned non-reconcilable error: %v", e)
		}
	}
	if successes != 1 {
		t.Fatalf("terminal transition winners=%d", successes)
	}
	before, err := s.GetSession(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := s.authorityContextForSession(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := s.AuthorityCommands.CurrentAuthorityProjection(ctx, authority.ID)
	if err != nil {
		t.Fatal(err)
	}
	if before.Status != "stopped" || before.EndReason != "contribution_role_finished" || projection.State == core.AuthorityStateActive || projection.SourceSequence != 2 {
		t.Fatalf("terminal state/history mismatch: status=%s sequence=%d", before.Status, projection.SourceSequence)
	}
	if err = s.endSession(ctx, session.ID, "failed", "runtime_process_missing_or_reused", true); !errors.Is(err, ErrConflict) {
		t.Fatalf("late supervisor transition: %v", err)
	}
	after, err := s.GetSession(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	next, err := s.AuthorityCommands.CurrentAuthorityProjection(ctx, authority.ID)
	if err != nil {
		t.Fatal(err)
	}
	if core.Digest(before) != core.Digest(after) || core.Digest(projection) != core.Digest(next) {
		t.Fatal("late supervisor rewrote terminal history")
	}
}
