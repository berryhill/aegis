//go:build linux

package hermes

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/execution"
)

func TestContributionProcNativeAdmissionBoundaries(t *testing.T) {
	for _, kind := range []string{"canceled", "deadline", "reservation-denied", "canceled-in-reservation"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			want := context.Canceled
			switch kind {
			case "canceled":
				cancel()
			case "deadline":
				var expired context.CancelFunc
				ctx, expired = context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer expired()
				want = context.DeadlineExceeded
			case "reservation-denied":
				want = ErrContributionStopUnavailable
			}
			cmd := exec.Command("true")
			b := execution.NativeBinding{IntentID: "boundary", CommandDigest: NativeCommandDigest(cmd)}
			reserved, finished := false, false
			err := RunContributionNative(ctx, b, cmd, func(context.Context) error {
				reserved = true
				if kind == "reservation-denied" {
					return want
				}
				cancel()
				return nil
			}, func(_ context.Context, p NativeCompletion) error {
				finished = true
				if p.Joined() || p.Binding() != b || cmd.Process != nil {
					t.Fatal("cancellation acknowledged unstarted command")
				}
				return nil
			})
			if !errors.Is(err, want) || cmd.Process != nil {
				t.Fatalf("continued after denial: %v process=%v", err, cmd.Process)
			}
			if reserved != (kind == "reservation-denied" || kind == "canceled-in-reservation") || finished != (kind == "canceled-in-reservation") {
				t.Fatalf("unexpected callbacks reserved=%v finished=%v", reserved, finished)
			}
		})
	}
}

func TestContributionProcOwnedGroupMismatch(t *testing.T) {
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &processState{cmd: cmd, done: make(chan error, 1)}
	go func() { p.done <- cmd.Wait(); close(p.done) }()
	t.Cleanup(func() { _ = cmd.Process.Kill(); <-p.done })
	a := &Adapter{processes: map[string]*processState{"runtime": p}}
	start, group, err := contributionProcessIdentity(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if group == cmd.Process.Pid {
		t.Fatal("fixture must not be a group leader")
	}
	b := ContributionStopBinding{SessionID: "session", RuntimeSessionID: "runtime", AuthorityID: "authority", AuthorityDigest: "digest", Role: "builder", Epoch: 1, PID: cmd.Process.Pid, ProcessStart: start}
	// Keep the exact PID and start, but remove the expected group identity. Neither
	// stop nor crash recovery may acknowledge or signal this still-live process.
	if _, _, err := a.ContributionProcessIdentity(b.RuntimeSessionID, b.PID, b.ProcessStart); err == nil {
		t.Fatal("non-leader admitted")
	}
	proof, err := a.StopContributionRole(context.Background(), b)
	if err == nil || proof.RoleProcessAcknowledged() {
		t.Fatalf("stop acknowledged mismatch: %+v %v", proof, err)
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		t.Fatal(err)
	}
	proof, err = (&Adapter{}).RecoverContributionRole(context.Background(), b, strings.TrimSpace(string(boot)), uint32(os.Getuid()))
	if err == nil || proof.RoleProcessAcknowledged() {
		t.Fatalf("recovery acknowledged mismatch: %+v %v", proof, err)
	}
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatal("denial signaled child", err)
	}
}
