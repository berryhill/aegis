package implementation

import (
	"context"
	"errors"
	"time"
)

// ErrWorkspaceBusy identifies ONLY ordinary nonblocking lock contention.
// Generic custody still returns immediately and remains exclusive.
var ErrWorkspaceBusy = errors.New("workspace custody busy")

// WaitWorkspaceCustody waits for a writer to leave, then releases the lock
// immediately. It grants no source/effect capability and never invokes work.
// A deadline is mandatory so this cannot become a watcher/daemon.
func WaitWorkspaceCustody(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("bounded custody wait requires deadline")
	}
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		unlock, err := lockWorkspace(path)
		if err == nil {
			unlock()
			return ctx.Err()
		}
		if !errors.Is(err, ErrWorkspaceBusy) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}
