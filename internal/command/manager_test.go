package command

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	managerdomain "github.com/berryhill/aegis/internal/manager"
	"github.com/berryhill/aegis/internal/tui"
	selfupdate "github.com/berryhill/aegis/internal/update"
)

func TestManagerMissingCredentialReferenceIsCollectedLocally(t *testing.T) {
	var output bytes.Buffer
	composer := tui.NewComposer(strings.NewReader("not valid\ntest\n"), &output, 255)
	reference, err := readManagerCredentialReference(context.Background(), composer, &output, tui.Capabilities{Profile: tui.PlainInteractive})
	if err != nil {
		t.Fatal(err)
	}
	if reference != "test" {
		t.Fatalf("reference=%q", reference)
	}
	if !strings.Contains(output.String(), "credential reference > ") || !strings.Contains(output.String(), "invalid credential reference") {
		t.Fatalf("output=%q", output.String())
	}
}

func TestManagerMissingCredentialReferenceCanBeCancelled(t *testing.T) {
	var output bytes.Buffer
	composer := tui.NewComposer(strings.NewReader("\n"), &output, 255)
	if _, err := readManagerCredentialReference(context.Background(), composer, &output, tui.Capabilities{Profile: tui.PlainInteractive}); err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("err=%v", err)
	}
}

func TestManagerInputEndReasonTreatsComposerEOFAsNormalExit(t *testing.T) {
	for _, test := range []struct {
		name    string
		eof     bool
		err     error
		want    string
		context func() context.Context
	}{
		{name: "EOF flag", eof: true, want: managerdomain.EndTerminalEOF},
		{name: "EOF error", err: io.EOF, want: managerdomain.EndTerminalEOF},
		{name: "read failure", err: errors.New("terminal failed"), want: managerdomain.EndRuntimeFailed},
		{name: "explicit EOF wins race", err: io.EOF, want: managerdomain.EndTerminalEOF, context: func() context.Context {
			ctx, cancel := context.WithCancelCause(context.Background())
			cancel(managerdomain.ErrInterrupt)
			return ctx
		}},
		{name: "context cancellation", err: context.Canceled, want: managerdomain.EndInterrupt, context: func() context.Context {
			ctx, cancel := context.WithCancelCause(context.Background())
			cancel(managerdomain.ErrInterrupt)
			return ctx
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if test.context != nil {
				ctx = test.context()
			}
			got, ended := managerInputEndReason(ctx, test.eof, test.err)
			if !ended || got != test.want {
				t.Fatalf("reason=%q ended=%t want=%q", got, ended, test.want)
			}
		})
	}
	if reason, ended := managerInputEndReason(context.Background(), false, nil); ended || reason != "" {
		t.Fatalf("ordinary input ended: reason=%q ended=%t", reason, ended)
	}
}

func TestManagerMakeCredentialInputNeverFallsThroughToHermes(t *testing.T) {
	canaryBytes := make([]byte, 16)
	if _, err := rand.Read(canaryBytes); err != nil {
		t.Fatal(err)
	}
	canary := hex.EncodeToString(canaryBytes)
	for _, test := range []struct {
		name  string
		input string
	}{
		{name: "value of", input: "alright, I want to make a new cred named test with a value of " + canary},
		{name: "secret of", input: `I want to make a new cred named "test" with a secret of "` + canary + `"`},
		{name: "missing space before named", input: "I want to store a new secretnamed test with a value of " + canary},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			root := NewRoot(Dependencies{In: strings.NewReader(test.input + "\nexit\n"), Out: &output, Err: io.Discard, Version: "test", IsTerminal: func(io.Reader, io.Writer) bool { return true }})
			root.SetArgs([]string{"--config", managerTestConfig(t), "manager"})
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			text := output.String()
			if strings.Contains(text, canary) {
				t.Fatal("credential value reached retained manager output")
			}
			if !strings.Contains(text, "credential-bearing input requires the active authenticated exact-local-model session; input was not retained") {
				t.Fatalf("credential input did not take deterministic create route: %s", text)
			}
			if strings.Contains(text, "credential-bearing create syntax was not recognized") || strings.Contains(text, "The local Aegis management model is unavailable") {
				t.Fatalf("credential input fell through deterministic create routing: %s", text)
			}
		})
	}
}

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

