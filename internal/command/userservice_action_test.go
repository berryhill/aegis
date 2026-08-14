package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type denyingUserServiceRunner struct {
	calls [][]string
}

func (r *denyingUserServiceRunner) Run(_ context.Context, args ...string) error {
	r.calls = append(r.calls, append([]string(nil), args...))
	return errors.New("raw unit-not-found error")
}

func (r *denyingUserServiceRunner) Output(_ context.Context, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	return nil, errors.New("raw unit-not-found error")
}

func TestServiceLifecycleCommandsReportMissingInstallationBeforeSystemctl(t *testing.T) {
	configPath, statePath := validConfigWithoutOperationalAuthority(t)
	transportDir := filepath.Join(statePath, "transport")
	if err := os.MkdirAll(transportDir, 0700); err != nil {
		t.Fatal(err)
	}
	tokenPath := filepath.Join(transportDir, "api.token")
	if err := os.WriteFile(tokenPath, []byte(strings.Repeat("a", 64)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	configDocument, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	configDocument = append(configDocument, []byte(fmt.Sprintf("api:\n  token_file: %q\n  unix_socket: %q\n", tokenPath, filepath.Join(transportDir, "aegis.sock")))...)
	if err = os.WriteFile(configPath, configDocument, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))

	for _, action := range []string{"start", "stop", "restart"} {
		t.Run(action, func(t *testing.T) {
			runner := &denyingUserServiceRunner{}
			cmd := userServiceCmd(runner, func(io.Reader, io.Writer) bool { return true }, &rootOptions{configFile: configPath})
			cmd.SetIn(strings.NewReader(""))
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs([]string{action})

			err := cmd.Execute()
			if got, want := err.Error(), "service_not_installed: Aegis user service is not installed; run `aegis service install`"; got != want {
				t.Fatalf("error = %q, want %q", got, want)
			}
			if len(runner.calls) != 0 {
				t.Fatalf("systemctl invoked for missing service: %v", runner.calls)
			}
		})
	}
}
