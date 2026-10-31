//go:build linux

package hermes

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestContributionNativeDescendantObservationBoundaries(t *testing.T) {
	for _, mode := range []string{"owned", "foreign", "zombie", "disappeared", "enumeration_denied", "read_denied", "malformed", "open_denied", "open_disappeared", "reused_start", "changed_group", "recheck_disappeared", "recheck_denied", "signal_denied", "close_denied", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			reads, opens, signals, closes := 0, 0, 0, 0
			empty, err := terminateContributionGroupFrom(ctx, 42,
				func(string) ([]os.DirEntry, error) {
					if mode == "enumeration_denied" {
						return nil, unix.EACCES
					}
					return contributionProcEntries(t)[:1], nil
				},
				func(string) ([]byte, error) {
					reads++
					switch mode {
					case "read_denied":
						return nil, unix.EACCES
					case "disappeared":
						return nil, unix.ESRCH
					case "malformed":
						return []byte("bad stat"), nil
					case "foreign":
						return contributionProcStat(99, "S"), nil
					case "zombie":
						return contributionProcStat(42, "Z"), nil
					}
					if reads == 2 {
						switch mode {
						case "reused_start":
							return []byte(strings.Replace(string(contributionProcStat(42, "S")), "12345", "12346", 1)), nil
						case "changed_group":
							return contributionProcStat(99, "Z"), nil
						case "recheck_disappeared":
							return nil, unix.ENOENT
						case "recheck_denied":
							return nil, unix.EPERM
						case "cancelled":
							cancel()
						}
					}
					return contributionProcStat(42, "S"), nil
				},
				func(pid, flags int) (int, error) {
					opens++
					if pid != 100 || flags != 0 {
						t.Fatal(pid, flags)
					}
					if mode == "open_denied" {
						return -1, unix.EPERM
					}
					if mode == "open_disappeared" {
						return -1, unix.ESRCH
					}
					return 7, nil
				},
				func(fd int, sig unix.Signal, _ *unix.Siginfo, flags int) error {
					signals++
					if fd != 7 || sig != unix.SIGKILL || flags != 0 {
						t.Fatal(fd, sig, flags)
					}
					if mode == "signal_denied" {
						return unix.EPERM
					}
					return nil
				},
				func(fd int) error {
					closes++
					if fd != 7 {
						t.Fatal(fd)
					}
					if mode == "close_denied" {
						return unix.EIO
					}
					return nil
				},
			)
			wantEmpty := mode == "foreign" || mode == "zombie" || mode == "disappeared"
			wantSignal := mode == "owned" || mode == "signal_denied" || mode == "close_denied"
			wantErr := mode == "enumeration_denied" || mode == "read_denied" || mode == "malformed" || mode == "open_denied" || mode == "reused_start" || mode == "changed_group" || mode == "recheck_denied" || mode == "signal_denied" || mode == "close_denied" || mode == "cancelled"
			if empty != wantEmpty || (err != nil) != wantErr || (signals == 1) != wantSignal {
				t.Fatalf("empty=%v err=%v signals=%d", empty, err, signals)
			}
			if opens == 1 && mode != "open_denied" && mode != "open_disappeared" && closes != 1 {
				t.Fatal("leaked pidfd")
			}
			if mode == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if (mode == "foreign" || mode == "zombie") && (reads != 1 || opens != 0) {
				t.Fatal("mixed observation or pinned foreign process")
			}
		})
	}
}
