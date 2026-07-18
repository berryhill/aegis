package command

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

func managerTestConfig(t *testing.T) string {
	t.Helper()
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "aegis.yaml")
	data := fmt.Sprintf("state_dir: %s\nprincipal:\n  id: principal\n  name: Principal\n  uid: %q\n  user: %q\n  auth_ttl: 5m\naudit:\n  checkpoint_dir: %s\n", filepath.Join(dir, "state"), current.Uid, current.Username, filepath.Join(dir, "checkpoints"))
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBareRootRequiresTTYBeforeInitialization(t *testing.T) {
	var out, stderr bytes.Buffer
	root := NewRoot(Dependencies{In: strings.NewReader("ignored"), Out: &out, Err: &stderr, Version: "test", IsTerminal: func(io.Reader, io.Writer) bool { return false }})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "manager_requires_tty") || ExitCode(err) != 2 {
		t.Fatalf("error=%v exit=%d", err, ExitCode(err))
	}
	if out.Len() != 0 {
		t.Fatalf("unexpected stdout %q", out.String())
	}
}

func TestHelpDoesNotInitialize(t *testing.T) {
	var out bytes.Buffer
	root := NewRoot(Dependencies{In: strings.NewReader(""), Out: &out, Err: io.Discard, Version: "test", IsTerminal: func(io.Reader, io.Writer) bool { return false }})
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "manager") {
		t.Fatalf("help missing manager: %q", out.String())
	}
}

func TestBareInteractiveManagerOwnsInputAndExits(t *testing.T) {
	var out bytes.Buffer
	root := NewRoot(Dependencies{In: strings.NewReader("/status\n/quit\n"), Out: &out, Err: io.Discard, Version: "test", IsTerminal: func(io.Reader, io.Writer) bool { return true }})
	root.SetArgs([]string{"--config", managerTestConfig(t)})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, expected := range []string{"Aegis manager", "Runtime: Hermes Agent", "Inference: Ollama local", "Security context: secrets-manager", "Cloud fallback: disabled", "route: local-only"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("output missing %q: %s", expected, text)
		}
	}
}
