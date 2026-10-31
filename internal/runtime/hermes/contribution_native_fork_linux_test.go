//go:build linux

package hermes

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/execution"
	"golang.org/x/sys/unix"
)

func TestContributionNativeForkAfterEnumeration(t *testing.T) {
	dir := t.TempDir()
	// The enumerated parent forks a new writer, then exits before stat is read.
	script := `(while [ ! -f "$1/go" ]; do :; done; (while :; do printf x >> "$1/writes"; sleep 0.001; done) & echo $! > "$1/writer"; exit 0) & echo $! > "$1/worker"; exit 0`
	cmd := exec.Command("/bin/sh", "-c", script, "fixture", dir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first, transitioned, joined := true, false, false
	initialEmpty := false
	err := runContributionNative(ctx, execution.NativeBinding{CommandDigest: NativeCommandDigest(cmd)}, cmd,
		func(context.Context) error { return nil },
		func(_ context.Context, p NativeCompletion) error { joined = p.Joined(); return nil },
		func(ctx context.Context, group int) (bool, error) {
			if !first {
				return terminateContributionGroup(ctx, group)
			}
			first = false
			empty, err := terminateContributionGroupFrom(ctx, group, func(path string) ([]os.DirEntry, error) {
				entries, err := os.ReadDir(path)
				if err != nil {
					return nil, err
				}
				if err = os.WriteFile(filepath.Join(dir, "go"), nil, 0600); err != nil {
					return nil, err
				}
				raw, err := os.ReadFile(filepath.Join(dir, "worker"))
				if err != nil {
					return nil, err
				}
				pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
				if err != nil {
					return nil, err
				}
				for {
					_, _, state, err := contributionProcessStatFrom(pid, os.ReadFile)
					_, writerErr := os.Stat(filepath.Join(dir, "writer"))
					if (contributionProcDisappeared(err) || state == "Z") && writerErr == nil {
						transitioned = true
						break
					}
					if ctx.Err() != nil {
						return nil, ctx.Err()
					}
					time.Sleep(time.Millisecond)
				}
				return entries, nil
			}, os.ReadFile, unix.PidfdOpen, unix.PidfdSendSignal, unix.Close)
			initialEmpty = empty
			return empty, err
		})
	if err != nil || !transitioned || !joined || !initialEmpty {
		t.Fatalf("err=%v transition=%v joined=%v initialEmpty=%v", err, transitioned, joined, initialEmpty)
	}
	before, _ := os.ReadFile(filepath.Join(dir, "writes"))
	time.Sleep(30 * time.Millisecond)
	after, _ := os.ReadFile(filepath.Join(dir, "writes"))
	if len(before) != len(after) {
		t.Fatal("writer survived acknowledged cleanup")
	}
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
