package command

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestLaunchConsoleOpensSignedOutPasswordGateWithoutCreatingSession(t *testing.T) {
	configPath, origin, stop := consolePasswordGateFixture(t)
	defer stop()
	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(&output)
	var opened string
	err := launchConsole(cmd, &rootOptions{configFile: configPath}, func(_ context.Context, target string) error {
		opened = target
		client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		response, requestErr := client.Get(target) //nolint:gosec // target is the configured loopback console fixture.
		if requestErr != nil {
			return requestErr
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized || len(response.Cookies()) != 0 {
			return fmt.Errorf("signed-out console status=%d cookies=%v", response.StatusCode, response.Cookies())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if opened != origin+"/console" {
		t.Fatalf("browser target=%q want signed-out console %q", opened, origin+"/console")
	}
	text := output.String()
	for _, expected := range []string{`"browser_opened": true`, `"authentication_required": "principal_password"`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("launch output missing %s: %s", expected, text)
		}
	}
	for _, forbidden := range []string{"bootstrap", "expires_at", "browser_session_established", "single_use"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("launch output retained passwordless authentication field %q: %s", forbidden, text)
		}
	}
}

func TestConsoleCommandReportsPasswordGateWithoutRequestingBootstrap(t *testing.T) {
	configPath, origin, stop := consolePasswordGateFixture(t)
	defer stop()
	var output bytes.Buffer
	cmd := consoleCmd(&rootOptions{configFile: configPath})
	cmd.SetContext(context.Background())
	cmd.SetOut(&output)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, expected := range []string{`"login_url": "` + origin + `/console"`, `"authentication_required": "principal_password"`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("console command output missing %s: %s", expected, text)
		}
	}
	for _, forbidden := range []string{"bootstrap", "expires_at", "browser_session_established", "single_use"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("console command retained passwordless field %q: %s", forbidden, text)
		}
	}
}

func TestLaunchConsoleReportsManualPasswordGateWhenBrowserCannotOpen(t *testing.T) {
	configPath, origin, stop := consolePasswordGateFixture(t)
	defer stop()
	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(&output)
	err := launchConsole(cmd, &rootOptions{configFile: configPath}, func(context.Context, string) error {
		return fmt.Errorf("synthetic opener failure")
	})
	if err == nil || !strings.Contains(err.Error(), "browser launch failed") {
		t.Fatalf("browser failure was not actionable: %v", err)
	}
	text := output.String()
	for _, expected := range []string{`"browser_opened": false`, `"manual_url": "` + origin + `/console"`, `"authentication_required": "principal_password"`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("manual password-gate output missing %s: %s", expected, text)
		}
	}
	for _, forbidden := range []string{"bootstrap", "expires_at", "single_use"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("manual password-gate output retained passwordless field %q: %s", forbidden, text)
		}
	}
}

func consolePasswordGateFixture(t *testing.T) (string, string, func()) {
	t.Helper()
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	tokenPath := filepath.Join(root, "api.token")
	if err = os.WriteFile(tokenPath, []byte(strings.Repeat("b", 64)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	consoleListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	origin := "http://" + consoleListener.Addr().String()
	configPath := filepath.Join(root, "aegis.yaml")
	document := fmt.Sprintf("state_dir: %q\nprincipal:\n  id: principal\n  name: Local operator\n  uid: %q\n  user: %q\n  auth_ttl: 5m\napi:\n  token_file: %q\n  unix_socket: %q\n  console:\n    origin: %s\naudit:\n  checkpoint_dir: %q\n", filepath.Join(root, "state"), current.Uid, current.Username, tokenPath, filepath.Join(root, "a.sock"), origin, filepath.Join(root, "checkpoints"))
	if err = os.WriteFile(configPath, []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	consoleServer := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/console" {
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		writer.WriteHeader(http.StatusUnauthorized)
	})}
	go func() { _ = consoleServer.Serve(consoleListener) }()
	return configPath, origin, func() {
		_ = consoleServer.Close()
		_ = consoleListener.Close()
	}
}
