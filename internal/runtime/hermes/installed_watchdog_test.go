package hermes

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Offline native resolver qualification: synthetic agent/payload only. No
// AIAgent, credential loader, network client or live turn is constructed.
func TestInstalledCodexWatchdogResolver(t *testing.T) {
	root := os.Getenv("AEGIS_TEST_HERMES_WATCHDOG_SOURCE")
	if root == "" {
		t.Skip("explicit installed native code root required")
	}
	python := filepath.Join(root, "venv", "bin", "python")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	value, err := remainingCodexEventTimeout(ctx)
	if err != nil {
		t.Fatal(err)
	}
	script := `import sys, os
sys.path.insert(0,sys.argv[1])
from types import SimpleNamespace
from agent.chat_completion_helpers import _resolve_nonstream_watchdogs
agent=SimpleNamespace(api_mode='codex_responses',provider='openai-codex',base_url='https://chatgpt.com/backend-api/codex',_base_url='https://chatgpt.com/backend-api/codex',_compute_non_stream_stale_timeout=lambda payload:300.0)
payload={'input':'x'*23050,'tools':[]}
key='HERMES_CODEX_EVENT_STALE_TIMEOUT_SECONDS'
os.environ.pop(key,None)
baseline=_resolve_nonstream_watchdogs(agent,payload)
assert baseline.idle_enabled and baseline.idle_timeout==12.0, baseline
os.environ[key]=sys.argv[2]
bounded=_resolve_nonstream_watchdogs(agent,payload)
assert bounded.idle_enabled and 0<bounded.idle_timeout<=2.0, bounded
assert bounded.idle_timeout==float(sys.argv[2])
assert bounded.ttfb_timeout==baseline.ttfb_timeout
assert bounded.stale_timeout==baseline.stale_timeout
print('native resolver: default=12s; derived positive <=2s; TTFB/stale unchanged; no API calls')
`
	cmd := exec.CommandContext(ctx, python, "-I", "-B", "-c", script, root, value)
	cmd.Env = minimalEnv(t.TempDir(), nil)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("native resolver: %v %s", err, out)
	}
	t.Log(strings.TrimSpace(string(out)))
}

func TestCodexWatchdogRejectsMissingExpiredAndCancelledDeadline(t *testing.T) {
	expired, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	excessive, end := context.WithTimeout(context.Background(), MaxAttemptDuration+time.Minute)
	defer end()
	for _, ctx := range []context.Context{context.Background(), expired, cancelled, excessive} {
		if value, err := remainingCodexEventTimeout(ctx); err == nil || value != "" {
			t.Fatal("unbounded/expired watchdog accepted")
		}
	}
}
