//go:build linux

package hermes

import (
	"context"
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

// The leader exits only after its ordinary background writer has published its
// identity. No inherited output pipe is needed to keep Cmd.Wait blocked.
func TestContributionNativeLeaderExitsBeforeWriter(t *testing.T) {
	for _, cancelRun := range []bool{false, true} {
		t.Run(strconv.FormatBool(cancelRun), func(t *testing.T) {
			dir := t.TempDir()
			pidfile := filepath.Join(dir, "pid")
			marker := filepath.Join(dir, "writes")
			cmd := exec.Command("sh", "-c", `(echo "$BASHPID" >/dev/null; while :; do echo x >> "$2"; done) & child=$!; echo "$child" > "$1"; exit 0`, "fixture", pidfile, marker)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			b := execution.NativeBinding{CommandDigest: NativeCommandDigest(cmd)}
			joined := false
			// Cleanup the exact fixture writer even on the baseline failure.
			defer func() {
				raw, _ := os.ReadFile(pidfile)
				pid, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
				if pid > 0 {
					fd, err := unix.PidfdOpen(pid, 0)
					if err == nil {
						_ = unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0)
						_ = unix.Close(fd)
					}
				}
			}()
			if cancelRun {
				go func() {
					for {
						if _, err := os.Stat(pidfile); err == nil {
							cancel()
							return
						}
						select {
						case <-ctx.Done():
							return
						case <-time.After(time.Millisecond):
						}
					}
				}()
			}
			err := RunContributionNative(ctx, b, cmd, func(context.Context) error { return nil }, func(_ context.Context, p NativeCompletion) error { joined = p.Joined(); return nil })
			if !joined {
				t.Fatalf("owned background writer not quiesced: %v", err)
			}
			if !cancelRun && err != nil {
				t.Fatal(err)
			}
			if cancelRun && ctx.Err() == nil {
				t.Fatal("fixture never cancelled")
			}
			before, _ := os.ReadFile(marker)
			time.Sleep(30 * time.Millisecond)
			after, _ := os.ReadFile(marker)
			if string(before) != string(after) {
				t.Fatal("writer survived acknowledged cleanup")
			}
		})
	}
}
