//go:build linux

package hermes

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

func TestContributionNativeForkAfterEnumeration(t *testing.T) {
	runLateForkBarrierFixture(t)
}

func TestContributionNativePinnedGroupSignalBoundaries(t *testing.T) {
	for _, mode := range []string{"owned", "absent", "wait_denied", "wait_lost", "signal_denied", "cancelled", "interrupted"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			waits, signals := 0, 0
			err := killPinnedContributionGroup(ctx, 42, func(kind, pid int, info *unix.Siginfo, flags int, r *unix.Rusage) error {
				waits++
				if kind != unix.P_PID || pid != 42 || flags != unix.WEXITED|unix.WNOHANG|unix.WNOWAIT {
					t.Fatal("not exact WNOWAIT pin")
				}
				switch mode {
				case "wait_denied":
					return unix.EPERM
				case "wait_lost":
					return unix.ECHILD
				case "interrupted":
					if waits == 1 {
						return unix.EINTR
					}
				}
				return nil
			}, func(pid int, sig unix.Signal) error {
				signals++
				if pid != -42 || sig != unix.SIGKILL {
					t.Fatal("wrong group/signal")
				}
				if mode == "absent" {
					return unix.ESRCH
				}
				if mode == "signal_denied" {
					return unix.EPERM
				}
				return nil
			})
			denied := mode == "wait_denied" || mode == "wait_lost" || mode == "cancelled"
			if (signals == 0) != denied || (err != nil) != (denied || mode == "signal_denied") {
				t.Fatalf("err=%v signals=%d", err, signals)
			}
			if mode == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
}
