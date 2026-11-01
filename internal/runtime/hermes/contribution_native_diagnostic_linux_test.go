//go:build linux

package hermes

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/execution"
	"golang.org/x/sys/unix"
)

func TestNativeDiagnosticClosedRenderAndIdentity(t *testing.T) {
	for _, tc := range []struct {
		err   error
		class string
	}{
		{context.DeadlineExceeded, "deadline"}, {context.Canceled, "cancelled"}, {unix.EACCES, "access"}, {unix.EPERM, "permission"}, {unix.ECHILD, "child_lost"}, {unix.ESRCH, "process_gone"}, {unix.ENOENT, "not_found"}, {unix.EINVAL, "invalid"}, {execution.ErrNativeQuiescenceUnavailable, "identity_or_stat_invalid"}, {errors.New("SECRET /private/prompt token=credential"), "other"},
	} {
		raw := fmt.Errorf("SECRET /private/prompt token=credential: %w", tc.err)
		stage := nativeStage("observe_readdir", raw)
		if !errors.Is(stage, tc.err) {
			t.Fatal("helper identity lost")
		}
		d := &nativeFailureDiagnostic{first: *stage, joinExpired: true, elapsed: 2 * time.Second}
		if !errors.Is(d, execution.ErrNativeQuiescenceUnavailable) || errors.Is(d, tc.err) && tc.err != execution.ErrNativeQuiescenceUnavailable {
			t.Fatal("external identity is not sentinel-only")
		}
		text := d.Error()
		if strings.Contains(text, "SECRET") || strings.Contains(text, "private") || strings.Contains(text, "credential") || !strings.Contains(text, "class="+tc.class) || !strings.Contains(text, "cleanup_ms=1000") {
			t.Fatal(text)
		}
	}
	d := &nativeFailureDiagnostic{first: nativeFailureStage{stage: "SECRET", err: errors.New("SECRET")}, elapsed: -time.Second}
	if strings.Contains(d.Error(), "SECRET") || !strings.Contains(d.Error(), "cleanup_ms=0") {
		t.Fatal(d)
	}
}

// The observer deliberately consumes the EXISTING cleanup deadline; no new
// timer, global hook, child Wait implementation, or production selector is used.
func TestNativeDiagnosticFirstUnknownSurvivesJoinExpiry(t *testing.T) {
	cmd := exec.Command("/bin/echo", "fixture")
	gate := make(chan struct{})
	defer close(gate)
	cmd.Stdout = nativeDiagnosticGateWriter{gate: gate}
	joined := true
	err := runContributionNativeWithBarrier(context.Background(), execution.NativeBinding{CommandDigest: NativeCommandDigest(cmd)}, cmd,
		func(context.Context) error { return nil }, func(_ context.Context, p NativeCompletion) error { joined = p.Joined(); return nil },
		func(ctx context.Context, _ int) (bool, error) {
			<-ctx.Done()
			return false, nativeStage("observe_readdir", fmt.Errorf("SECRET: %w", unix.EACCES))
		},
		func(context.Context, int) error { return nil })
	var d *nativeFailureDiagnostic
	if joined || !errors.As(err, &d) || d.first.stage != "observe_readdir" || nativeFailureClass(d.first.err) != "access" {
		t.Fatalf("joined=%v err=%v", joined, err)
	}
	if !d.joinExpired {
		t.Fatal("terminal join expiry not retained")
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Fatal("payload escaped")
	}
}

type nativeDiagnosticGateWriter struct{ gate <-chan struct{} }

func (w nativeDiagnosticGateWriter) Write(p []byte) (int, error) { <-w.gate; return len(p), nil }

func TestNativeDiagnosticActualStageFailureAndFinishPrecedence(t *testing.T) {
	finishErr := errors.New("finish failure")
	for _, failFinish := range []bool{false, true} {
		cmd := exec.Command("/bin/true")
		joined := true
		err := runContributionNativeWithBarrier(context.Background(), execution.NativeBinding{CommandDigest: NativeCommandDigest(cmd)}, cmd,
			func(context.Context) error { return nil }, func(_ context.Context, p NativeCompletion) error {
				joined = p.Joined()
				if failFinish {
					return finishErr
				}
				return nil
			},
			func(ctx context.Context, group int) (bool, error) {
				return terminateContributionGroupFrom(ctx, group, func(string) ([]os.DirEntry, error) { return nil, fmt.Errorf("SECRET: %w", unix.EACCES) }, os.ReadFile, unix.PidfdOpen, unix.PidfdSendSignal, unix.Close)
			},
			func(context.Context, int) error { return nil })
		if joined {
			t.Fatal("denial minted joined proof")
		}
		if failFinish {
			if err != finishErr {
				t.Fatal("finish precedence lost")
			}
			continue
		}
		var d *nativeFailureDiagnostic
		if !errors.As(err, &d) || d.first.stage != "observe_readdir" || !errors.Is(err, execution.ErrNativeQuiescenceUnavailable) || strings.Contains(err.Error(), "SECRET") {
			t.Fatal(err)
		}
	}
}

func TestNativeDiagnosticBarrierIdentity(t *testing.T) {
	err := killPinnedContributionGroup(context.Background(), 42, func(int, int, *unix.Siginfo, int, *unix.Rusage) error { return fmt.Errorf("SECRET: %w", unix.ECHILD) }, func(int, unix.Signal) error { t.Fatal("signal after denial"); return nil })
	var stage *nativeFailureStage
	if !errors.Is(err, unix.ECHILD) || !errors.As(err, &stage) || stage.stage != "barrier_waitid" || strings.Contains(err.Error(), "SECRET") {
		t.Fatal(err)
	}
	err = killPinnedContributionGroup(context.Background(), 42, func(int, int, *unix.Siginfo, int, *unix.Rusage) error { return nil }, func(int, unix.Signal) error { return unix.EPERM })
	if !errors.Is(err, unix.EPERM) || !errors.As(err, &stage) || stage.stage != "barrier_signal" {
		t.Fatal(err)
	}
}
