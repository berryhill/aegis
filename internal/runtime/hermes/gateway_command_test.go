package hermes

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
)

// Protocol fixtures, not native-runtime or provider qualification.
func TestModernDirtyVersionMetadataIsNotTransportQualification(t *testing.T) {
	installed, err := ParseVersionOutput([]byte("Hermes Agent v0.23.0+1339.gacdc8f1.dirty (2026.9.24) · upstream acdc8f1b\nInstall directory: /fixture\n"))
	if err != nil || installed.Version != "0.23.0" {
		t.Fatal(installed, err)
	}
	if !errors.Is(qualifyProviderTransport(core.RuntimeDescriptor{Version: installed.Version, Installation: installed.Installation}), ErrProviderAuthRuntimeUnqualified) {
		t.Fatal("metadata opened qualification")
	}
	for _, suffix := range []string{".dirty", "+1.gabcdefg.dirty", "+1.gabcdef1.dirty.more"} {
		if _, err = ParseVersionOutput([]byte("Hermes Agent v0.23.0" + suffix)); err == nil {
			t.Fatal("malformed version accepted")
		}
	}
}

// Protocol fixtures, not native-runtime or provider qualification.
func commandFixture(t *testing.T, mutate func([]string) any) (core.RuntimeDescriptor, string, string) {
	t.Helper()
	root := t.TempDir()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	python, err = filepath.EvalSymlinks(python)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(python, "/tools/") {
		// Protocol-only fake source must not borrow a real PM tool-store owner.
		python, err = filepath.EvalSymlinks("/usr/bin/python3")
		if err != nil {
			t.Fatal(err)
		}
	}
	argv := []string{python, "-I", "-c", gatewayBootstrap(root)}
	var payload any = argv
	if mutate != nil {
		payload = mutate(argv)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	// json string literal is also a Python string for these ASCII-only fixtures.
	literal, _ := json.Marshal(string(encoded))
	script := "import os, sys; assert sys.argv[1:] == ['--print-runtime-command', '--installed-dependencies', '--module', 'tui_gateway.entry']; assert os.environ['HOME'] == os.environ['HERMES_HOME']; assert not any(os.environ.get(k) for k in ['OPENAI_API_KEY', 'HTTP_PROXY', 'HERMES_RUNTIME_DIR', 'PYTHONPATH']); print(" + string(literal) + ")"
	probe, _ := json.Marshal([]string{python, "-I", "-c", installedBootstrap(root, "exec('pass')")})
	probeLiteral, _ := json.Marshal(string(probe))
	script = "import sys; " + "print(" + string(probeLiteral) + ") if sys.argv[1:] == ['--print-runtime-command', '--installed-dependencies', '--code', 'pass', '--'] else exec(" + controllerCodeLiteral(script) + ")"
	launcher := "#!/bin/sh\nexec " + python + " -I -c '" + strings.ReplaceAll(script, "'", "'\"'\"'") + "' \"$@\"\n"
	for name, data := range map[string]string{".hermes/bin/hermes": launcher, "hermes_cli/_launchers.py": "def print_runtime_command(): pass\n", "hermes_bootstrap.py": "# fixture", "hermes_constants.py": "# fixture", "pm/environments.py": "# fixture", "pm/installed_activation.py": "# fixture", "hermes_cli/_early_recovery.py": "# fixture", "hermes_cli/update_lock.py": "# fixture", "pm/__init__.py": "# lazy PM facade fixture", "pm/paths.py": "# install stamp path fixture", "pm/filesystem.py": "# lease lock fixture", "hermes_cli/venv_sync.py": "# launch preparation fixture", "hermes_cli/runtime_state.py": "# generation lease fixture", "pyproject.toml": "# fixture", "setup.py": "# dependency setup fixture", "pm/lock.json": "{}", "uv.lock": "# fixture"} {
		path := filepath.Join(root, name)
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, []byte(data), 0700); err != nil {
			t.Fatal(err)
		}
	}
	return core.RuntimeDescriptor{Installation: root, Executable: filepath.Join(root, ".hermes/bin/hermes")}, t.TempDir(), python
}

