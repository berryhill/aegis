//go:build linux

package hermes

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/berryhill/aegis/internal/execution"
	"golang.org/x/sys/unix"
)

func TestContributionNativeCensusDiagnostics(t *testing.T) {
	for _, stage := range []string{"census_directory", "census_stat", "census_open", "census_recheck", "census_identity", "census_signal", "census_close", "census_context"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			reads, signals, closes := 0, 0, 0
			cause := error(unix.EACCES)
			if stage == "census_context" {
				cause = context.Canceled
				cancel()
			}
			if stage == "census_identity" {
				cause = execution.ErrNativeQuiescenceUnavailable
			}
			empty, err := terminateContributionGroupFrom(ctx, 42,
				func(string) ([]os.DirEntry, error) {
					if stage == "census_directory" {
						return nil, cause
					}
					return contributionProcEntries(t)[:1], nil
				},
				func(string) ([]byte, error) {
					reads++
					if (stage == "census_stat" && reads == 1) || (stage == "census_recheck" && reads == 2) {
						return nil, cause
					}
					group := 42
					if stage == "census_identity" && reads == 2 {
						group = 99
					}
					return contributionProcStat(group, "S"), nil
				},
				func(int, int) (int, error) {
					if stage == "census_open" {
						return -1, cause
					}
					return 7, nil
				},
				func(int, unix.Signal, *unix.Siginfo, int) error {
					signals++
					if stage == "census_signal" {
						return cause
					}
					return nil
				},
				func(int) error {
					closes++
					if stage == "census_close" {
						return cause
					}
					return nil
				})
			var diagnostic *execution.NativeQuiescenceError
			if !errors.As(err, &diagnostic) || diagnostic.Stage() != stage || !errors.Is(err, cause) || !errors.Is(err, execution.ErrNativeQuiescenceUnavailable) || empty {
				t.Fatalf("origin lost: want=%s err=%v empty=%v", stage, err, empty)
			}
			if stage != "census_signal" && stage != "census_close" && signals != 0 {
				t.Fatal("failed observation authorized signal")
			}
			if reads == 2 && closes != 1 {
				t.Fatal("failed recheck leaked pidfd")
			}
		})
	}
}

func TestContributionNativeBarrierDiagnostics(t *testing.T) {
	for _, stage := range []string{"barrier_waitid", "barrier_signal"} {
		t.Run(stage, func(t *testing.T) {
			signals := 0
			err := killPinnedContributionGroup(context.Background(), 42,
				func(int, int, *unix.Siginfo, int, *unix.Rusage) error {
					if stage == "barrier_waitid" {
						return unix.ECHILD
					}
					return nil
				},
				func(int, unix.Signal) error { signals++; return unix.EPERM })
			want := error(unix.ECHILD)
			if stage == "barrier_signal" {
				want = unix.EPERM
			}
			var diagnostic *execution.NativeQuiescenceError
			if !errors.As(err, &diagnostic) || diagnostic.Stage() != stage || !errors.Is(err, want) {
				t.Fatalf("barrier origin lost: %v", err)
			}
			if stage == "barrier_waitid" && signals != 0 {
				t.Fatal("failed ownership authorized barrier")
			}
		})
	}
}
