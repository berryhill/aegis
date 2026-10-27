package app

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSessionTerminalLockCancellationAndIsolation(t *testing.T) {
	s := new(Service)
	holder, err := s.lockSessionTerminal(context.Background(), "first")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if holder != nil {
			holder()
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		unlock, err := s.lockSessionTerminal(ctx, "first")
		if unlock != nil {
			unlock()
		}
		result <- err
	}()
	// Observe the actual waiter reference before cancelling. A sleep alone
	// cannot establish contention or distinguish pre-cancelled admission.
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
waiting:
	for {
		s.sessionTerminalMu.Lock()
		users := s.sessionTerminalLocks["first"].users
		s.sessionTerminalMu.Unlock()
		if users == 2 {
			break waiting
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("same-session waiter never reached lock admission")
		}
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("contended cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled waiter remained blocked behind cleanup")
	}

	// An unrelated session does not wait for the held first-session cleanup.
	other, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	unlock, err := s.lockSessionTerminal(other, "second")
	if err != nil {
		t.Fatalf("unrelated session blocked: %v", err)
	}
	unlock()
	holder()
	holder = nil
	s.sessionTerminalMu.Lock()
	remaining := len(s.sessionTerminalLocks)
	s.sessionTerminalMu.Unlock()
	if remaining != 0 {
		t.Fatal("completed/cancelled operations retain historical lock entries")
	}
	if unlock, err := s.lockSessionTerminal(ctx, "first"); !errors.Is(err, context.Canceled) || unlock != nil {
		t.Fatal("pre-cancelled admission accepted", err)
	}
}

func TestSessionTerminalLockDeadline(t *testing.T) {
	s := new(Service)
	unlock, err := s.lockSessionTerminal(context.Background(), "held")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if next, err := s.lockSessionTerminal(ctx, "held"); !errors.Is(err, context.DeadlineExceeded) || next != nil {
		t.Fatalf("contended cleanup deadline was not preserved: %v", err)
	}
}
