package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/config"
	resetdomain "github.com/berryhill/aegis/internal/reset"
)

type resetCommandFixture struct {
	home, config, state string
	service             *resetdomain.Service
}

func newResetCommandFixture(t *testing.T, initialized bool) resetCommandFixture {
	t.Helper()
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(t.TempDir(), "home")
	if err = os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(home, ".config", "aegis", "aegis.yaml")
	state := filepath.Join(home, "state")
	copyUser := *current
	service := &resetdomain.Service{
		Current: func() (*user.User, error) { value := copyUser; return &value, nil },
		LookupID: func(uid string) (*user.User, error) {
			value := copyUser
			if uid != value.Uid {
				return nil, errors.New("unknown uid")
			}
			return &value, nil
		},
		HomeDir: func() (string, error) { return home, nil },
	}
	if initialized {
		if err = os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
			t.Fatal(err)
		}
		document := fmt.Sprintf("state_dir: %q\nprincipal:\n  id: principal\n  name: Principal\n  uid: %q\n  user: %q\n  auth_ttl: 5m\naudit:\n  checkpoint_dir: %q\n", state, current.Uid, current.Username, filepath.Join(state, "audit-checkpoints"))
		if err = os.WriteFile(configPath, []byte(document), 0600); err != nil {
			t.Fatal(err)
		}
		if err = os.MkdirAll(filepath.Join(state, "plans"), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(state, "plans", "one.json"), []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return resetCommandFixture{home: home, config: configPath, state: state, service: service}
}

func executeReset(t *testing.T, fixture resetCommandFixture, input string, terminal bool, ctx context.Context) (string, error) {
	t.Helper()
	var out bytes.Buffer
	root := NewRoot(Dependencies{In: strings.NewReader(input), Out: &out, Err: io.Discard, Version: "test", IsTerminal: func(io.Reader, io.Writer) bool { return terminal }, Resetter: fixture.service})
	root.SetArgs([]string{"--config", fixture.config, "reset"})
	if ctx != nil {
		root.SetContext(ctx)
	}
	err := root.Execute()
	return out.String(), err
}

func TestResetCommandPreviewAndYesConfirmation(t *testing.T) {
	fixture := newResetCommandFixture(t, true)
	output, err := executeReset(t, fixture, "y\n", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`"operation": "reset"`,
		`"resolved_config_path": "` + fixture.config + `"`,
		`"path": "` + filepath.Join(fixture.state, "plans", "one.json") + `"`,
		`"confirmation_required": "y/yes"`,
		"Apply this exact reset plan? [y/N]",
		`"credential_records_destroyed": false`,
		`"local_kek_destroyed": false`,
		"encrypted credentials and audit history",
		"Hermes installation and normal Hermes profiles",
		"Ollama installation, operator-managed daemon, and downloaded model stores",
		`"state": "uninitialized"`,
		`"next_command": "aegis"`,
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("preview missing %q:\n%s", expected, output)
		}
	}
	if config.Inspect(fixture.config).State != config.StateAbsent {
		t.Fatal("reset did not produce absent config")
	}
}

func TestResetCommandDeclineEOFNonTTYAndCancellationWriteNothing(t *testing.T) {
	for _, test := range []struct {
		name, input string
		terminal    bool
		cancel      bool
		wantError   bool
		wantReason  string
	}{
		{name: "default no", input: "\n", terminal: true},
		{name: "explicit no", input: "no\n", terminal: true},
		{name: "old phrase", input: "reset aegis\n", terminal: true},
		{name: "eof", input: "", terminal: true},
		{name: "non tty", input: resetdomain.Confirmation + "\n", terminal: false, wantError: true, wantReason: resetdomain.ReasonRequiresTTY},
		{name: "cancellation", input: resetdomain.Confirmation + "\n", terminal: true, cancel: true, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newResetCommandFixture(t, true)
			ctx := context.Background()
			if test.cancel {
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			}
			output, err := executeReset(t, fixture, test.input, test.terminal, ctx)
			if test.wantError != (err != nil) {
				t.Fatalf("error=%v output=%s", err, output)
			}
			if test.wantReason != "" && !strings.Contains(err.Error(), test.wantReason) {
				t.Fatalf("error=%v does not contain %s", err, test.wantReason)
			}
			if _, statErr := os.Stat(fixture.config); statErr != nil {
				t.Fatalf("config changed: %v", statErr)
			}
			if _, statErr := os.Stat(filepath.Join(fixture.state, "plans", "one.json")); statErr != nil {
				t.Fatalf("state changed: %v", statErr)
			}
		})
	}
}

func TestResetCommandDetectsChangeBetweenPreviewAndApply(t *testing.T) {
	fixture := newResetCommandFixture(t, true)
	fixture.service.BeforeApply = func(resetdomain.Plan) {
		path := filepath.Join(fixture.state, "plans", "one.json")
		_ = os.Remove(path)
		_ = os.WriteFile(path, []byte("changed"), 0600)
	}
	_, err := executeReset(t, fixture, resetdomain.Confirmation+"\n", true, nil)
	if err == nil || !strings.Contains(err.Error(), resetdomain.ReasonChanged) {
		t.Fatalf("error=%v", err)
	}
	if _, statErr := os.Stat(fixture.config); statErr != nil {
		t.Fatalf("config changed: %v", statErr)
	}
}

func TestResetThenBareOnboardingAndNonTTYBoundary(t *testing.T) {
	fixture := newResetCommandFixture(t, true)
	if _, err := executeReset(t, fixture, resetdomain.Confirmation+"\n", true, nil); err != nil {
		t.Fatal(err)
	}

	var interactive bytes.Buffer
	root := NewRoot(Dependencies{In: strings.NewReader("yes\n/status\n/quit\n"), Out: &interactive, Err: io.Discard, Version: "test", IsTerminal: func(io.Reader, io.Writer) bool { return true }})
	root.SetArgs([]string{"--config", fixture.config, "--state-dir", fixture.state})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(interactive.String(), "Aegis first-run initialization") {
		t.Fatalf("onboarding not replayed: %s", interactive.String())
	}
	if config.Inspect(fixture.config).State != config.StateValid {
		t.Fatal("onboarding did not recreate valid configuration")
	}

	plan, err := fixture.service.Plan(context.Background(), fixture.config)
	if err != nil {
		t.Fatal(err)
	}
	if err = fixture.service.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	nonTTY := NewRoot(Dependencies{In: strings.NewReader("ignored"), Out: &output, Err: io.Discard, Version: "test", IsTerminal: func(io.Reader, io.Writer) bool { return false }})
	nonTTY.SetArgs([]string{"--config", fixture.config})
	err = nonTTY.Execute()
	if err == nil || !strings.Contains(err.Error(), "manager_not_initialized") || !strings.Contains(output.String(), `"state": "uninitialized"`) {
		t.Fatalf("error=%v output=%s", err, output.String())
	}
}
