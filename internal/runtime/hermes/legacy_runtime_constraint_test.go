package hermes

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionLaunchRejectsRuntimeUpgradeAfterMandate(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "hermes-test")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\nprintf 'Hermes Agent v0.21.3\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	adapter := New(exe, slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := validAttemptTurnRequest(root)
	_, _, pid, _, err := adapter.Launch(context.Background(), root, request.Launch.Mandate, request.Launch.AuthorityContext, nil, BrokerBridge{})
	if err == nil || !strings.Contains(err.Error(), "runtime binding does not match authority context") || pid != 0 {
		t.Fatalf("upgraded runtime must not inherit prior mandate: pid=%d err=%v", pid, err)
	}
}
