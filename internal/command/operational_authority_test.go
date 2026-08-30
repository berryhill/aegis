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

	authoritybadger "github.com/berryhill/aegis/internal/persistence/authority/badger"
)

func validConfigWithoutOperationalAuthority(t *testing.T) (string, string) {
	t.Helper()
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	statePath := filepath.Join(root, "state")
	configPath := filepath.Join(root, "aegis.yaml")
	data := fmt.Sprintf("state_dir: %q\nprincipal:\n  id: principal\n  name: Principal\n  uid: %q\n  user: %q\n  auth_ttl: 5m\naudit:\n  checkpoint_dir: %q\n", statePath, current.Uid, current.Username, filepath.Join(statePath, "audit-checkpoints"))
	if err = os.WriteFile(configPath, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return configPath, statePath
}

func TestOrdinaryCommandDeniesMissingOperationalAuthorityWithoutMutation(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		t.Run(fmt.Sprintf("terminal_%t", terminal), func(t *testing.T) {
			configPath, statePath := validConfigWithoutOperationalAuthority(t)
			root := NewRoot(Dependencies{In: strings.NewReader("yes\n"), Out: io.Discard, Err: io.Discard, IsTerminal: func(io.Reader, io.Writer) bool { return terminal }})
			root.SetArgs([]string{"--config", configPath, "agents", "list"})
			err := root.Execute()
			if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), reasonOperationalAuthorityNotInitialized) {
				t.Fatalf("error=%v exit=%d", err, ExitCode(err))
			}
			if _, statErr := os.Lstat(filepath.Join(statePath, "persistence", "authority-v1")); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("ordinary command mutated authority state: %v", statErr)
			}
		})
	}
}

func TestNonInteractiveAuthorityEntryPointsDenyMissingOperationalAuthorityWithoutMutation(t *testing.T) {
	for _, args := range [][]string{{}, {"init"}, {"manager"}} {
		name := "bare"
		if len(args) != 0 {
			name = args[0]
		}
		t.Run(name, func(t *testing.T) {
			configPath, statePath := validConfigWithoutOperationalAuthority(t)
			root := NewRoot(Dependencies{In: strings.NewReader("yes\n"), Out: io.Discard, Err: io.Discard, IsTerminal: func(io.Reader, io.Writer) bool { return false }})
			root.SetArgs(append([]string{"--config", configPath}, args...))
			err := root.Execute()
			if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), reasonOperationalAuthorityNotInitialized) {
				t.Fatalf("error=%v exit=%d", err, ExitCode(err))
			}
			if _, statErr := os.Lstat(filepath.Join(statePath, "persistence", "authority-v1")); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("noninteractive entry point mutated authority state: %v", statErr)
			}
		})
	}
}

func TestInteractiveBareStartupReconcilesMissingOperationalAuthority(t *testing.T) {
	configPath, statePath := validConfigWithoutOperationalAuthority(t)
	var out bytes.Buffer
	root := NewRoot(Dependencies{In: strings.NewReader("yes\n"), Out: &out, Err: io.Discard, IsTerminal: func(io.Reader, io.Writer) bool { return true }})
	root.SetArgs([]string{"--config", configPath})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"DECISION / Initialize empty operational authority generation", "Setup progress  1/5 verified", "Choose credential authority custody"} {
		if !strings.Contains(out.String(), expected) {
			t.Fatalf("bare startup did not continue canonical onboarding after reconciliation %q: %s", expected, out.String())
		}
	}
	for _, forbidden := range []string{"Bind exact local model", "Run end-to-end certification", "AEGIS / manager", "Aegis gateway installation preview"} {
		if strings.Contains(out.String(), forbidden) {
			t.Fatalf("bare startup advanced after credential-authority onboarding was declined %q: %s", forbidden, out.String())
		}
	}
	inspection := authoritybadger.Inspect(context.Background(), filepath.Join(statePath, "persistence", "authority-v1"))
	if inspection.State != authoritybadger.StateReady {
		t.Fatalf("inspection=%+v", inspection)
	}
}

func TestInteractiveInitReconcilesExactAbsenceOnceAndAgentsListReturnsJSON(t *testing.T) {
	configPath, statePath := validConfigWithoutOperationalAuthority(t)
	authorityPath := filepath.Join(statePath, "persistence", "authority-v1")
	var out bytes.Buffer
	root := NewRoot(Dependencies{In: strings.NewReader("yes\n4\n"), Out: &out, Err: io.Discard, IsTerminal: func(io.Reader, io.Writer) bool { return true }})
	root.SetArgs([]string{"--config", configPath, "init"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "DECISION / Initialize empty operational authority generation") {
		t.Fatalf("missing explicit reconciliation confirmation: %s", out.String())
	}
	first := authoritybadger.Inspect(context.Background(), authorityPath)
	if first.State != authoritybadger.StateReady {
		t.Fatalf("inspection=%+v", first)
	}

	var listOut bytes.Buffer
	listRoot := NewRoot(Dependencies{In: strings.NewReader(""), Out: &listOut, Err: io.Discard, IsTerminal: func(io.Reader, io.Writer) bool { return false }})
	listRoot.SetArgs([]string{"--config", configPath, "agents", "list"})
	if err := listRoot.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(listOut.String()) != "[]" {
		t.Fatalf("agents list=%q", listOut.String())
	}
	second := authoritybadger.Inspect(context.Background(), authorityPath)
	if second.State != authoritybadger.StateReady || second.Generation.GenerationID != first.Generation.GenerationID {
		t.Fatalf("generation identity changed: first=%+v second=%+v", first, second)
	}
}

func TestInteractiveInitRefusesExistingInvalidOperationalAuthorityWithoutReplacement(t *testing.T) {
	configPath, statePath := validConfigWithoutOperationalAuthority(t)
	authorityPath := filepath.Join(statePath, "persistence", "authority-v1")
	if err := os.MkdirAll(authorityPath, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(authorityPath, "legacy.db")
	if err := os.WriteFile(marker, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	root := NewRoot(Dependencies{In: strings.NewReader("yes\n"), Out: io.Discard, Err: io.Discard, IsTerminal: func(io.Reader, io.Writer) bool { return true }})
	root.SetArgs([]string{"--config", configPath, "init"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "existing invalid operational authority") {
		t.Fatalf("error=%v", err)
	}
	data, readErr := os.ReadFile(marker)
	if readErr != nil || string(data) != "preserve" {
		t.Fatalf("existing state replaced: %q %v", data, readErr)
	}
}
