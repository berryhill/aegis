package command

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	managerdomain "github.com/berryhill/aegis/internal/manager"
)

func TestManagerCertifyExposesContinueOnErrorOption(t *testing.T) {
	flag := managerCertifyCmd(nil).Flags().Lookup("continue-on-error")
	if flag == nil || flag.DefValue != "false" {
		t.Fatalf("continue-on-error flag=%v", flag)
	}
}

func blockedCertificationExecutor(t *testing.T, timeout time.Duration, progress func(string)) (liveConformanceExecutor, *io.PipeWriter) {
	t.Helper()
	reader, writer := io.Pipe()
	client, err := managerdomain.NewGatewayClient(reader, io.Discard, 4096)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		_, _ = io.WriteString(writer, "{\"jsonrpc\":\"2.0\",\"id\":\"aegis-1\",\"result\":{\"session_id\":\"fixture-session\"}}\n")
	}()
	var budget atomic.Int32
	return liveConformanceExecutor{gateway: client, budget: &budget, maximum: 4096, timeout: timeout, progress: progress}, writer
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

func TestCertificationCleanupIsIdempotent(t *testing.T) {
	calls := 0
	cleanup := &certificationCleanup{}
	cleanup.add(func() error {
		calls++
		return nil
	})
	if err := cleanup.close(); err != nil {
		t.Fatal(err)
	}
	if err := cleanup.close(); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("cleanup calls=%d, want 1", calls)
	}
}

func TestCertificationFailureRunsCleanupInReverseOrder(t *testing.T) {
	var calls []int
	cleanup := &certificationCleanup{}
	cleanup.add(func() error { calls = append(calls, 1); return nil })
	cleanup.add(func() error { calls = append(calls, 2); return nil })
	func() {
		defer func() { _ = cleanup.close() }()
		executor, writer := blockedCertificationExecutor(t, 20*time.Millisecond, nil)
		defer writer.Close()
		_, _ = executor.Execute(context.Background(), managerdomain.ConformanceCorpus()[0])
	}()
	if len(calls) != 2 || calls[0] != 2 || calls[1] != 1 {
		t.Fatalf("cleanup order=%v", calls)
	}
}

func TestCertificationAmbiguousLoadPreservesPreExistingExactRunner(t *testing.T) {
	const model = "exact:model"
	digest := "sha256:" + strings.Repeat("a", 64)
	running := true
	var unloads int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/ps":
			models := []any{}
			if running {
				models = append(models, map[string]any{"name": model, "digest": digest})
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"models": models})
		case "/api/generate":
			var body map[string]any
			_ = json.NewDecoder(request.Body).Decode(&body)
			if body["keep_alive"] == float64(0) {
				unloads++
				running = false
				_ = json.NewEncoder(writer).Encode(map[string]any{"done": true})
				return
			}
			http.Error(writer, "ambiguous load outcome", http.StatusInternalServerError)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client, err := managerdomain.NewOllamaClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	cleanup := &certificationCleanup{}
	err = loadCertificationModel(context.Background(), cleanup, client, model, digest, 65536, time.Minute, time.Second)
	if err == nil || !strings.Contains(err.Error(), managerdomain.ReasonModelLoadFailed) {
		t.Fatalf("load error=%v", err)
	}
	if cleanupErr := cleanup.close(); cleanupErr != nil {
		t.Fatalf("cleanup error=%v", cleanupErr)
	}
	if unloads != 0 || !running {
		t.Fatalf("pre-existing exact runner was mutated: unloads=%d running=%v", unloads, running)
	}
}

func TestCertificationCleanupUnloadsSuccessfulAegisOwnedRunner(t *testing.T) {
	const model = "exact:model"
	digest := "sha256:" + strings.Repeat("a", 64)
	running := false
	unloads := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/ps":
			models := []any{}
			if running {
				models = append(models, map[string]any{"name": model, "digest": digest})
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"models": models})
		case "/api/generate":
			var body map[string]any
			_ = json.NewDecoder(request.Body).Decode(&body)
			if body["keep_alive"] == float64(0) {
				unloads++
				running = false
			} else {
				running = true
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"done": true})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client, err := managerdomain.NewOllamaClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	cleanup := &certificationCleanup{}
	if err = loadCertificationModel(context.Background(), cleanup, client, model, digest, 65536, time.Minute, time.Second); err != nil {
		t.Fatal(err)
	}
	if err = cleanup.close(); err != nil {
		t.Fatal(err)
	}
	if unloads != 1 || running {
		t.Fatalf("owned runner cleanup: unloads=%d running=%v", unloads, running)
	}
}

func TestCertificationCleanupJoinsEveryFailure(t *testing.T) {
	cleanup := &certificationCleanup{}
	first := errors.New("first cleanup failure")
	second := errors.New("second cleanup failure")
	cleanup.add(func() error { return first })
	cleanup.add(func() error { return second })
	err := cleanup.close()
	if !errors.Is(err, first) || !errors.Is(err, second) {
		t.Fatalf("cleanup error=%v", err)
	}
}

func TestCertificationPrimaryFailureRetainsClassificationWhenCleanupFails(t *testing.T) {
	cleanup := &certificationCleanup{}
	cleanup.add(func() error { return errors.New("cleanup failed") })
	primary := errors.New(managerdomain.ReasonModelLoadFailed + ": load failed")
	result := errors.Join(primary, cleanup.close())
	if !errors.Is(result, primary) || !strings.Contains(result.Error(), managerdomain.ReasonModelLoadFailed) || !strings.Contains(result.Error(), "cleanup failed") {
		t.Fatalf("joined result=%v", result)
	}
}
