//go:build linux

package hermes

import (
	"context"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestContributionNativeExitCensusContinues(t *testing.T) {
	for _, mode := range []string{"live", "denied", "malformed"} {
		for _, reverse := range []bool{false, true} {
			t.Run(mode+map[bool]string{false: "/dead-first", true: "/dead-last"}[reverse], func(t *testing.T) {
				base := contributionProcEntries(t)[0]
				entries := []os.DirEntry{nativeExitEntry{base, "100"}, nativeExitEntry{base, "101"}}
				if reverse {
					entries[0], entries[1] = entries[1], entries[0]
				}
				seen := map[string]int{}
				signals, closes := 0, 0
				empty, err := terminateContributionGroupFrom(context.Background(), 42,
					func(string) ([]os.DirEntry, error) { return entries, nil },
					func(path string) ([]byte, error) {
						seen[path]++
						if path == "/proc/100/stat" {
							return nativeExitStat(-1, "X"), nil
						}
						if mode == "denied" {
							return nil, unix.EACCES
						}
						if mode == "malformed" {
							return []byte("bad stat"), nil
						}
						return []byte(strings.Replace(string(contributionProcStat(42, "S")), "100 (", "101 (", 1)), nil
					},
					func(pid, flags int) (int, error) {
						if pid != 101 || flags != 0 {
							t.Fatal(pid, flags)
						}
						return 7, nil
					},
					func(int, unix.Signal, *unix.Siginfo, int) error { signals++; return nil },
					func(int) error { closes++; return nil })
				if empty || (err == nil) != (mode == "live") {
					t.Fatalf("empty=%v err=%v", empty, err)
				}
				if seen["/proc/101/stat"] == 0 {
					t.Fatal("terminal observation hid remaining inventory")
				}
				if mode == "live" && (signals != 1 || closes != 1 || seen["/proc/100/stat"] != 1) {
					t.Fatalf("signals=%d closes=%d seen=%v", signals, closes, seen)
				}
				if mode != "live" && (signals != 0 || closes != 0) {
					t.Fatal("denied inventory granted signaling")
				}
			})
		}
	}
}