func isolatedPaths(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state-home"))
	return filepath.Join(home, ".argis", "aegis.yaml"), filepath.Join(root, "state")
}

func TestBareRootNonTTYUninitializedReturnsStructuredAction(t *testing.T) {
	configPath, _ := isolatedPaths(t)
	var out, stderr bytes.Buffer
	root := NewRoot(Dependencies{In: strings.NewReader("ignored"), Out: &out, Err: &stderr, Version: "test", IsTerminal: func(io.Reader, io.Writer) bool { return false }})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "manager_not_initialized") || ExitCode(err) != 2 {
		t.Fatalf("error=%v exit=%d", err, ExitCode(err))
	}
	text := out.String()
	for _, expected := range []string{`"state": "uninitialized"`, `"initialized": false`, `"reason": "manager_not_initialized"`, `"next_command": "aegis init"`, `"exit_status": 2`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("structured output missing %q: %s", expected, text)
		}
	}
	if _, statErr := os.Stat(configPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("non-TTY invocation wrote configuration: %v", statErr)
	}
}

func TestHelpAndVersionDoNotRequireConfiguration(t *testing.T) {
	isolatedPaths(t)
	for _, args := range [][]string{{"--help"}, {"help"}, {"--version"}, {"version"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			var out bytes.Buffer
			root := NewRoot(Dependencies{In: strings.NewReader(""), Out: &out, Err: io.Discard, Version: "test", IsTerminal: func(io.Reader, io.Writer) bool { return false }})
			root.SetArgs(args)
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if out.Len() == 0 {
				t.Fatal("expected command output")
			}
		})
	}
}

func TestVersionCommandMatchesVersionFlag(t *testing.T) {
	isolatedPaths(t)
	var outputs []string
	for _, args := range [][]string{{"--version"}, {"version"}} {
		var out bytes.Buffer
		root := NewRoot(Dependencies{In: strings.NewReader(""), Out: &out, Err: io.Discard, Version: "1.2.3", IsTerminal: func(io.Reader, io.Writer) bool { return false }})
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		outputs = append(outputs, out.String())
	}
	if outputs[0] != outputs[1] || outputs[0] != "aegis version 1.2.3\n" {
		t.Fatalf("version outputs differ: %q", outputs)
	}
}

func TestBareInteractiveFirstRunInitializesThenStartsManager(t *testing.T) {
	configPath, statePath := isolatedPaths(t)
	var out bytes.Buffer
	root := NewRoot(Dependencies{In: strings.NewReader("yes\n/status\n/quit\n"), Out: &out, Err: io.Discard, Version: "test", IsTerminal: func(io.Reader, io.Writer) bool { return true }})
	root.SetArgs([]string{"--state-dir", statePath})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, expected := range []string{"AEGIS / bootstrap", "Aegis first-run initialization", "Configuration path: " + configPath, "State path: " + statePath, "Exact configuration to write:", "Initialization completed atomically", "derived state  principal-configured", "Credential authority custody"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("output missing %q: %s", expected, text)
		}
	}
	assertSecureConfig(t, configPath)
}

