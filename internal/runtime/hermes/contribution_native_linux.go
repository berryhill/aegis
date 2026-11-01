//go:build linux

package hermes

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"github.com/berryhill/aegis/internal/execution"
	"golang.org/x/sys/unix"
)

// NativeCompletion is minted only by the exact owned Cmd.Wait and group-quiescent
// join. It proves no owned writer, NOT success, authority or custody release.
type NativeCompletion struct {
	binding execution.NativeBinding
	joined  bool
}

func (p NativeCompletion) Binding() execution.NativeBinding { return p.binding }
func (p NativeCompletion) Joined() bool                     { return p.joined }
func NativeCommandDigest(cmd *exec.Cmd) string              { return execution.NativeCommandDigest(cmd) }

// RunContributionNative accepts only an already constructed/qualified internal
// command. Reserve must durably fence it before Start. Finish is persistence-only.
// No shell, command parser, approval, successor or HTTP route is introduced here.
func RunContributionNative(ctx context.Context, b execution.NativeBinding, cmd *exec.Cmd, reserve func(context.Context) error, finish func(context.Context, NativeCompletion) error) error {
	return runContributionNative(ctx, b, cmd, reserve, finish, terminateContributionGroup)
}

func runContributionNative(ctx context.Context, b execution.NativeBinding, cmd *exec.Cmd, reserve func(context.Context) error, finish func(context.Context, NativeCompletion) error, observe func(context.Context, int) (bool, error)) error {
	return runContributionNativeWithBarrier(ctx, b, cmd, reserve, finish, observe, func(ctx context.Context, group int) error {
		return killPinnedContributionGroup(ctx, group, unix.Waitid, unix.Kill)
	})
}

