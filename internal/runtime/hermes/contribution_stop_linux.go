//go:build linux

package hermes

import (
	"context"
	"golang.org/x/sys/unix"
	"os"
	"strconv"
	"strings"
	"time"
)

func contributionProcessIdentity(pid int) (start string, group int, err error) {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return "", 0, err
	}
	end := strings.LastIndexByte(string(raw), ')')
	if end < 0 {
		return "", 0, ErrContributionStopUnavailable
	}
	fields := strings.Fields(string(raw[end+1:]))
	if len(fields) <= 19 {
		return "", 0, ErrContributionStopUnavailable
	}
	group, err = strconv.Atoi(fields[2])
	return fields[19], group, err
}

func contributionGroupEmpty(group int) (bool, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		_, pg, err := contributionProcessIdentity(pid)
		if os.IsNotExist(err) {
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
