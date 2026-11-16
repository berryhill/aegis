//go:build linux

package hermes

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/execution"
	"golang.org/x/sys/unix"
)

const attemptResistantChild = `/bin/sh -c 'trap "" TERM; printf "%s\n" "$$" > "$HERMES_PYTHON_SRC_ROOT/descendant.pid"; exec sleep 30' &
while [ ! -s "$HERMES_PYTHON_SRC_ROOT/descendant.pid" ]; do sleep 0.01; done
`

func TestAttemptOwnedDescendantLifecycle(t *testing.T) {
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	defer unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 0, 0, 0, 0)
	for _, design := range []bool{false, true} {
		for _, mode := range []string{"denied", "normal", "early-exit", "cancel", "error"} {
			t.Run(fmt.Sprintf("design=%t/%s", design, mode), func(t *testing.T) {
				before, after, complete := attemptResistantChild, "", syntheticToolComplete
				switch mode {
				case "denied":
					before += syntheticToolEvent("tool.start", "selected")
				case "early-exit":
					before += "exit 7\n"
				case "cancel":
					after, complete = "while :; do sleep 0.01; done\n", ""
				case "error":
					before += syntheticToolEvent("error", "selected")
				}
				script := syntheticToolGateway(before, after, complete)
				if design {
					script = strings.ReplaceAll(script, "SYNTHETIC_PRIVATE_PREVIEW", "<aegis-charter>{}</aegis-charter>")
				}
				a, root := attemptTestAdapter(t, script)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				childPath := filepath.Join(root, "install", "descendant.pid")
				// Fixtures own and reap any leak even when a product assertion fails.
				t.Cleanup(func() {
					b, err := os.ReadFile(childPath)
					if err != nil {
						return
					}
					pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
					if pid <= 0 {
						return
					}
					fd, err := unix.PidfdOpen(pid, 0)
					if err != nil {
						return
					}
					defer unix.Close(fd)
					_ = unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0)
					var info unix.Siginfo
					_ = unix.Waitid(unix.P_PIDFD, fd, &info, unix.WEXITED, nil)
				})
				cancelDone := make(chan struct{})
				if mode == "cancel" {
					go func() {
						defer close(cancelDone)
						for ctx.Err() == nil {
							if _, err := os.Stat(childPath); err == nil {
								cancel()
								return
							}
							time.Sleep(time.Millisecond)
						}
					}()
				} else {
					close(cancelDone)
				}
				start := time.Now()
				var output string
				var err error
				if design {
					output, _, err = a.DesignProposal(ctx, filepath.Join(root, "state"), "synthetic", false, nil)
				} else {
					var result AttemptTurnResult
					result, err = a.AttemptTurn(ctx, validAttemptTurnRequest(root))
					output = result.Output
					if mode == "normal" && result.Attempt.State != execution.StateSucceeded {
						t.Fatalf("%+v %v", result, err)
					}
				}
				<-cancelDone
				if mode == "normal" {
					if err != nil || output == "" {
						t.Fatalf("normal: %q %v", output, err)
					}
				} else if err == nil || output != "" {
					t.Fatalf("failure: %q %v", output, err)
				}
				if mode == "cancel" && !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				if errors.Is(err, ErrAttemptCleanup) {
					t.Fatal(err)
				}
				if time.Since(start) > 4*time.Second {
					t.Fatal("unbounded lifecycle")
				}
				assertSyntheticGatewayClean(t, root)
				b, err := os.ReadFile(childPath)
				if err != nil {
					t.Fatal(err)
				}
				pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
				if err != nil {
					t.Fatal(err)
				}
				if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
					t.Fatal("owned descendant survives return", pid, err)
				}
			})
		}
	}
}

func TestAttemptOwnedCleanupFailureDoesNotAcknowledge(t *testing.T) {
	for _, stage := range []string{"ownership", "barrier", "observation", "deadline", "lost-wait"} {
		t.Run(stage, func(t *testing.T) {
			cmd := exec.Command("/bin/sh", "-c", "exit 0")
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			if err := waitAttemptLeader(context.Background(), cmd.Process.Pid); err != nil {
				t.Fatal(err)
			}
			calls := 0
			signal := func(ctx context.Context, group int, sig unix.Signal) error {
				calls++
				if stage == "ownership" || (stage == "barrier" && sig == unix.SIGKILL) {
					return unix.ECHILD
				}
				return killPinnedContributionGroup(ctx, group, unix.Waitid, unix.Kill)
			}
			observed := false
			observe := func(ctx context.Context, _ int) (bool, error) {
				observed = true
				switch stage {
				case "observation":
					return false, os.ErrPermission
				case "deadline":
					<-ctx.Done()
					return true, nil // Late success must deny.
				case "lost-wait":
					var status syscall.WaitStatus
					_, err := syscall.Wait4(cmd.Process.Pid, &status, 0, nil)
					if err != nil {
						t.Fatal(err)
					}
				}
				return true, nil
			}
			start := time.Now()
			if err := stopOwnedAttemptProcessWith(cmd, io.NopCloser(strings.NewReader("")), signal, observe); !errors.Is(err, ErrAttemptCleanup) {
				t.Fatal("false cleanup success", err)
			}
			if time.Since(start) > 4*time.Second {
				t.Fatal("cleanup deadline not bounded")
			}
			if stage == "ownership" && (calls != 1 || observed) {
				t.Fatal("continued group operations after ownership loss")
			}
			if stage == "barrier" && observed {
				t.Fatal("accepted census without barrier")
			}
		})
	}
}

func TestAttemptOwnedReapCannotConsumeLeaderOrLiveChild(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	pid := cmd.Process.Pid
	if err := reapAttemptDescendant(pid, pid); err != nil {
		t.Fatal(err)
	}
	if err := reapAttemptDescendant(pid, pid+1); !errors.Is(err, ErrAttemptCleanup) {
		t.Fatal("accepted foreign/live process", err)
	}
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatal("foreign process affected", err)
	}
}
