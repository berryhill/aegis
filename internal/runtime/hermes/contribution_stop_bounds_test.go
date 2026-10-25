//go:build linux

package hermes

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestContributionStopIgnoringTermBounded(t *testing.T) {
	ready := filepath.Join(t.TempDir(), "ready")
	a, _, b := contributionStopChild(t, "trap '' TERM; : > '"+ready+"'; while :; do :; done")
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child readiness")
		}
		time.Sleep(time.Millisecond)
	}
	start := time.Now()
	done := make(chan error, 1)
	go func() {
		proof, err := a.StopContributionRole(context.Background(), b)
		if err == nil && !proof.RoleProcessAcknowledged() {
			err = errors.New("missing proof")
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("unbounded stop")
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("stop exceeded bound")
	}
	empty, err := contributionGroupEmpty(b.PID)
	if err != nil || !empty {
		t.Fatal("proof before group empty", err)
	}
}

func TestContributionStopActualTimedWait(t *testing.T) {
	ready := filepath.Join(t.TempDir(), "ready")
	a, _, b := contributionStopChild(t, "trap '' TERM; : > '"+ready+"'; while :; do :; done")
	for deadline := time.Now().Add(time.Second); ; {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("readiness")
		}
		time.Sleep(time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	proof, err := a.StopContributionRole(ctx, b)
	if !errors.Is(err, context.DeadlineExceeded) || proof.RoleProcessAcknowledged() {
		t.Fatalf("timed wait: %+v %v", proof, err)
	}
}
