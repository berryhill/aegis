//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package implementation

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestContributionCustodyWaitDoesNotChangeGenericLock(t *testing.T) {
	path := t.TempDir()
	unlock, err := lockWorkspace(path)
	if err != nil {
		t.Fatal(err)
	}
	if release, err := lockWorkspace(path); !errors.Is(err, ErrWorkspaceBusy) {
		if release != nil {
			release()
		}
		unlock()
		t.Fatal("generic lock did not remain nonblocking/exclusive", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	released := make(chan struct{})
	time.AfterFunc(20*time.Millisecond, func() { unlock(); close(released) })
	if err := WaitWorkspaceCustody(ctx, path); err != nil {
		t.Fatal(err)
	}
	<-released
	release, err := lockWorkspace(path)
	if err != nil {
		t.Fatal("waiter retained custody", err)
	}
	release()
}
func TestContributionCustodyWaitCancellationFailureAndDeadline(t *testing.T) {
	path := t.TempDir()
	release, err := lockWorkspace(path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := WaitWorkspaceCustody(cancelled, path); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	deadline, done := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer done()
	if err := WaitWorkspaceCustody(deadline, path); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if got, err := lockWorkspace(path); !errors.Is(err, ErrWorkspaceBusy) {
		if got != nil {
			got()
		}
		t.Fatal("wait stole owner custody", err)
	}
	if err := WaitWorkspaceCustody(context.Background(), path); err == nil {
		t.Fatal("unbounded wait accepted")
	}
	bounded, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := WaitWorkspaceCustody(bounded, path+"/absent"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unrelated resource failure disguised as contention", err)
	}
}