func TestModernGatewayCommandCompleteAndFresh(t *testing.T) {
	for _, key := range []string{"OPENAI_API_KEY", "HTTP_PROXY", "HERMES_RUNTIME_DIR", "PYTHONPATH"} {
		t.Setenv(key, "synthetic-ambient-denied")
	}
	d, home, python := commandFixture(t, nil)
	g, err := discoverGatewayCommand(context.Background(), d, home)
	if err != nil {
		t.Fatal(err)
	}
	if !g.modern || !reflect.DeepEqual(g.argv, []string{python, "-I", "-c", gatewayBootstrap(d.Installation)}) {
		t.Fatal("bootstrap argv discarded")
	}
	if err = g.fresh(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) != 0 {
		t.Fatal("query home retained")
	}
	if err = os.WriteFile(filepath.Join(d.Installation, "uv.lock"), []byte("changed"), 0700); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(g.fresh(), errGatewayCommand) {
		t.Fatal("dependency input drift accepted")
	}
}

func TestModernGatewayCommandDeniesUnsupportedPayloads(t *testing.T) {
	for name, mutate := range map[string]func([]string) any{
		"null":       func(a []string) any { return nil },
		"object":     func(a []string) any { return map[string]any{"argv": a} },
		"empty":      func(a []string) any { return []string{} },
		"extra":      func(a []string) any { return append(a, "--provider=other") },
		"shell":      func(a []string) any { a[0] = "/bin/sh"; return a },
		"wrong-root": func(a []string) any { a[3] = gatewayBootstrap("/other"); return a },
		"code":       func(a []string) any { a[3] += "; print('unreviewed')"; return a },
		"module": func(a []string) any {
			a[3] = strings.ReplaceAll(a[3], "tui_gateway.entry", "hermes_cli.main")
			return a
		},
		"bare-python": func(a []string) any { return a[:1] },
		"nul":         func(a []string) any { a[0] += "\x00"; return a },
		"oversized":   func(a []string) any { a[3] = strings.Repeat("x", 17000); return a },
	} {
		t.Run(name, func(t *testing.T) {
			d, home, _ := commandFixture(t, mutate)
			if _, err := discoverGatewayCommand(context.Background(), d, home); !errors.Is(err, errGatewayCommand) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestModernGatewayMissingLauncherNeverUsesLegacyPython(t *testing.T) {
	d, home, _ := commandFixture(t, nil)
	if err := os.Remove(d.Executable); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(d.Installation, "venv/bin"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d.Installation, "venv/bin/python"), []byte("#!/bin/sh\nexit 0"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := discoverGatewayCommand(context.Background(), d, home); !errors.Is(err, errGatewayCommand) {
		t.Fatal("legacy fallback", err)
	}
}

func TestLegacyGatewayCommandPreserved(t *testing.T) {
	a, root := attemptTestAdapter(t, "#!/bin/sh\nexit 0")
	d, err := a.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, oldLauncherSource := range []bool{false, true} {
		if oldLauncherSource {
			if err = os.MkdirAll(filepath.Join(d.Installation, "hermes_cli"), 0700); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(d.Installation, "hermes_cli/_launchers.py"), []byte("# legacy launcher module"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		g, err := discoverGatewayCommand(context.Background(), d, root)
		if err != nil || g.modern || !reflect.DeepEqual(g.argv, []string{gatewayPython(d), "-m", "tui_gateway.entry"}) {
			t.Fatalf("legacy changed: %v", err)
		}
	}
}

func TestGatewayCommandCancellation(t *testing.T) {
	d, home, _ := commandFixture(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if _, err := discoverGatewayCommand(ctx, d, home); err == nil {
		t.Fatal("cancel accepted")
	}
	if time.Since(start) > time.Second {
		t.Fatal("cancel not bounded")
	}
}

func TestDisposableTurnConfigExactLaneAndBounds(t *testing.T) {
	home := t.TempDir()
	if err := writeDisposableTurnConfig(home, "openai-codex", "unchanged-model.1"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var c map[string]any
	if err = json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	model := c["model"].(map[string]any)
	if len(model) != 2 || model["provider"] != "openai-codex" || model["default"] != "unchanged-model.1" {
		t.Fatal("lane mutated or capacity invented")
	}
	agent := c["agent"].(map[string]any)
	if agent["max_turns"] != float64(1) || agent["api_max_retries"] != float64(1) || agent["auto_recovery_cycles"] != float64(0) {
		t.Fatal("unbounded agent")
	}
	info, err := os.Stat(filepath.Join(home, "config.yaml"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("config custody")
	}
	if err = writeDisposableTurnConfig(home, "other", "other"); err == nil {
		t.Fatal("overwrote config")
	}
}