// runContributionNativeWithBarrier is an unexported fixture seam. Production
// always supplies the real WNOWAIT-owned kernel barrier above, never a selector.
func runContributionNativeWithBarrier(ctx context.Context, b execution.NativeBinding, cmd *exec.Cmd, reserve func(context.Context) error, finish func(context.Context, NativeCompletion) error, observe func(context.Context, int) (bool, error), barrier func(context.Context, int) error) error {
	if cmd == nil || cmd.Process != nil || cmd.SysProcAttr != nil || b.CommandDigest != NativeCommandDigest(cmd) || reserve == nil || finish == nil || observe == nil || barrier == nil {
		return execution.ErrInvalidNativeCommand
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := reserve(ctx); err != nil {
		return err
	}
	complete := func(p NativeCompletion, cause error) error {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		if err := finish(cleanup, p); err != nil {
			return err
		}
		return cause
	}
	if err := ctx.Err(); err != nil {
		return complete(NativeCompletion{binding: b}, err)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	if err := cmd.Start(); err != nil {
		return complete(NativeCompletion{binding: b}, err)
	}
	group := cmd.Process.Pid
	// Leave the exact child waitable until descendant cleanup is complete. Its
	// unreaped PID prevents group-number reuse even when the leader exits first.
	// Cmd.Wait is the sole reaper and is still required before acknowledging.
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	var cause error
	var first *nativeFailureStage
	record := func(stage string, err error) {
		if first == nil {
			first = nativeStage(stage, err)
		}
	}
waitLeader:
	for {
		var info unix.Siginfo
		err := unix.Waitid(unix.P_PID, group, &info, unix.WEXITED|unix.WNOHANG|unix.WNOWAIT, nil)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			record("wait_leader", err)
			cause = execution.ErrNativeQuiescenceUnavailable
			break
		}
		if info.Signo != 0 {
			break
		}
		select {
		case <-ctx.Done():
			cause = ctx.Err()
			break waitLeader
		case <-tick.C:
		}
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	cleanupStarted := time.Now()
	defer cancel()
	quiescent := false
	// A waitid failure cannot establish that we still own the group number. Do
	// not scan/signal that group; kill only the pinned os.Process and deny proof.
	if cause != execution.ErrNativeQuiescenceUnavailable {
		// Exact unreaped-child ownership, not a proc inventory, authorizes this
		// barrier. Stop owned fork publication before the complete fresh scan.
		// A later observation error still denies proof and retains custody even
		// though the admitted owned group may already have been killed.
		if err := barrier(cleanup, group); err != nil {
			record("barrier_waitid", err)
			cause = execution.ErrNativeQuiescenceUnavailable
			goto joined
		}
		for {
			empty, err := observe(cleanup, group)
			if err != nil {
				record("observe_stat", err)
				cause = execution.ErrNativeQuiescenceUnavailable
				break
			}
			if empty {
				quiescent = true
				break
			}
			select {
			case <-cleanup.Done():
				record("observe_nonempty_deadline", cleanup.Err())
				cause = execution.ErrNativeQuiescenceUnavailable
				goto joined
			case <-tick.C:
			}
		}
	}
joined:
	if !quiescent {
		// Observation failure denies group ownership, not our exact child handle.
		// Do not leave a still-running leader behind a bounded unknown result.
		_ = cmd.Process.Kill()
	}
	joinExpired := false
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if cause == nil {
			cause = err
		}
	case <-cleanup.Done():
		joinExpired = true
		record("cmd_wait_deadline", cleanup.Err())
		quiescent = false
		cause = execution.ErrNativeQuiescenceUnavailable
	}
	if err := ctx.Err(); err != nil && cause == nil {
		cause = err
	}
	// Decorate only after exact-sentinel control flow; finish errors still win.
	if cause == execution.ErrNativeQuiescenceUnavailable && first != nil {
		cause = &nativeFailureDiagnostic{first: *first, joinExpired: joinExpired, elapsed: time.Since(cleanupStarted)}
	}
	return complete(NativeCompletion{binding: b, joined: quiescent}, cause)
}

// killPinnedContributionGroup is valid only before the sole Cmd.Wait reaper
// starts. Successful WNOWAIT revalidation keeps the exact child's PID allocated,
// so this private Setpgid group's number cannot name a recycled foreign group.
// Linux group SIGKILL and fork publication serialize through tasklist_lock:
// an already published child is visited, while a concurrent unpublished fork
// fails the fatal_signal_pending check in copy_process before publication.
// SIGKILL cannot be caught/blocked. Thus no surviving member can fork beyond
// this barrier. This is not a sandbox against deliberate setpgid/setsid escape.
// ESRCH means no signalable member, NOT quiescence; fresh scans remain required.
func killPinnedContributionGroup(ctx context.Context, group int, waitid func(int, int, *unix.Siginfo, int, *unix.Rusage) error, kill func(int, unix.Signal) error) error {
	if group <= 1 {
		return nativeStage("barrier_waitid", execution.ErrNativeQuiescenceUnavailable)
	}
	for {
		if err := ctx.Err(); err != nil {
			return nativeStage("barrier_waitid", err)
		}
		var info unix.Siginfo
		err := waitid(unix.P_PID, group, &info, unix.WEXITED|unix.WNOHANG|unix.WNOWAIT, nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return nativeStage("barrier_waitid", err)
		}
		break
	}
	if err := ctx.Err(); err != nil {
		return nativeStage("barrier_signal", err)
	}
	if err := kill(-group, unix.SIGKILL); err != nil && !errors.Is(err, unix.ESRCH) {
		return nativeStage("barrier_signal", err)
	}
	return nil
}

// terminateContributionGroup may run only while the owned leader is unreaped.
// Each per-process signal is pidfd-pinned and identity-rechecked. A scan alone
// cannot prove quiescence: the caller must first establish the kernel fork
// barrier above, and only accept an empty observation taken AFTER that barrier.
// Zombies have no writers; state and membership come from the SAME stat read.
func terminateContributionGroup(ctx context.Context, group int) (bool, error) {
	return terminateContributionGroupFrom(ctx, group, os.ReadDir, os.ReadFile, unix.PidfdOpen, unix.PidfdSendSignal, unix.Close)
}

func terminateContributionGroupFrom(ctx context.Context, group int, readDir func(string) ([]os.DirEntry, error), readFile func(string) ([]byte, error), open func(int, int) (int, error), signal func(int, unix.Signal, *unix.Siginfo, int) error, closeFD func(int) error) (bool, error) {
	entries, err := readDir("/proc")
	if err != nil {
		return false, nativeStage("observe_readdir", err)
	}
	empty := true
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return false, nativeStage("observe_context", err)
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		start, pg, state, err := contributionProcessStatFrom(pid, readFile)
		if contributionProcDisappeared(err) {
			continue
		}
		if err != nil {
			return false, nativeStage("observe_stat", err)
		}
		if pg != group || state == "Z" || state == "X" || state == "x" {
			continue
		}
		empty = false
		fd, err := open(pid, 0)
		if errors.Is(err, unix.ESRCH) {
			continue
		}
		if err != nil {
			return false, nativeStage("observe_pidfd_open", err)
		}
		current, currentGroup, _, readErr := contributionProcessStatFrom(pid, readFile)
		if contributionProcDisappeared(readErr) {
			if err := closeFD(fd); err != nil {
				return false, nativeStage("observe_close", err)
			}
			continue
		}
		if readErr != nil || current != start || currentGroup != group {
			closeFD(fd)
			if readErr != nil {
				return false, nativeStage("observe_recheck_read", errors.Join(execution.ErrNativeQuiescenceUnavailable, readErr))
			}
			return false, nativeStage("observe_recheck_identity", execution.ErrNativeQuiescenceUnavailable)
		}
		if err := ctx.Err(); err != nil {
			closeFD(fd)
			return false, nativeStage("observe_context", err)
		}
		err = signal(fd, unix.SIGKILL, nil, 0)
		closeErr := closeFD(fd)
		if closeErr != nil {
			return false, nativeStage("observe_close", closeErr)
		}
		if err != nil && !errors.Is(err, unix.ESRCH) {
			return false, nativeStage("observe_signal", err)
		}
	}
	return empty, nil
}

