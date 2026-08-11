package userservice

import (
	"context"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type recordingRunner struct {
	calls [][]string
	err   error
}

func (r *recordingRunner) Run(_ context.Context, args ...string) error {
	r.calls = append(r.calls, append([]string(nil), args...))
	return r.err
}

func (r *recordingRunner) Output(context.Context, ...string) ([]byte, error) {
	return []byte("inactive\n"), nil
}

func serviceFixture(t *testing.T) (string, string) {
	t.Helper()
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	state := filepath.Join(root, "state")
	token := filepath.Join(state, "transport", "api.token")
	if err = os.MkdirAll(filepath.Dir(token), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(token, []byte(strings.Repeat("a", 64)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "aegis.yaml")
	document := "state_dir: \"" + state + "\"\nprincipal:\n  id: principal\n  name: Local operator\n  uid: \"" + current.Uid + "\"\n  user: \"" + current.Username + "\"\n  auth_ttl: 5m\napi:\n  token_file: \"" + token + "\"\n  unix_socket: \"" + filepath.Join(state, "transport", "aegis.sock") + "\"\naudit:\n  checkpoint_dir: \"" + filepath.Join(state, "audit-checkpoints") + "\"\n"
	if err = os.WriteFile(configPath, []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(root, "aegis")
	if err = os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return executable, configPath
}

func TestPreviewIsDeterministicSecretFreeAndRejectsForeignUnit(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	executable, configPath := serviceFixture(t)
	first, err := Preview(executable, configPath)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Preview(executable, configPath)
	if err != nil {
		t.Fatal(err)
	}
	if first.UnitDigest != second.UnitDigest || first.Confirmation != Confirmation || strings.Contains(string(first.unit), strings.Repeat("a", 64)) {
		t.Fatalf("unsafe or nondeterministic preview: %+v", first)
	}
	if !strings.Contains(string(first.unit), " serve --config ") || !strings.Contains(string(first.unit), "NoNewPrivileges=true") || !strings.Contains(string(first.unit), "UMask=0077") {
		t.Fatalf("unit omitted daemon safety controls:\n%s", first.unit)
	}
	if err = os.MkdirAll(filepath.Dir(first.UnitPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(first.UnitPath, []byte("[Service]\nExecStart=/foreign\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Installed(first); !errors.Is(err, ErrForeignUnit) {
		t.Fatalf("foreign unit was accepted: %v", err)
	}
	runner := &recordingRunner{}
	if err = Apply(context.Background(), first, runner, time.Millisecond); !errors.Is(err, ErrForeignUnit) {
		t.Fatalf("apply overwrote foreign unit: %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("systemctl invoked before foreign-unit denial: %v", runner.calls)
	}
}

func TestActionAllowsOnlyBoundedUserServiceLifecycle(t *testing.T) {
	runner := &recordingRunner{}
	for _, action := range []string{"start", "stop", "restart"} {
		if err := Action(context.Background(), runner, action); err != nil {
			t.Fatal(err)
		}
	}
	if err := Action(context.Background(), runner, "enable-linger"); err == nil {
		t.Fatal("unsupported user-service authority was accepted")
	}
	if len(runner.calls) != 3 {
		t.Fatalf("unexpected lifecycle calls: %v", runner.calls)
	}
}
