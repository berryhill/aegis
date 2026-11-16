//go:build linux

package hermes

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"time"

	"golang.org/x/sys/unix"
)

func prepareAttemptProcess() error { return nil }

// Observe exit without reaping: the exact waitable child reserves its private
// Setpgid group number until cleanup. Only stopOwnedAttemptProcess calls Cmd.Wait.
func watchAttemptProcess(command *exec.Cmd, failures chan<- error) func() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		err := waitAttemptLeader(ctx, command.Process.Pid)
		if ctx.Err() == nil {
			if err == nil {
				err = io.EOF
			}
			select {
			case failures <- err:
			default:
			}
		}
	}()
	return func() { cancel(); <-done }
}

func waitAttemptLeader(ctx context.Context, pid int) error {
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var info unix.Siginfo
		err := unix.Waitid(unix.P_PID, pid, &info, unix.WEXITED|unix.WNOHANG|unix.WNOWAIT, nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil || info.Signo != 0 {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

func stopOwnedAttemptProcess(command *exec.Cmd, stdin io.Closer) error {
	return stopOwnedAttemptProcessWith(command, stdin, func(ctx context.Context, group int, signal unix.Signal) error {
		// Reuse the native WNOWAIT ownership gate for TERM as well as the
		// uncatchable KILL/fork barrier. No group signals follow Cmd.Wait.
		return killPinnedContributionGroup(ctx, group, unix.Waitid, func(pid int, _ unix.Signal) error {
			return unix.Kill(pid, signal)
		})
	}, func(ctx context.Context, group int) (bool, error) {
		return terminateContributionGroupFrom(ctx, group, os.ReadDir, os.ReadFile, unix.PidfdOpen, unix.PidfdSendSignal, unix.Close, func(pid int) error {
			return reapAttemptDescendant(pid, group)
		})
	})
}

// The seam is test-only; production uses the existing pinned native barrier and
// complete fresh census. A deadline/observation/Wait failure is not quiescence.
func stopOwnedAttemptProcessWith(command *exec.Cmd, stdin io.Closer, signal func(context.Context, int, unix.Signal) error, observe func(context.Context, int) (bool, error)) error {
	_ = stdin.Close()
	cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	group := command.Process.Pid
	cause := signal(cleanup, group, unix.SIGTERM)
	if cause == nil {
		grace, endGrace := context.WithTimeout(cleanup, 2*time.Second)
		err := waitAttemptLeader(grace, group)
		endGrace()
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			cause = err
		}
	}
	// Even a cooperative or already-exited leader can leave a resistant child.
	if cause == nil {
		cause = signal(cleanup, group, unix.SIGKILL)
	}
	if cause == nil {
		cause = awaitAttemptQuiescence(cleanup, group, observe)
	}
	if cause != nil {
		// Failure of group ownership permits only the exact os.Process handle.
		_ = command.Process.Kill()
	}
	// Sole reaper, only AFTER the final group signal/observation. If a task is
	// uninterruptible, return failure on deadline and keep its home, not success.
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case waitErr := <-done:
		// A signal/exit status is expected; a lost wait owner is not reaping.
		var exitErr *exec.ExitError
		if waitErr != nil && !errors.As(waitErr, &exitErr) && !errors.Is(waitErr, context.Canceled) && !errors.Is(waitErr, context.DeadlineExceeded) {
			cause = errors.Join(cause, waitErr)
		}
	case <-cleanup.Done():
		cause = errors.Join(cause, cleanup.Err())
	}
	if cause != nil {
		return ErrAttemptCleanup
	}
	return nil
}

func awaitAttemptQuiescence(ctx context.Context, group int, observe func(context.Context, int) (bool, error)) error {
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		empty, err := observe(ctx, group)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if empty {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

// Under a subreaper, an ordinary orphan may now be our child. Reap only a
// pidfd-pinned, freshly rechecked terminal member, never the Cmd.Wait leader.
// ECHILD means another parent owns reaping; a zombie has no runtime effects.
func reapAttemptDescendant(pid, group int) error {
	if pid == group {
		return nil
	}
	fd, err := unix.PidfdOpen(pid, 0)
	if errors.Is(err, unix.ESRCH) {
		return nil
	}
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	_, pg, state, err := nativeCensusStatFrom(pid, os.ReadFile)
	if contributionProcDisappeared(err) {
		return nil
	}
	if err != nil || pg != group || (state != "Z" && state != "X" && state != "x") {
		return ErrAttemptCleanup
	}
	var info unix.Siginfo
	for {
		err = unix.Waitid(unix.P_PIDFD, fd, &info, unix.WEXITED|unix.WNOHANG, nil)
		if !errors.Is(err, unix.EINTR) {
			break
		}
	}
	if errors.Is(err, unix.ECHILD) {
		return nil
	}
	return err
}
