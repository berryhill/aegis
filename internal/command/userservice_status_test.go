package command

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGatewayStatusExposesReadinessSeparatelyFromActivity(t *testing.T) {
	configPath, statePath := validConfigWithoutOperationalAuthority(t)
	tokenPath := filepath.Join(statePath, "status.token")
	if err := os.MkdirAll(statePath, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokenPath, []byte(strings.Repeat("a", 64)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	document, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	document = append(document, []byte(fmt.Sprintf("api:\n  token_file: %q\n  unix_socket: %q\n", tokenPath, filepath.Join(statePath, "a.sock")))...)
	if err = os.WriteFile(configPath, document, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	bin := t.TempDir()
	if err = os.WriteFile(filepath.Join(bin, "loginctl"), []byte("#!/bin/sh\nprintf 'no\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	runner := &denyingUserServiceRunner{}
	cmd := userServiceCmd(runner, func(io.Reader, io.Writer) bool { return false }, &rootOptions{configFile: configPath})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"status"})
	if err = cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var status map[string]any
	if err = json.Unmarshal(out.Bytes(), &status); err != nil {
		t.Fatalf("output %s: %v", out.String(), err)
	}
	if status["ready"] != false || status["readiness_state"] != "not_installed" || status["readiness_reason"] != "gateway_not_installed" || status["active_error"] != true {
		t.Fatalf("status: %+v", status)
	}
	if len(runner.calls) != 1 || runner.calls[0][0] != "is-active" {
		t.Fatalf("unexpected service calls: %v", runner.calls)
	}
}
