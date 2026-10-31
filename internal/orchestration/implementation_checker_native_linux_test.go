//go:build linux

package orchestration

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/implementation"
)

func TestImplementationCheckerNative(t *testing.T) {
	for _, mode := range []string{"success", "exit", "cancel", "deadline", "missing-executable", "pre-cancel", "unjoined"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			pidfile := filepath.Join(dir, "pid")
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			cmd := exec.Command("/bin/sh", "-c", `/bin/sleep 60 & echo $! > "$1"; exit 0`, "fixture", pidfile)
			switch mode {
			case "exit":
				cmd.Args = []string{"sh", "-c", "exit 7"}
			case "cancel", "deadline":
				cmd.Args = []string{"sh", "-c", `/bin/sleep 60 & echo $! > "$1"; wait`, "fixture", pidfile}
				if mode == "deadline" {
					var stop context.CancelFunc
					ctx, stop = context.WithTimeout(ctx, 100*time.Millisecond)
					defer stop()
				}
			case "missing-executable":
				cmd = exec.Command(filepath.Join(dir, "missing"))
			case "pre-cancel":
				cancel()
			case "unjoined":
				// A started command without this runner's joined completion must never
				// return its ordinary command error as if cleanup had been proven.
				cmd = exec.Command("/bin/true")
				if err := cmd.Run(); err != nil {
					t.Fatal(err)
				}
			}
			done := make(chan struct{})
			if mode == "cancel" {
				go func() {
					defer close(done)
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
			err := runImplementationCheckerNative(ctx, cmd)
			if mode == "cancel" {
				<-done
			}
			switch mode {
			case "success":
				if err != nil {
					t.Fatal(err)
				}
			case "exit":
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 7 {
					t.Fatalf("lost exit status: %v", err)
				}
			case "cancel", "pre-cancel":
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("lost cancellation: %v", err)
				}
			case "deadline":
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("lost deadline: %v", err)
				}
			case "missing-executable":
				if err == nil || errors.Is(err, implementation.ErrCheckerQuiescenceUnavailable) || cmd.Process != nil {
					t.Fatalf("start failure normalized as unknown: %v", err)
				}
			case "unjoined":
				if !errors.Is(err, implementation.ErrCheckerQuiescenceUnavailable) {
					t.Fatalf("unjoined accepted: %v", err)
				}
			}
			if mode == "success" || mode == "cancel" || mode == "deadline" {
				raw, err := os.ReadFile(pidfile)
				if err != nil {
					t.Fatal(err)
				}
				stat, err := os.ReadFile("/proc/" + strings.TrimSpace(string(raw)) + "/stat")
				if err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
				if err == nil && !strings.Contains(string(stat), ") Z ") && !strings.Contains(string(stat), ") X ") && !strings.Contains(string(stat), ") x ") {
					t.Fatalf("live descendant at production port return: %s", stat)
				}
			}
		})
	}
}
