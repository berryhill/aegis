package api

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A separate test process seeds private durable history and exits, closing both
// stores before the candidate executable opens them. Only runtime/claim history
// is controlled; Graph publication, runtime authority and admission are public
// application methods. This is not a managed-provider qualification.
func TestExecutionDebuggerInstalledSeed(t *testing.T) {
	root := os.Getenv("AEGIS_DEBUGGER_SEED_ROOT")
	if root == "" {
		t.Skip("child-only isolated durable-history preparation")
	}
	svc, subject, _, executor, done := debuggerFixtureKind(t, true)
	close(executor.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	view, err := svc.ExecutionDebuggerAs(context.Background(), subject, "debug-queue", "")
	if err != nil {
		t.Fatal(err)
	}
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	svc.Config.API.Listen = probe.Addr().String()
	probe.Close()
	svc.Config.API.Console.Origin = "http://" + svc.Config.API.Listen
	// Relative socket resolves inside the private process cwd, avoiding the Unix
	// path limit without touching production transport or using a shared socket.
	svc.Config.API.UnixSocket = "aegis.sock"
	// Seed the actual HTTP Console presentation contract, including canonical
	// geometry, rather than comparing its response to the narrower app DTO.
	// The restart probe compares every field except request freshness exactly.
	for name, value := range map[string]any{"owner.json": svc.Config, "expected-debugger.json": debuggerPresentation(view)} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(root, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestExecutionDebuggerInstalledRestart(t *testing.T) {
	candidate := os.Getenv("AEGIS_DEBUGGER_CANDIDATE")
	if candidate == "" {
		t.Skip("set AEGIS_DEBUGGER_CANDIDATE to the frozen executable")
	}
	candidate, err := filepath.Abs(candidate)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(filepath.Join(repo, ".candidate", "issue325"), "installed-")
	if err != nil {
		t.Fatal(err)
	}
	// Retain private evidence for independent replay; it is excluded from source.
	t.Log("private installed proof:", root)
	seed := exec.Command(os.Args[0], "-test.run=^TestExecutionDebuggerInstalledSeed$", "-test.v")
	seed.Env = append(os.Environ(), "AEGIS_DEBUGGER_SEED_ROOT="+root)
	if out, err := seed.CombinedOutput(); err != nil {
		t.Fatalf("seed: %v %s", err, out)
	}
	python := os.Getenv("AEGIS_GRAPH_BROWSER_PYTHON")
	if python == "" {
		python = "python3"
	}
	command := exec.Command(python, filepath.Join(repo, "scripts", "execution_debugger_installed_test.py"), candidate, root)
	command.Env = append(os.Environ(), "AEGIS_CONSOLE_PASSWORD="+principalTestSecret())
	out, err := command.CombinedOutput()
	t.Log(string(out))
	if err != nil {
		t.Fatal(err)
	}
}
