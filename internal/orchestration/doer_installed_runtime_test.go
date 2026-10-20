//go:build linux

package orchestration

import (
	"context"
	hermesruntime "github.com/berryhill/aegis/internal/runtime/hermes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInstalledLocalLayaAvailability(t *testing.T) {
	if os.Getenv("AEGIS_TEST_INSTALLED_LAYA") != "1" {
		t.Skip("opt-in real installed offline Laya probe")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(cwd, "..", "..", ".candidate")
	base, _ = filepath.Abs(base)
	if err = os.MkdirAll(base, 0700); err != nil {
		t.Fatal(err)
	}
	state, err := os.MkdirTemp(base, "installed-laya-proof-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(state)
	w := &QueueWorker{}
	start := time.Now()
	if err = w.PrepareInstalledDoerRuntime(state, &hermesruntime.Adapter{}); err != nil {
		t.Fatalf("discovery: %v", err)
	}
	if !w.implementation.managedLocalDoer || len(w.implementation.config.AuthorizedContracts) != 0 || w.implementation.config.GoBinary != "" {
		t.Fatal("managed preparation granted native authority")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	gate, err := w.implementation.decision.Gate(ctx, "Task: Write the five UTF-8 bytes hello to result.txt, without a newline.\nExpected result: result.txt exists in the workspace and contains exactly \"hello\"")
	if err != nil {
		t.Fatalf("real fixed-helper gate: %v", err)
	}
	t.Logf("actual installed Laya gate=%+v elapsed=%s", gate, time.Since(start))
	verdict, err := w.implementation.decision.Verdict(ctx, "Write the five UTF-8 bytes hello to result.txt, without a newline.", "Completed result.txt. Independent byte comparison found exactly the five UTF-8 bytes hello, with no newline; no other paths were changed.")
	if err != nil {
		t.Fatalf("real fixed-helper verdict: %v", err)
	}
	t.Logf("actual installed Laya verdict=%+v", verdict)
	// Only the five public assets are present, never ambient HF credentials.
	if _, err := os.Stat(filepath.Join(w.implementation.config.LayaHome, ".cache", "huggingface", "token")); !os.IsNotExist(err) {
		t.Fatal("ambient credential inherited")
	}
}

func TestInstalledLocalLayaDiscoveryMissing(t *testing.T) {
	w := &QueueWorker{}
	if err := w.prepareInstalledDoerRuntime(t.TempDir(), t.TempDir(), &hermesruntime.Adapter{}); err == nil || w.implementation != nil {
		t.Fatal("missing installed dependencies admitted")
	}
}
