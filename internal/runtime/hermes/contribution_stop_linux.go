//go:build linux

package hermes

import (
	"context"
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"strconv"
	"strings"
	"time"
)

func contributionProcessIdentity(pid int) (start string, group int, err error) {
	return contributionProcessIdentityFrom(pid, os.ReadFile)
}

func contributionProcessIdentityFrom(pid int, readFile func(string) ([]byte, error)) (start string, group int, err error) {
	start, group, _, err = contributionProcessStatFrom(pid, readFile)
	return start, group, err
}

// Read identity, membership and state from one snapshot. In particular, recovery
// must not combine a group's membership with a reused PID's later zombie state.
func contributionProcessStatFrom(pid int, readFile func(string) ([]byte, error)) (start string, group int, state string, err error) {
	raw, err := readFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return "", 0, "", err
	}
	end := strings.LastIndexByte(string(raw), ')')
	if end < 0 {
		return "", 0, "", ErrContributionStopUnavailable
	}
	fields := strings.Fields(string(raw[end+1:]))
	if len(fields) <= 19 {
		return "", 0, "", ErrContributionStopUnavailable
	}
	group, err = strconv.Atoi(fields[2])
	if err != nil || group < 0 || len(fields[0]) != 1 || !strings.Contains("RSDZTtXxKWPI", fields[0]) {
		return "", 0, "", ErrContributionStopUnavailable
	}
	if _, err := strconv.ParseUint(fields[19], 10, 64); err != nil {
		return "", 0, "", ErrContributionStopUnavailable
	}
	return fields[19], group, fields[0], nil
}

// Only an already enumerated /proc PID may disappear benignly. Never use this
// to admit missing owned identity, failed /proc enumeration or other I/O errors.
func contributionProcDisappeared(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, unix.ESRCH)
}

func contributionGroupEmpty(group int) (bool, error) {
	return contributionGroupEmptyFrom(group, os.ReadDir, os.ReadFile)
}

func contributionGroupEmptyFrom(group int, readDir func(string) ([]os.DirEntry, error), readFile func(string) ([]byte, error)) (bool, error) {
	entries, err := readDir("/proc")
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		_, pg, err := contributionProcessIdentityFrom(pid, readFile)
		if contributionProcDisappeared(err) {
			continue
		}
		if err != nil {
			return false, err
		}
		if pg == group {
			return false, nil
		}
	}
	return true, nil
}

func (a *Adapter) stopContributionRole(ctx context.Context, b ContributionStopBinding) (ContributionStopProof, error) {
	// Product-owned upper bound even for Background callers. Leave a full
	// second after escalation for the owned Wait and group-empty observation.
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return ContributionStopProof{}, err
	}
	a.mu.Lock()
	p := a.processes[b.RuntimeSessionID]
	a.mu.Unlock()
	if p == nil || p.cmd == nil || p.cmd.Process == nil || p.cmd.Process.Pid != b.PID || p.done == nil {
		return ContributionStopProof{}, ErrContributionStopUnavailable
	}
	// Pin the kernel process BEFORE checking identity or sending any signal. Never
	// use kill(-pid): a pidfd pins only the leader, not a reusable group number.
	fd, err := unix.PidfdOpen(b.PID, 0)
	if err != nil {
		return ContributionStopProof{}, ErrContributionStopUnavailable
	}
	defer unix.Close(fd)
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return ContributionStopProof{}, ErrContributionStopUnavailable
	}
	start, group, err := contributionProcessIdentity(b.PID)
	if err != nil || start != b.ProcessStart || group != b.PID || unix.PidfdSendSignal(fd, 0, nil, 0) != nil {
		return ContributionStopProof{}, ErrContributionStopUnavailable
	}
	select {
	case <-p.done:
		return ContributionStopProof{}, ErrContributionStopUnavailable
	default:
	}
	if err := ctx.Err(); err != nil {
		return ContributionStopProof{}, err
	}
	if err = unix.PidfdSendSignal(fd, unix.SIGTERM, nil, 0); err != nil {
		return ContributionStopProof{}, ErrContributionStopUnavailable
	}
	// Wait error after our signal is expected; the exact owned Wait channel, not
	// exit success or a dead PID, establishes reaping. Missing/crashed owner denies.
	escalation := time.NewTimer(time.Second)
	defer escalation.Stop()
	select {
	case <-ctx.Done():
		return ContributionStopProof{}, ctx.Err()
	case <-p.done:
	case <-escalation.C:
		// Signal only the pinned kernel process, never a reusable process group.
		if err := unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0); err != nil && err != unix.ESRCH {
			return ContributionStopProof{}, ErrContributionStopUnavailable
		}
		select {
		case <-ctx.Done():
			return ContributionStopProof{}, ctx.Err()
		case <-p.done:
		}
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		empty, err := contributionGroupEmpty(group)
		if err != nil {
			return ContributionStopProof{}, ErrContributionStopUnavailable
		}
		if empty {
			return ContributionStopProof{binding: b, boot: strings.TrimSpace(string(boot)), acknowledged: true}, nil
		}
		select {
		case <-ctx.Done():
			return ContributionStopProof{}, ctx.Err()
		case <-ticker.C:
		}
	}
}
