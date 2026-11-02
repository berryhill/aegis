//go:build linux

package hermes

import (
	"context"
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"strings"
	"testing"
)

type nativeExitEntry struct {
	os.DirEntry
	name string
}

func (e nativeExitEntry) Name() string { return e.name }

// Synthetic complete metadata; the captured-kernel test uses real bytes.
func nativeExitStat(group int, state string) []byte {
	raw := contributionProcStat(group, state)
	fields := strings.Fields(string(raw[strings.LastIndexByte(string(raw), ')')+1:]))
	return append(raw, []byte(strings.Repeat(" 0", 50-len(fields)))...)
}

func TestContributionNativeKernelExitSnapshot(t *testing.T) {
	// Actual /proc snapshot of the probe's own exiting child, not provider output.
	captured := []byte("1465333 (sh) X 0 -1 -1 0 -1 4227084 71 0 0 0 0 0 0 0 20 0 0 0 133985412 0 0 0 0 0 0 0 0 0 0 0 0 1 0 0 17 7 0 0 0 0 0 0 0 0 0 0 0 0 0\n")
	reads, opens, signals := 0, 0, 0
	empty, err := terminateContributionGroupFrom(context.Background(), 42,
		func(string) ([]os.DirEntry, error) {
			return []os.DirEntry{nativeExitEntry{contributionProcEntries(t)[0], "1465333"}}, nil
		},
		func(string) ([]byte, error) { reads++; return captured, nil },
		func(int, int) (int, error) { opens++; return 7, nil },
		func(int, unix.Signal, *unix.Siginfo, int) error { signals++; return nil },
		func(int) error { return nil })
	if err != nil || !empty || reads != 1 || opens != 0 || signals != 0 {
		t.Fatalf("empty=%v err=%v reads=%d opens=%d signals=%d", empty, err, reads, opens, signals)
	}
	if _, _, _, err := contributionProcessStatFrom(100, func(string) ([]byte, error) { return captured, nil }); err == nil {
		t.Fatal("terminal census was promoted into role identity")
	}
}

func TestContributionNativeExitSnapshotBoundaries(t *testing.T) {
	for _, mode := range []string{"X", "x", "Z", "live", "other_negative", "noncanonical", "missing", "short", "truncated_start_present", "bad_prefix", "bad_tail", "bad_start", "overflow_start", "negative_start", "read_denied", "read_io"} {
		t.Run(mode, func(t *testing.T) {
			raw := nativeExitStat(-1, "X")
			var readErr error
			switch mode {
			case "X", "x", "Z":
				raw = nativeExitStat(-1, mode)
			case "live":
				raw = nativeExitStat(-1, "R")
			case "other_negative":
				raw = nativeExitStat(-2, "X")
			case "noncanonical":
				raw = []byte(strings.Replace(string(raw), " -1 ", " -01 ", 1))
			case "missing":
				raw = []byte("bad")
			case "short":
				raw = []byte("100 (sh) X 1 -1")
			case "bad_start":
				raw = []byte(strings.Replace(string(raw), "12345", "bad", 1))
			case "truncated_start_present":
				raw = contributionProcStat(-1, "X")
			case "bad_prefix":
				raw = []byte(strings.Replace(string(raw), "100 (", "101 (", 1))
			case "bad_tail":
				raw = append(raw[:len(raw)-1], 'q')
			case "overflow_start":
				raw = []byte(strings.Replace(string(raw), "12345", "18446744073709551616", 1))
			case "negative_start":
				raw = []byte(strings.Replace(string(raw), "12345", "-1", 1))
			case "read_denied":
				readErr = unix.EACCES
			case "read_io":
				readErr = unix.EIO
			}
			opens, signals := 0, 0
			empty, err := terminateContributionGroupFrom(context.Background(), 42, func(string) ([]os.DirEntry, error) { return contributionProcEntries(t)[:1], nil }, func(string) ([]byte, error) { return raw, readErr }, func(int, int) (int, error) { opens++; return 7, nil }, func(int, unix.Signal, *unix.Siginfo, int) error { signals++; return nil }, func(int) error { return nil })
			terminal := mode == "X"
			if empty != terminal || (err == nil) != terminal || opens != 0 || signals != 0 {
				t.Fatalf("empty=%v err=%v opens=%d signals=%d", empty, err, opens, signals)
			}
		})
	}
}

func TestContributionNativeExitRecheckNoSignal(t *testing.T) {
	for _, mode := range []string{"terminal", "changed_start", "close_denied", "live", "foreign_terminal", "truncated", "read_denied"} {
		t.Run(mode, func(t *testing.T) {
			reads, signals, closes := 0, 0, 0
			empty, err := terminateContributionGroupFrom(context.Background(), 42, func(string) ([]os.DirEntry, error) { return contributionProcEntries(t)[:1], nil }, func(string) ([]byte, error) {
				reads++
				if reads == 1 {
					return contributionProcStat(42, "S"), nil
				}
				raw := nativeExitStat(-1, "X")
				if mode == "changed_start" {
					raw = []byte(strings.Replace(string(raw), "12345", "12346", 1))
				}
				if mode == "live" {
					raw = nativeExitStat(-1, "R")
				}
				if mode == "foreign_terminal" {
					raw = nativeExitStat(99, "X")
				}
				if mode == "truncated" {
					raw = contributionProcStat(-1, "X")
				}
				if mode == "read_denied" {
					return raw, unix.EACCES
				}
				return raw, nil
			}, func(int, int) (int, error) { return 7, nil }, func(int, unix.Signal, *unix.Siginfo, int) error { signals++; return nil }, func(int) error {
				closes++
				if mode == "close_denied" {
					return unix.EIO
				}
				return nil
			})
			if empty || (err == nil) != (mode == "terminal") || signals != 0 || closes != 1 {
				t.Fatalf("empty=%v err=%v signals=%d closes=%d", empty, err, signals, closes)
			}
			if mode == "close_denied" && !errors.Is(err, unix.EIO) {
				t.Fatal(err)
			}
		})
	}
}
