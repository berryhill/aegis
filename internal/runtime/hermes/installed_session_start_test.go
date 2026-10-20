package hermes

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
)

// No provider request: use only synthetic access-only auth, ready and tools.show.
func TestInstalledProviderSessionStart(t *testing.T) {
	executable, root := os.Getenv("AEGIS_TEST_HERMES_EXECUTABLE"), os.Getenv("AEGIS_TEST_HERMES_PROBE_ROOT")
	if executable == "" || root == "" {
		t.Skip("explicit installed executable and private probe root required")
	}
	home, err := os.MkdirTemp(root, "installed-session-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(home)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != "PATH" && key != "LANG" {
			t.Setenv(key, "")
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("HERMES_HOME", home)
	t.Setenv("PYTHONDONTWRITEBYTECODE", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	a := New(executable, slog.New(slog.NewTextHandler(io.Discard, nil)))
	d, err := a.Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = qualifyProviderTransport(d); err != nil {
		t.Fatal(err)
	}
	r := providerAttempt(home)
	m, authority := r.Launch.Mandate, r.Launch.AuthorityContext
	m.Runtime = d
	authority.Runtime = d
	authority.Digest = core.AuthorityContextDigest(authority)
	a.SetProviderAuthenticationResolver(func(_ context.Context, h core.HermesConfig) (ProviderAccessToken, error) {
		return NewProviderAccessToken(h.Provider, syntheticProviderToken(time.Now().Add(time.Hour)), time.Now())
	})
	id, runtimeHome, pid, tools, err := a.Launch(ctx, home, m, authority, nil, BrokerBridge{}, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer a.Terminate(context.Background(), id, true)
	if pid <= 0 || !a.Alive(id) || len(tools) != 0 {
		t.Fatal("runtime not alive or tools differ")
	}
	a.mu.Lock()
	ps := a.processes[id]
	a.mu.Unlock()
	if ps == nil || strings.Join(ps.cmd.Args[1:], " ") != "-m tui_gateway.entry" {
		t.Fatal("not qualified headless gateway")
	}
	if data, e := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); e != nil || len(data) == 0 {
		t.Fatal("process identity unavailable", e)
	}
	config := map[string]string{}
	for _, entry := range ps.cmd.Env {
		key, value, _ := strings.Cut(entry, "=")
		config[key] = value
	}
	for key, want := range map[string]string{"HOME": runtimeHome, "HERMES_HOME": runtimeHome, "HERMES_PYTHON_SRC_ROOT": d.Installation, "HERMES_MODEL": m.Hermes.Model, "HERMES_TUI_MODEL": m.Hermes.Model, "HERMES_TUI_PROVIDER": m.Hermes.Provider, "HERMES_TUI_TOOLSETS": emptyToolset, "HERMES_SAFE_MODE": "1", "HERMES_IGNORE_USER_CONFIG": "1", "HERMES_IGNORE_RULES": "1", "HERMES_DISABLE_AUTO_SKILLS": "1"} {
		if config[key] != want {
			t.Fatal("headless config mismatch", key)
		}
	}
	for _, key := range []string{"OPENAI_API_KEY", "AEGIS_CODEX_ACCESS_TOKEN", "CODEX_HOME", "HERMES_PROFILE", "HTTPS_PROXY"} {
		if config[key] != "" {
			t.Fatal("ambient configuration escaped isolation", key)
		}
	}
	if filepath.Dir(runtimeHome) != filepath.Join(home, "runtime") {
		t.Fatal("not disposable")
	}
	t.Log("actual qualified installed gateway alive; ready and zero-tool snapshot verified; synthetic auth only; no inference")
}
