package hermes

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Opt-in, credential-free installed-runtime probe. Never submits a prompt.
func TestInstalledHermesCompatibility(t *testing.T) {
	executable := os.Getenv("AEGIS_TEST_HERMES_EXECUTABLE")
	root := os.Getenv("AEGIS_TEST_HERMES_PROBE_ROOT")
	if executable == "" || root == "" {
		t.Skip("explicit installed executable and task-owned probe root required")
	}
	if !filepath.IsAbs(executable) || !filepath.IsAbs(root) {
		t.Fatal("absolute probe paths required")
	}
	home, err := os.MkdirTemp(root, "installed-hermes-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(home)
	// Discovery inherits the caller's environment. Remove all ambient credentials,
	// profile selection and provider configuration before invoking it.
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != "PATH" && key != "LANG" {
			t.Setenv(key, "")
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("HERMES_HOME", home)
	t.Setenv("PYTHONDONTWRITEBYTECODE", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	descriptor, err := New(executable, slog.New(slog.NewTextHandler(io.Discard, nil))).Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("discovered Hermes %s", descriptor.Version)
	python := gatewayPython(descriptor)
	if python == "" {
		t.Fatal("gateway Python unavailable")
	}
	cmd := exec.CommandContext(ctx, python, "-m", "tui_gateway.entry")
	cmd.Dir = home
	cmd.Env = append(minimalEnv(home, nil), "HERMES_PYTHON_SRC_ROOT="+descriptor.Installation, "HERMES_TUI_TOOLSETS="+launchToolsets(nil), "HERMES_TUI_SKILLS=", "HERMES_DISABLE_AUTO_SKILLS=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stdin.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	messages := make(chan gatewayMessage, 128)
	failures := make(chan error, 1)
	go readGateway(stdout, messages, failures)
	if _, err = waitGateway(ctx, messages, failures, func(m gatewayMessage) bool { return m.Method == "event" && m.Params.Type == "gateway.ready" }); err != nil {
		t.Fatal(err)
	}
	if err = writeGateway(stdin, "probe-tools", "tools.show", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	response, err := waitGateway(ctx, messages, failures, func(m gatewayMessage) bool { return fmt.Sprint(m.ID) == "probe-tools" })
	if err != nil {
		t.Fatal(err)
	}
	if response.Error != nil {
		t.Fatal("tools.show rejected")
	}
	if total, ok := response.Result["total"].(float64); !ok || total != 0 {
		t.Fatalf("explicit empty tool isolation failed: total=%v", response.Result["total"])
	}
	if err = verifyEmptyGateway(ctx, stdin, messages, failures); err != nil {
		t.Fatal(err)
	}
	if err = probeEmptyGateway(ctx, descriptor, home); err != nil {
		t.Fatalf("interactive launch preflight: %v", err)
	}
	t.Log("gateway.ready and tools.show: zero tools; direct verification and interactive preflight passed; no prompt/provider/model call")
}