// TEMPORARY hosted-failure diagnostics. This private vocabulary is not authority,
// a logging surface, or a replacement for the original cleanup decisions.
type nativeFailureStage struct {
	stage string
	err   error
}

func (e *nativeFailureStage) Error() string {
	return closedNativeStage(e.stage) + ":" + nativeFailureClass(e.err)
}
func (e *nativeFailureStage) Unwrap() error { return e.err }
func nativeStage(stage string, err error) *nativeFailureStage {
	var tagged *nativeFailureStage
	if errors.As(err, &tagged) {
		return tagged
	}
	return &nativeFailureStage{stage: stage, err: err}
}
func closedNativeStage(stage string) string {
	switch stage {
	case "wait_leader", "barrier_waitid", "barrier_signal", "observe_readdir", "observe_context", "observe_stat", "observe_pidfd_open", "observe_recheck_read", "observe_recheck_identity", "observe_signal", "observe_close", "observe_nonempty_deadline", "cmd_wait_deadline":
		return stage
	default:
		return "observe_stat"
	}
}
func nativeFailureClass(err error) string {
	for _, c := range []struct {
		err  error
		name string
	}{
		{context.DeadlineExceeded, "deadline"}, {context.Canceled, "cancelled"},
		{unix.EACCES, "access"}, {unix.EPERM, "permission"}, {unix.ECHILD, "child_lost"},
		{unix.ESRCH, "process_gone"}, {unix.ENOENT, "not_found"}, {unix.EINVAL, "invalid"},
		{execution.ErrNativeQuiescenceUnavailable, "identity_or_stat_invalid"},
	} {
		if errors.Is(err, c.err) {
			return c.name
		}
	}
	return "other"
}

type nativeFailureDiagnostic struct {
	first       nativeFailureStage
	joinExpired bool
	elapsed     time.Duration
}

func (e *nativeFailureDiagnostic) Unwrap() error { return execution.ErrNativeQuiescenceUnavailable }
func (e *nativeFailureDiagnostic) Error() string {
	ms := e.elapsed.Milliseconds()
	if ms < 0 {
		ms = 0
	}
	if ms > 1000 {
		ms = 1000
	}
	return fmt.Sprintf("native quiescence unavailable: first=%s class=%s cmd_wait_deadline=%t cleanup_ms=%d", closedNativeStage(e.first.stage), nativeFailureClass(e.first.err), e.joinExpired, ms)
}
