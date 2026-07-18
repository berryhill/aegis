package manager

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestLifecycleTransitionsAndFirstReasonWins(t *testing.T) {
	lifecycle := NewLifecycle()
	for _, state := range []LifecycleState{LifecyclePreflighting, LifecycleStarting, LifecycleActive} {
		if err := lifecycle.Advance(state); err != nil {
			t.Fatal(err)
		}
	}
	if err := lifecycle.RequestClose(EndInterrupt); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.RequestClose(EndRuntimeFailed); err != nil {
		t.Fatal(err)
	}
	state, reason := lifecycle.Snapshot()
	if state != LifecycleClosing || reason != EndInterrupt {
		t.Fatalf("state=%s reason=%s", state, reason)
	}
	if err := lifecycle.Advance(LifecycleActive); err == nil {
		t.Fatal("closing lifecycle returned to active")
	}
	if err := lifecycle.Advance(LifecycleCleaning); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Advance(LifecycleClosed); err != nil {
		t.Fatal(err)
	}
}

func TestLifecycleCanEnterDegradedThenClose(t *testing.T) {
	lifecycle := NewLifecycle()
	for _, state := range []LifecycleState{LifecyclePreflighting, LifecycleStarting, LifecycleDegraded} {
		if err := lifecycle.Advance(state); err != nil {
			t.Fatal(err)
		}
	}
	if err := lifecycle.RequestClose(EndUserExit); err != nil {
		t.Fatal(err)
	}
	if state, _ := lifecycle.Snapshot(); state != LifecycleClosing {
		t.Fatalf("state=%s", state)
	}
}

func TestLifecycleConcurrentShutdownArbitrationIsRaceSafe(t *testing.T) {
	lifecycle := NewLifecycle()
	_ = lifecycle.Advance(LifecycleActive)
	var wait sync.WaitGroup
	for _, reason := range []string{EndUserExit, EndInterrupt, EndRuntimeFailed, EndTermination} {
		wait.Add(1)
		go func(reason string) {
			defer wait.Done()
			_ = lifecycle.RequestClose(reason)
		}(reason)
	}
	wait.Wait()
	state, reason := lifecycle.Snapshot()
	if state != LifecycleClosing || !ValidEndReason(reason) {
		t.Fatalf("state=%s reason=%s", state, reason)
	}
}

func TestEndReasonFromCancellationCause(t *testing.T) {
	for _, test := range []struct {
		cause error
		want  string
	}{{ErrInterrupt, EndInterrupt}, {ErrTermination, EndTermination}, {ErrSessionExpired, EndSessionExpired}, {ErrSessionRevoked, EndSessionRevoked}, {ErrRuntimeFailed, EndRuntimeFailed}} {
		ctx, cancel := context.WithCancelCause(context.Background())
		cancel(test.cause)
		if got := EndReasonFromContext(ctx); got != test.want {
			t.Fatalf("cause=%v got=%s want=%s", test.cause, got, test.want)
		}
	}
	if !errors.Is(ErrInterrupt, ErrInterrupt) {
		t.Fatal("sentinel error identity changed")
	}
}
