//go:build linux

package hermes

import (
	"context"
	"errors"
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
	if cmd == nil || cmd.Process != nil || cmd.SysProcAttr != nil || b.CommandDigest != NativeCommandDigest(cmd) || reserve == nil || finish == nil {
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
waitLeader:
	for {
		var info unix.Siginfo
		err := unix.Waitid(unix.P_PID, group, &info, unix.WEXITED|unix.WNOHANG|unix.WNOWAIT, nil)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
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
	defer cancel()
	quiescent := false
	// A waitid failure cannot establish that we still own the group number. Do
	// not scan/signal that group; kill only the pinned os.Process and deny proof.
	if cause != execution.ErrNativeQuiescenceUnavailable {
		// An inventory is not a fork barrier, even when it appears empty. Keep
		// its fail-closed observation checks, then kill the whole pinned group
		// and require a NEW quiescence observation after that kernel barrier.
		_, err := observe(cleanup, group)
		if err == nil {
			err = killPinnedContributionGroup(cleanup, group, unix.Waitid, unix.Kill)
		}
		if err != nil {
			cause = execution.ErrNativeQuiescenceUnavailable
			goto joined
		}
		for {
			empty, err := observe(cleanup, group)
			if err != nil {
				cause = execution.ErrNativeQuiescenceUnavailable
				break
			}
			if empty {
				quiescent = true
				break
			}
			select {
			case <-cleanup.Done():
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
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if cause == nil {
			cause = err
		}
	case <-cleanup.Done():
		quiescent = false
		cause = execution.ErrNativeQuiescenceUnavailable
	}
	if err := ctx.Err(); err != nil && cause == nil {
		cause = err
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
		return execution.ErrNativeQuiescenceUnavailable
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var info unix.Siginfo
		err := waitid(unix.P_PID, group, &info, unix.WEXITED|unix.WNOHANG|unix.WNOWAIT, nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		break
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := kill(-group, unix.SIGKILL); err != nil && !errors.Is(err, unix.ESRCH) {
		return err
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
		return false, err
	}
	empty := true
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return false, err
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
			return false, err
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
			return false, err
		}
		current, currentGroup, _, readErr := contributionProcessStatFrom(pid, readFile)
		if contributionProcDisappeared(readErr) {
			if err := closeFD(fd); err != nil {
				return false, err
			}
			continue
		}
		if readErr != nil || current != start || currentGroup != group {
			closeFD(fd)
			return false, execution.ErrNativeQuiescenceUnavailable
		}
		if err := ctx.Err(); err != nil {
			closeFD(fd)
			return false, err
		}
		err = signal(fd, unix.SIGKILL, nil, 0)
		closeErr := closeFD(fd)
		if closeErr != nil {
			return false, closeErr
		}
		if err != nil && !errors.Is(err, unix.ESRCH) {
			return false, err
		}
	}
	return empty, nil
}