func TestExplicitInitDeclineWritesNothing(t *testing.T) {
	configPath, statePath := isolatedPaths(t)
	var out bytes.Buffer
	root := NewRoot(Dependencies{In: strings.NewReader("no\n"), Out: &out, Err: io.Discard, Version: "test", IsTerminal: func(io.Reader, io.Writer) bool { return true }})
	root.SetArgs([]string{"--state-dir", statePath, "init"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "declined; no writes") {
		t.Fatalf("missing decline result: %s", out.String())
	}
	if _, err := os.Stat(configPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("declined setup wrote config: %v", err)
	}
	if _, err := os.Stat(statePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("declined setup wrote state: %v", err)
	}
}

func TestExplicitInitCreatesRestrictiveValidConfiguration(t *testing.T) {
	configPath, statePath := isolatedPaths(t)
	root := NewRoot(Dependencies{In: strings.NewReader("\n"), Out: io.Discard, Err: io.Discard, Version: "test", IsTerminal: func(io.Reader, io.Writer) bool { return true }})
	root.SetArgs([]string{"--state-dir", statePath, "init"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	assertSecureConfig(t, configPath)
}

func TestFirstRunRecoversRecognizedInterruptedTemporary(t *testing.T) {
	configPath, statePath := isolatedPaths(t)
	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		t.Fatal(err)
	}
	partial := filepath.Join(filepath.Dir(configPath), ".aegis.yaml.init-interrupted")
	if err := os.WriteFile(partial, []byte("incomplete"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	root := NewRoot(Dependencies{In: strings.NewReader("yes\n"), Out: &out, Err: io.Discard, Version: "test", IsTerminal: func(io.Reader, io.Writer) bool { return true }})
	root.SetArgs([]string{"--state-dir", statePath, "init"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Recovery: remove 1") {
		t.Fatalf("partial recovery was not disclosed: %s", out.String())
	}
	if _, err := os.Stat(partial); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial was not recovered: %v", err)
	}
	assertSecureConfig(t, configPath)
}

func TestMalformedAndInsecureConfigurationFailClosedWithoutOverwrite(t *testing.T) {
	for _, test := range []struct {
		name string
		mode os.FileMode
		data string
		want string
	}{
		{"malformed", 0600, "principal: [\n", "malformed"},
		{"insecure", 0644, "principal: {}\n", "insecure mode"},
	} {
		t.Run(test.name, func(t *testing.T) {
			configPath, statePath := isolatedPaths(t)
			if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
				t.Fatal(err)
			}
			original := []byte(test.data)
			if err := os.WriteFile(configPath, original, test.mode); err != nil {
				t.Fatal(err)
			}
			root := NewRoot(Dependencies{In: strings.NewReader("yes\n"), Out: io.Discard, Err: io.Discard, Version: "test", IsTerminal: func(io.Reader, io.Writer) bool { return true }})
			root.SetArgs([]string{"--state-dir", statePath, "init"})
			err := root.Execute()
			if err == nil || !strings.Contains(err.Error(), configPath) && !strings.Contains(err.Error(), "repair-required") || !strings.Contains(err.Error(), test.want) && !strings.Contains(err.Error(), "configuration_") {
				t.Fatalf("error=%v", err)
			}
			got, readErr := os.ReadFile(configPath)
			if readErr != nil || !bytes.Equal(got, original) {
				t.Fatalf("existing configuration changed: %q %v", got, readErr)
			}
		})
	}
}

func TestBareInteractiveManagerWithValidConfigOwnsInputAndExits(t *testing.T) {
	var out bytes.Buffer
	root := NewRoot(Dependencies{In: strings.NewReader("/status\n/quit\n"), Out: &out, Err: io.Discard, Version: "test", IsTerminal: func(io.Reader, io.Writer) bool { return true }})
	root.SetArgs([]string{"--config", managerTestConfig(t), "manager"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, expected := range []string{"[AEGIS] Authenticated as principal", "Runtime: Hermes Agent", "Trust stanza / security context: secrets-manager", "No cloud fallback: enabled", "Route: local-only"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("output missing %q: %s", expected, text)
		}
	}
	for _, internalStage := range []string{"validating credential authority", "discovering Hermes Agent", "verifying local Ollama route", "verifying exact model artifact", "validating certification", "opening authenticated inference route", "starting disposable Hermes runtime"} {
		if strings.Contains(text, internalStage) {
			t.Fatalf("successful internal stage leaked into default UX: %q", internalStage)
		}
	}
}

type fakeUpdater struct {
	calls  []bool
	result selfupdate.Result
	err    error
}

func (f *fakeUpdater) Run(_ context.Context, check bool) (selfupdate.Result, error) {
	f.calls = append(f.calls, check)
	return f.result, f.err
}

func TestUpdateAliasAndSubcommandUseSameInjectedServiceAndOutput(t *testing.T) {
	isolatedPaths(t)
	result := selfupdate.Result{CurrentVersion: "1.0.0", LatestVersion: "1.1.0", UpdateAvailable: true, Updated: true, Executable: "/isolated/aegis"}
	updater := &fakeUpdater{result: result}
	var outputs []string
	for _, args := range [][]string{{"--update"}, {"update"}} {
		var out bytes.Buffer
		root := NewRoot(Dependencies{In: strings.NewReader(""), Out: &out, Err: io.Discard, Version: "test", Updater: updater, IsTerminal: func(io.Reader, io.Writer) bool { return false }})
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		outputs = append(outputs, out.String())
	}
	if len(updater.calls) != 2 || updater.calls[0] || updater.calls[1] || outputs[0] != outputs[1] {
		t.Fatalf("calls=%v outputs=%q", updater.calls, outputs)
	}
}

func TestUpdateAliasAndSubcommandHaveEquivalentErrors(t *testing.T) {
	isolatedPaths(t)
	want := errors.New("injected updater failure")
	updater := &fakeUpdater{err: want}
	for _, args := range [][]string{{"--update"}, {"update"}} {
		root := NewRoot(Dependencies{In: strings.NewReader(""), Out: io.Discard, Err: io.Discard, Version: "test", Updater: updater, IsTerminal: func(io.Reader, io.Writer) bool { return false }})
		root.SetArgs(args)
		if err := root.Execute(); !errors.Is(err, want) || ExitCode(err) != 1 {
			t.Fatalf("args=%v error=%v exit=%d", args, err, ExitCode(err))
		}
	}
}

func TestUpdateAliasRejectsAmbiguousCombinations(t *testing.T) {
	isolatedPaths(t)
	for _, args := range [][]string{{"--update", "session", "start", "id"}, {"--update", "update"}, {"--update", "--version"}, {"--update", "--help"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			updater := &fakeUpdater{}
			root := NewRoot(Dependencies{In: strings.NewReader(""), Out: io.Discard, Err: io.Discard, Version: "test", Updater: updater, IsTerminal: func(io.Reader, io.Writer) bool { return false }})
			root.SetArgs(args)
			err := root.Execute()
			if err == nil || ExitCode(err) != 2 || len(updater.calls) != 0 {
				t.Fatalf("args=%v error=%v exit=%d calls=%v", args, err, ExitCode(err), updater.calls)
			}
		})
	}
}

func TestUnrelatedUnknownFlagRetainsExistingExitCode(t *testing.T) {
	isolatedPaths(t)
	root := NewRoot(Dependencies{In: strings.NewReader(""), Out: io.Discard, Err: io.Discard, Version: "test", IsTerminal: func(io.Reader, io.Writer) bool { return false }})
	root.SetArgs([]string{"--does-not-exist"})
	err := root.Execute()
	if err == nil || ExitCode(err) != 1 {
		t.Fatalf("error=%v exit=%d", err, ExitCode(err))
	}
}

func assertSecureConfig(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("config mode=%04o", info.Mode().Perm())
	}
	rootInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if rootInfo.Mode().Perm() != 0700 {
		t.Fatalf("canonical root mode=%04o", rootInfo.Mode().Perm())
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`id: "principal"`, `uid: "` + current.Uid + `"`, `user: "` + current.Username + `"`} {
		if !strings.Contains(string(contents), expected) {
			t.Fatalf("configuration missing %q: %s", expected, contents)
		}
	}
}
