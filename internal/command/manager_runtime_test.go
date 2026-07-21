package command

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	managerdomain "github.com/berryhill/aegis/internal/manager"
)

func TestConversationCleanupAttemptsEveryStageAggregatesAndFinalizesOnce(t *testing.T) {
	const stages = 6
	for failed := 0; failed <= stages; failed++ {
		t.Run(fmt.Sprintf("failure-%d", failed), func(t *testing.T) {
			calls := make([]atomic.Int32, stages)
			steps := make([]func(context.Context) error, stages)
			for index := range steps {
				index := index
				steps[index] = func(context.Context) error {
					calls[index].Add(1)
					if failed == index {
						return fmt.Errorf("stage-%d-failed", index)
					}
					return nil
				}
			}
			var receipts atomic.Int32
			var cleanupStatus string
			runtime := &conversationalRuntime{testCleanup: steps, testFinalize: func(_ context.Context, reason, cleanup string) error {
				receipts.Add(1)
				cleanupStatus = cleanup
				if reason != managerdomain.EndInterrupt {
					t.Errorf("reason=%s", reason)
				}
				if failed == stages {
					return errors.New("audit-write-failed")
				}
				return nil
			}}
			start := make(chan struct{})
			errorsFound := make(chan error, 8)
			var wait sync.WaitGroup
			for range 8 {
				wait.Add(1)
				go func() {
					defer wait.Done()
					<-start
					errorsFound <- runtime.Close(context.Background(), managerdomain.EndInterrupt)
				}()
			}
			close(start)
			wait.Wait()
			close(errorsFound)
			for closeErr := range errorsFound {
				if closeErr == nil {
					t.Fatal("injected cleanup failure was lost")
				}
				expected := "audit-write-failed"
				if failed < stages {
					expected = fmt.Sprintf("stage-%d-failed", failed)
				}
				if !strings.Contains(closeErr.Error(), expected) {
					t.Fatalf("cleanup error=%v", closeErr)
				}
			}
			for index := range calls {
				if calls[index].Load() != 1 {
					t.Fatalf("stage %d calls=%d", index, calls[index].Load())
				}
			}
			if receipts.Load() != 1 {
				t.Fatalf("receipt calls=%d", receipts.Load())
			}
			if failed < stages && cleanupStatus != "incomplete" {
				t.Fatalf("cleanup status=%s", cleanupStatus)
			}
		})
	}
}

func TestConversationCleanupHonorsDeadlineAndContinues(t *testing.T) {
	var following atomic.Int32
	runtime := &conversationalRuntime{testCleanup: []func(context.Context) error{
		func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() },
		func(context.Context) error { following.Add(1); return nil },
	}, testFinalize: func(context.Context, string, string) error { return nil }}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := runtime.Close(ctx, managerdomain.EndRuntimeFailed)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cleanup error=%v", err)
	}
	if time.Since(started) > time.Second || following.Load() != 1 {
		t.Fatalf("bounded cleanup elapsed=%s following=%d", time.Since(started), following.Load())
	}
}

func TestCleanupFailuresExposeOnlyStableStageNames(t *testing.T) {
	runtime := &conversationalRuntime{}
	err := runtime.cleanupStep("unloading and verifying exact model removal", func() error {
		return errors.New("untrusted backend detail")
	})
	if err == nil {
		t.Fatal("cleanup failure was lost")
	}
	failures := runtime.cleanupFailures()
	if len(failures) != 1 || failures[0] != "unloading and verifying exact model removal" {
		t.Fatalf("cleanup failures=%q", failures)
	}
}
