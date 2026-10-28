package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
)

func terminalAdmissionFixture(t *testing.T) (*Service, core.Session) {
	t.Helper()
	s := testService(t)
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
	mandate, _, err := s.PreviewSession(context.Background(), "office", 1, "principal", core.Environment{Name: "local"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.StartSession(context.Background(), mandate.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Hermes.Terminate(context.Background(), session.RuntimeSessionID, false) })
	return s, session
}

func TestSessionTerminalCancelledWaitDoesNotMutate(t *testing.T) {
	s, original := terminalAdmissionFixture(t)
	authority, err := s.authorityContextForSession(context.Background(), original.ID)
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.AuthorityCommands.CurrentAuthorityProjection(context.Background(), authority.ID)
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := s.lockSessionTerminal(context.Background(), original.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- s.endSession(ctx, original.ID, "stopped", "cancelled_cleanup", true) }()
	waitTerminalContenders(t, s, original.ID, 2)
	cancel()
	select {
	case err = <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled waiter lost cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled session cleanup remained blocked")
	}
	after, err := s.GetSession(original.ID)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := s.AuthorityCommands.CurrentAuthorityProjection(context.Background(), authority.ID)
	if err != nil {
		t.Fatal(err)
	}
	if core.Digest(after) != core.Digest(original) || core.Digest(projection) != core.Digest(before) {
		t.Fatal("cancelled lock admission mutated session or authority")
	}
}

func TestSessionTerminalSupervisorFirstPreservesOutcome(t *testing.T) {
	s, original := terminalAdmissionFixture(t)
	ctx := context.Background()
	if err := s.endSession(ctx, original.ID, "failed", "runtime_process_missing_or_reused", true); err != nil {
		t.Fatal(err)
	}
	authority, err := s.authorityContextForSession(ctx, original.ID)
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.GetSession(original.ID)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := s.AuthorityCommands.CurrentAuthorityProjection(ctx, authority.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.endSession(ctx, original.ID, "stopped", "contribution_role_finished", true); !errors.Is(err, ErrConflict) {
		t.Fatalf("late distinct cleanup must retain first outcome: %v", err)
	}
	after, err := s.GetSession(original.ID)
	if err != nil {
		t.Fatal(err)
	}
	next, err := s.AuthorityCommands.CurrentAuthorityProjection(ctx, authority.ID)
	if err != nil {
		t.Fatal(err)
	}
	if before.Status != "failed" || before.EndReason != "runtime_process_missing_or_reused" || projection.SourceSequence != 2 || core.Digest(before) != core.Digest(after) || core.Digest(projection) != core.Digest(next) {
		t.Fatal("role cleanup rewrote supervisor's terminal history")
	}
}
