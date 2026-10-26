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

// ContributionProcessIdentity is captured only while this adapter owns the exact
// runtime process. It is immutable admission evidence, never reconstructed later.
func (a *Adapter) ContributionProcessIdentity(runtime string, pid int, expectedStart string) (string, uint32, error) {
	a.mu.Lock()
	p := a.processes[runtime]
	a.mu.Unlock()
	if p == nil || p.cmd == nil || p.cmd.Process == nil || p.cmd.Process.Pid != pid || p.done == nil {
		return "", 0, ErrContributionStopUnavailable
	}
	select {
	case <-p.done:
		return "", 0, ErrContributionStopUnavailable
	default:
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	start, group, identityErr := contributionProcessIdentity(pid)
	uid, uidErr := contributionUID(pid)
	if err != nil || identityErr != nil || uidErr != nil || start != expectedStart || group != pid || uid != uint32(os.Getuid()) {
		return "", 0, ErrContributionStopUnavailable
	}
	return strings.TrimSpace(string(boot)), uid, nil
}

func contributionUID(pid int) (uint32, error) {
	var st unix.Stat_t
	err := unix.Stat("/proc/"+strconv.Itoa(pid), &st)
	return st.Uid, err
}

// RecoverContributionRole never uses the old adapter map or parent Wait. The
// caller must hold canonical source custody and reconcile all native effects.
// Boot mismatch, absent leader and legacy identity deliberately deny; absence
// cannot establish which process exited or what its effects were.
func (a *Adapter) RecoverContributionRole(ctx context.Context, b ContributionStopBinding, boot string, uid uint32) (ContributionStopProof, error) {
	deny := func() (ContributionStopProof, error) { return ContributionStopProof{}, ErrContributionStopUnavailable }
	if b.SessionID == "" || b.RuntimeSessionID == "" || b.AuthorityID == "" || b.AuthorityDigest == "" || b.Role == "" || b.Epoch == 0 || b.PID <= 0 || b.ProcessStart == "" || boot == "" || uid != uint32(os.Getuid()) {
		return deny()
	}
	a.mu.Lock()
	owned := a.processes[b.RuntimeSessionID] != nil
	a.mu.Unlock()
	if owned {
		return deny()
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return ContributionStopProof{}, err
	}
	current, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil || strings.TrimSpace(string(current)) != boot {
		return deny()
	}
	fd, err := unix.PidfdOpen(b.PID, 0)
	if err != nil {
		return deny()
	}
	defer unix.Close(fd)
	start, group, err := contributionProcessIdentity(b.PID)
	actualUID, uidErr := contributionUID(b.PID)
	if err != nil || uidErr != nil || start != b.ProcessStart || group != b.PID || actualUID != uid || unix.PidfdSendSignal(fd, 0, nil, 0) != nil {
		return deny()
	}
	if err := ctx.Err(); err != nil {
		return ContributionStopProof{}, err
	}
	if unix.PidfdSendSignal(fd, unix.SIGTERM, nil, 0) != nil {
		return deny()
	}
	escalation := time.Now().Add(time.Second)
	killed := false
	for {
		if err := ctx.Err(); err != nil {
			return ContributionStopProof{}, err
		}
		poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		n, err := unix.Poll(poll, 10)
		if err != nil && err != unix.EINTR {
			return deny()
		}
		if n > 0 && poll[0].Revents&unix.POLLIN != 0 {
			empty, err := contributionGroupQuiescent(group)
			if err != nil {
				return deny()
			}
			if empty {
				return ContributionStopProof{binding: b, boot: boot, kind: "recovered-pidfd-exit.v1", acknowledged: true}, nil
			}
		}
		if !killed && time.Now().After(escalation) {
			err := unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0)
			if err != nil && err != unix.ESRCH {
				return deny()
			}
			killed = true
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func contributionGroupQuiescent(group int) (bool, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
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
		if pg != group {
			continue
		}
		raw, err := os.ReadFile("/proc/" + entry.Name() + "/stat")
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return false, err
		}
		end := strings.LastIndexByte(string(raw), ')')
		fields := strings.Fields(string(raw[end+1:]))
		// A zombie cannot execute; recovered proof does not claim parent reaping.
		if len(fields) == 0 || fields[0] != "Z" {
			return false, nil
		}
	}
	return true, nil
}
