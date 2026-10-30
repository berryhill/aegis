//go:build linux

package hermes

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"testing/fstest"
)

func contributionProcEntries(t *testing.T) []os.DirEntry {
	t.Helper()
	entries, err := fstest.MapFS{"100": &fstest.MapFile{}, "101": &fstest.MapFile{}, "self": &fstest.MapFile{}}.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func contributionProcStat(group int, state string) []byte {
	fields := []string{state, "1", strconv.Itoa(group)}
	for len(fields) < 19 {
		fields = append(fields, "0")
	}
	fields = append(fields, "12345")
	return []byte("100 (name with ) spaces) " + strings.Join(fields, " "))
}

func TestContributionProcDisappearance(t *testing.T) {
	scans := map[string]func(int, func(string) ([]os.DirEntry, error), func(string) ([]byte, error)) (bool, error){
		"empty": contributionGroupEmptyFrom, "quiescent": contributionGroupQuiescentFrom,
	}
	for name, scan := range scans {
		t.Run(name, func(t *testing.T) {
			for _, errno := range []error{syscall.ENOENT, syscall.ESRCH} {
				t.Run(errno.Error(), func(t *testing.T) {
					vanished := fmt.Errorf("observe: %w", &os.PathError{Op: "read", Path: "/proc/100/stat", Err: errno})
					for _, live := range []bool{false, true} {
						t.Run(fmt.Sprint("live=", live), func(t *testing.T) {
							empty, err := scan(42, func(string) ([]os.DirEntry, error) { return contributionProcEntries(t), nil }, func(path string) ([]byte, error) {
								if path == "/proc/100/stat" {
									return nil, vanished
								}
								group := 99
								if live {
									group = 42
								}
								return contributionProcStat(group, "S"), nil
							})
							if err != nil || empty == live {
								t.Fatalf("empty=%v err=%v live=%v", empty, err, live)
							}
						})
					}
					// Failure to enumerate /proc itself is NOT a disappearing enumerated PID.
					empty, err := scan(42, func(string) ([]os.DirEntry, error) { return nil, vanished }, func(string) ([]byte, error) { t.Fatal("read after failed enumeration"); return nil, nil })
					if empty || !errors.Is(err, errno) {
						t.Fatalf("enumeration accepted: %v %v", empty, err)
					}
					// The same disappearance cannot establish an owned process identity.
					_, _, err = contributionProcessIdentityFrom(100, func(string) ([]byte, error) { return nil, vanished })
					if !errors.Is(err, errno) {
						t.Fatalf("identity error lost: %v", err)
					}
				})
			}
		})
	}
}

func TestContributionProcObservationFailClosed(t *testing.T) {
	for name, scan := range map[string]func(int, func(string) ([]os.DirEntry, error), func(string) ([]byte, error)) (bool, error){"empty": contributionGroupEmptyFrom, "quiescent": contributionGroupQuiescentFrom} {
		t.Run(name, func(t *testing.T) {
			for _, errno := range []error{syscall.EACCES, syscall.EPERM, syscall.EIO} {
				failure := fmt.Errorf("observe: %w", &os.PathError{Op: "read", Path: "/proc/100/stat", Err: errno})
				empty, err := scan(42, func(string) ([]os.DirEntry, error) { return contributionProcEntries(t), nil }, func(string) ([]byte, error) { return nil, failure })
				if empty || !errors.Is(err, errno) {
					t.Fatalf("observation accepted: %v %v", empty, err)
				}
			}
			for _, raw := range [][]byte{nil, []byte("100 malformed"), []byte("100 (x) S"), contributionProcStat(-1, "S"), contributionProcStat(99, "?"), []byte(strings.Replace(string(contributionProcStat(99, "S")), "12345", "invalid", 1))} {
				empty, err := scan(42, func(string) ([]os.DirEntry, error) { return contributionProcEntries(t), nil }, func(string) ([]byte, error) { return raw, nil })
				if empty || err == nil {
					t.Fatalf("malformed accepted: %q empty=%v err=%v", raw, empty, err)
				}
			}
		})
	}
}

func TestContributionProcQuiescentSingleSnapshot(t *testing.T) {
	// A second read can observe a reused PID with a different group. Do not combine
	// membership from one process with the state of its replacement.
	calls := 0
	empty, err := contributionGroupQuiescentFrom(42, func(string) ([]os.DirEntry, error) { return contributionProcEntries(t)[:1], nil }, func(string) ([]byte, error) {
		calls++
		if calls == 1 {
			return contributionProcStat(42, "S"), nil
		}
		return contributionProcStat(99, "Z"), nil
	})
	if empty || err != nil || calls != 1 {
		t.Fatalf("mixed snapshots: empty=%v err=%v reads=%d", empty, err, calls)
	}
	for _, state := range []string{"S", "Z"} {
		empty, err := contributionGroupEmptyFrom(42, func(string) ([]os.DirEntry, error) { return contributionProcEntries(t)[:1], nil }, func(string) ([]byte, error) { return contributionProcStat(42, state), nil })
		if empty || err != nil {
			t.Fatalf("empty accepted member %s: %v %v", state, empty, err)
		}
		quiet, err := contributionGroupQuiescentFrom(42, func(string) ([]os.DirEntry, error) { return contributionProcEntries(t)[:1], nil }, func(string) ([]byte, error) { return contributionProcStat(42, state), nil })
		if err != nil || quiet != (state == "Z") {
			t.Fatalf("quiescent state %s: %v %v", state, quiet, err)
		}
	}
}
