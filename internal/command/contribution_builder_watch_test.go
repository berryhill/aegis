package command

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestContributionBuilderWatchDelayedReadiness(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); waitContributionBuilder(ctx, root, cancel) }()
	// Baseline work has no builder marker. It must not disconnect prematurely.
	select {
	case <-done:
		t.Fatal("watcher stopped before builder readiness")
	case <-time.After(100 * time.Millisecond):
	}
	if err := os.WriteFile(filepath.Join(root, "builder-ready"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watcher failed to observe delayed builder")
	}
	if ctx.Err() != context.Canceled {
		t.Fatal("request was not disconnected")
	}
	if _, err := os.Stat(filepath.Join(root, "builder-release")); err != nil {
		t.Fatal("builder was not released", err)
	}
}

func TestContributionBuilderWatchCancellationJoins(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		waitContributionBuilder(ctx, root, func() { t.Error("missing marker disconnected request") })
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watcher outlived canceled request")
	}
	if _, err := os.Stat(filepath.Join(root, "builder-release")); !os.IsNotExist(err) {
		t.Fatal("cancellation released nonexistent builder", err)
	}
}
