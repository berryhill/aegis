package command

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	managerdomain "github.com/berryhill/aegis/internal/manager"
)

func blockedCertificationExecutor(t *testing.T, timeout time.Duration, progress func(string)) (liveConformanceExecutor, *io.PipeWriter) {
	t.Helper()
	reader, writer := io.Pipe()
	client, err := managerdomain.NewGatewayClient(reader, io.Discard, 4096)
	if err != nil {
		t.Fatal(err)
	}
	var budget atomic.Int32
	return liveConformanceExecutor{gateway: client, session: "fixture-session", budget: &budget, maximum: 4096, timeout: timeout, progress: progress}, writer
}

func TestLiveConformanceTurnTimeoutIsBoundedAndAbortsCorpus(t *testing.T) {
	var calls atomic.Int32
	executor, writer := blockedCertificationExecutor(t, 25*time.Millisecond, func(string) { calls.Add(1) })
	defer writer.Close()
	candidate := managerdomain.Candidates()[0]
	started := time.Now()
	_, err := managerdomain.RunCertification(context.Background(), executor, candidate, candidate.OllamaName, "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "Q4", "0.18.2", "0.32.0", 65536, time.Now())
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("blocked turn exceeded bound: %s", elapsed)
	}
	var failure *managerdomain.ConformanceFailure
	if !errors.As(err, &failure) || failure.CaseID != "strict-envelope" || failure.Reason != managerdomain.ReasonTurnTimeout {
		t.Fatalf("timeout error=%v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("corpus continued after timeout: calls=%d", calls.Load())
	}
}

func TestLiveConformanceCancellationPropagatesPromptly(t *testing.T) {
	executor, writer := blockedCertificationExecutor(t, time.Minute, nil)
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	_, err := executor.Execute(ctx, managerdomain.ConformanceCorpus()[0])
	if time.Since(started) > 500*time.Millisecond {
		t.Fatal("cancellation did not propagate promptly")
	}
	var failure *managerdomain.ConformanceFailure
	if !errors.As(err, &failure) || failure.Reason != managerdomain.ReasonStartupCancelled || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error=%v", err)
	}
}

func TestLiveConformanceAuthorityExpiryIsExplicitAndFailClosed(t *testing.T) {
	executor, writer := blockedCertificationExecutor(t, time.Minute, nil)
	defer writer.Close()
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(20*time.Millisecond))
	defer cancel()
	_, err := executor.Execute(ctx, managerdomain.ConformanceCorpus()[0])
	var failure *managerdomain.ConformanceFailure
	if !errors.As(err, &failure) || failure.Reason != managerdomain.ReasonSessionExpired || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("authority expiry error=%v", err)
	}
}

func TestCertificationFailureRunsCleanupInReverseOrder(t *testing.T) {
	var calls []int
	cleanup := &certificationCleanup{}
	cleanup.add(func() { calls = append(calls, 1) })
	cleanup.add(func() { calls = append(calls, 2) })
	func() {
		defer cleanup.close()
		executor, writer := blockedCertificationExecutor(t, 20*time.Millisecond, nil)
		defer writer.Close()
		_, _ = executor.Execute(context.Background(), managerdomain.ConformanceCorpus()[0])
	}()
	if len(calls) != 2 || calls[0] != 2 || calls[1] != 1 {
		t.Fatalf("cleanup order=%v", calls)
	}
}
