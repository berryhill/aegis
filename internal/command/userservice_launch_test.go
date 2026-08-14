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

func TestLaunchConsoleObtainsSingleUseBootstrapThenOpensConfiguredOrigin(t *testing.T) {
	configPath, stop := consoleBootstrapFixture(t, http.StatusCreated, `{"bootstrap":"single-use-test-bootstrap","expires_at":"2030-01-01T00:00:00Z"}`)
	defer stop()
	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(&output)
	var opened string
	err := launchConsole(cmd, &rootOptions{configFile: configPath}, func(_ context.Context, target string) error {
		opened = target
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if opened != "http://127.0.0.1:18443/console" {
		t.Fatalf("opened target = %q", opened)
	}
	text := output.String()
	for _, expected := range []string{`"browser_opened": true`, `"single_use": true`, `"reusable_bearer_exposed": false`, `"bootstrap": "single-use-test-bootstrap"`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("launch output missing %s: %s", expected, text)
		}
	}
}

func TestLaunchConsoleReturnsManualHandoffWhenBrowserCannotOpen(t *testing.T) {
	configPath, stop := consoleBootstrapFixture(t, http.StatusCreated, `{"bootstrap":"single-use-test-bootstrap","expires_at":"2030-01-01T00:00:00Z"}`)
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
	for _, expected := range []string{`"browser_opened": false`, `"manual_url": "http://127.0.0.1:18443/console"`, `"single_use": true`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("manual handoff output missing %s: %s", expected, text)
		}
	}
}

func TestLaunchConsoleDoesNotOpenBrowserWhenBootstrapIsDenied(t *testing.T) {
	configPath, stop := consoleBootstrapFixture(t, http.StatusForbidden, `{"error":"denied"}`)
	defer stop()
	called := false
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(&bytes.Buffer{})
	err := launchConsole(cmd, &rootOptions{configFile: configPath}, func(context.Context, string) error {
		called = true
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("bootstrap denial was not preserved: %v", err)
	}
	if called {
		t.Fatal("browser opened before authority admitted a bootstrap")
	}
}

func consoleBootstrapFixture(t *testing.T, status int, body string) (string, func()) {
	t.Helper()
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	socketRoot, err := os.MkdirTemp("", "aegis-console-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketRoot) })
	socket := filepath.Join(socketRoot, "a.sock")
	tokenValue := strings.Repeat("b", 64)
	tokenPath := filepath.Join(root, "api.token")
	if err = os.WriteFile(tokenPath, []byte(tokenValue+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "aegis.yaml")
	document := fmt.Sprintf("state_dir: %q\nprincipal:\n  id: principal\n  name: Local operator\n  uid: %q\n  user: %q\n  auth_ttl: 5m\napi:\n  token_file: %q\n  unix_socket: %q\n  console:\n    origin: http://127.0.0.1:18443\naudit:\n  checkpoint_dir: %q\n", filepath.Join(root, "state"), current.Uid, current.Username, tokenPath, socket, filepath.Join(root, "checkpoints"))
	if err = os.WriteFile(configPath, []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/console/bootstrap" || request.Header.Get("Authorization") != "Bearer "+tokenValue {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(status)
		_, _ = writer.Write([]byte(body))
	})}
	go func() { _ = server.Serve(listener) }()
	return configPath, func() {
		_ = server.Close()
		_ = listener.Close()
	}
}
