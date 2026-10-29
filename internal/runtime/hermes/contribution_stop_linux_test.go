//go:build linux

package hermes

import (
	"context"
	"errors"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// Real harmless child, real command.Wait; no synthetic stop result injection.
func contributionStopChild(t *testing.T, script string) (*Adapter, *processState, ContributionStopBinding) {
	t.Helper()
	cmd := exec.Command("sh", "-c", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &processState{cmd: cmd, done: make(chan error, 1)}
	a := &Adapter{processes: map[string]*processState{"runtime": p}}
	start, _, err := contributionProcessIdentity(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	go func() { p.done <- cmd.Wait(); close(p.done) }()
	t.Cleanup(func() { _ = cmd.Process.Kill(); <-p.done })
	return a, p, ContributionStopBinding{SessionID: "session", RuntimeSessionID: "runtime", AuthorityID: "authority", AuthorityDigest: "digest", Role: "builder", Epoch: 1, PID: cmd.Process.Pid, ProcessStart: start}
}
func TestContributionStopOwnedProcess(t *testing.T) {
	a, _, b := contributionStopChild(t, "exec sleep 60")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	proof, err := a.StopContributionRole(ctx, b)
	if err != nil || !proof.RoleProcessAcknowledged() || proof.Binding() != b || proof.BootID() == "" {
		t.Fatalf("%+v %v", proof, err)
	}
}
func TestContributionStopDenials(t *testing.T) {
	for _, kind := range []string{"timeout", "missing-owner", "pid-reuse", "crash"} {
		t.Run(kind, func(t *testing.T) {
			a, p, b := contributionStopChild(t, "exec sleep 60")
			ctx := context.Background()
			switch kind {
			case "timeout":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "missing-owner":
				delete(a.processes, "runtime")
			case "pid-reuse":
				b.ProcessStart = "different-start"
			case "crash":
				_ = p.cmd.Process.Kill()
				<-p.done
			}
			proof, err := a.StopContributionRole(ctx, b)
			if err == nil || proof.RoleProcessAcknowledged() {
				t.Fatalf("acknowledged %s: %+v %v", kind, proof, err)
			}
			if kind == "timeout" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if kind == "pid-reuse" || kind == "missing-owner" {
				if err = p.cmd.Process.Signal(syscall.Signal(0)); err != nil {
					t.Fatal("denial signalled unrelated child", err)
				}
			}
		})
	}
}
