package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	authoritybadger "github.com/berryhill/aegis/internal/persistence/authority/badger"
	"github.com/berryhill/aegis/internal/userservice"
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
			if got, want := err.Error(), "gateway_not_installed: Aegis gateway is not installed; run `aegis gateway install`"; got != want {
				t.Fatalf("error = %q, want %q", got, want)
			}
			if len(runner.calls) != 0 {
				t.Fatalf("systemctl invoked for missing service: %v", runner.calls)
			}
		})
	}
}

func TestGatewayIsCanonicalAndServiceIsOnlyItsCompatibilityAlias(t *testing.T) {
	command := userServiceCmd(&denyingUserServiceRunner{}, func(io.Reader, io.Writer) bool { return true }, &rootOptions{})
	if command.Name() != "gateway" {
		t.Fatalf("canonical command = %q, want gateway", command.Name())
	}
	if len(command.Aliases) != 1 || command.Aliases[0] != "service" {
		t.Fatalf("aliases = %v, want [service]", command.Aliases)
	}
}

func TestBareRootRequiresGatewayForDevelopmentAndProductionProfiles(t *testing.T) {
	for _, test := range []struct {
		profile ExecutionProfile
		want    bool
	}{
		{profile: DevelopmentProfile, want: true},
		{profile: ProductionProfile, want: true},
		{profile: "", want: false},
	} {
		if got := requiresGateway(test.profile); got != test.want {
			t.Fatalf("requiresGateway(%q) = %v, want %v", test.profile, got, test.want)
		}
	}
}

type exactGatewayRunner struct {
	active    bool
	unitPath  string
	execStart string
	runs      [][]string
}

func freshGatewayActivationFixture(t *testing.T) (string, *exactGatewayRunner, *atomic.Int32) {
	t.Helper()
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	socketRoot, err := os.MkdirTemp("", "aegis-fresh-gateway-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketRoot) })
	socketPath := filepath.Join(socketRoot, "a.sock")
	token := strings.Repeat("d", 64)
	tokenPath := filepath.Join(root, "api.token")
	if err = os.WriteFile(tokenPath, []byte(token+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(root, "state")
	configPath := filepath.Join(root, "aegis.yaml")
	configDocument := fmt.Sprintf("state_dir: %q\nprincipal:\n  id: principal\n  name: Local operator\n  uid: %q\n  user: %q\n  auth_ttl: 5m\napi:\n  token_file: %q\n  unix_socket: %q\n  console:\n    origin: http://127.0.0.1:18443\naudit:\n  checkpoint_dir: %q\n", statePath, current.Uid, current.Username, tokenPath, socketPath, filepath.Join(root, "checkpoints"))
	if err = os.WriteFile(configPath, []byte(configDocument), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = authoritybadger.Initialize(context.Background(), filepath.Join(statePath, "persistence", "authority-v1")); err != nil {
		t.Fatal(err)
	}

	unexpectedRequests := &atomic.Int32{}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/readyz" || request.Header.Get("Authorization") != "Bearer "+token {
			unexpectedRequests.Add(1)
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"status":"ready","audit":{"state":"current","current":true,"verifiable":true}}`))
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		_ = server.Close()
		_ = listener.Close()
	})

	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg-config"))
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := userservice.Preview(executable, configPath)
	if err != nil {
		t.Fatal(err)
	}
	return configPath, &exactGatewayRunner{
		unitPath:  plan.UnitPath,
		execStart: fmt.Sprintf("{ path=%s ; argv[]=%s serve --config %s ; ignore_errors=no ; start_time=[n/a] ; stop_time=[n/a] ; pid=0 ; code=(null) ; status=0/0 }", plan.Executable, plan.Executable, plan.ConfigPath),
	}, unexpectedRequests
}

func TestFreshBareGatewayActivationReturnsAfterPresentingConsole(t *testing.T) {
	configPath, runner, unexpectedRequests := freshGatewayActivationFixture(t)
	var output bytes.Buffer
	var opened string
	command := NewRoot(Dependencies{
		In:          strings.NewReader("yes\nthis must not become manager input\n"),
		Out:         &output,
		Err:         io.Discard,
		Version:     "test",
		Profile:     ProductionProfile,
		UserService: runner,
		OpenBrowser: func(_ context.Context, target string) error { opened = target; return nil },
		IsTerminal:  func(io.Reader, io.Writer) bool { return true },
	})
	command.SetArgs([]string{"--config", configPath})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(opened, "/console") || !strings.Contains(output.String(), `"browser_opened": true`) {
		t.Fatalf("opened=%q output=%s", opened, output.String())
	}
	if got := unexpectedRequests.Load(); got != 0 {
		t.Fatalf("fresh activation entered the manager API: unexpected_requests=%d output=%s", got, output.String())
	}
	for _, forbidden := range []string{"AEGIS / manager", "manager_model_absent", "Conversational local inference unavailable"} {
		if strings.Contains(output.String(), forbidden) {
			t.Fatalf("fresh activation entered manager surface %q: %s", forbidden, output.String())
		}
	}
}

func TestFreshBareGatewayActivationReturnsBrowserFailureWithoutManagerFallback(t *testing.T) {
	configPath, runner, unexpectedRequests := freshGatewayActivationFixture(t)
	var output bytes.Buffer
	command := NewRoot(Dependencies{
		In:          strings.NewReader("yes\nthis must not become manager input\n"),
		Out:         &output,
		Err:         io.Discard,
		Version:     "test",
		Profile:     ProductionProfile,
		UserService: runner,
		OpenBrowser: func(context.Context, string) error { return errors.New("synthetic opener failure") },
		IsTerminal:  func(io.Reader, io.Writer) bool { return true },
	})
	command.SetArgs([]string{"--config", configPath})
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "browser launch failed") || !strings.Contains(output.String(), `"manual_url"`) {
		t.Fatalf("err=%v output=%s", err, output.String())
	}
	if got := unexpectedRequests.Load(); got != 0 {
		t.Fatalf("browser failure fell through to manager API: unexpected_requests=%d output=%s", got, output.String())
	}
}

func (r *exactGatewayRunner) Run(_ context.Context, args ...string) error {
	r.runs = append(r.runs, append([]string(nil), args...))
	if len(args) >= 2 && args[0] == "enable" && args[1] == "--now" {
		r.active = true
	}
	return nil
}

func (r *exactGatewayRunner) Output(_ context.Context, args ...string) ([]byte, error) {
	joined := strings.Join(args, " ")
	switch {
	case strings.Contains(joined, "--property ActiveState"):
		if r.active {
			return []byte("active\n"), nil
		}
		return []byte("inactive\n"), nil
	case strings.Contains(joined, "--property UnitFileState"):
		return []byte("disabled\n"), nil
	case strings.Contains(joined, "--property FragmentPath"):
		return []byte(r.unitPath + "\n"), nil
	case strings.Contains(joined, "--property ExecStart"):
		return []byte(r.execStart + "\n"), nil
	default:
		return nil, fmt.Errorf("unexpected systemctl query: %s", joined)
	}
}

func TestSecondBareRunAdmitsHealthyExactGatewayBeforeBootstrapAuthorityInspection(t *testing.T) {
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	socketRoot, err := os.MkdirTemp("", "aegis-second-bare-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketRoot) })
	socketPath := filepath.Join(socketRoot, "a.sock")
	token := strings.Repeat("c", 64)
	tokenPath := filepath.Join(root, "api.token")
	if err = os.WriteFile(tokenPath, []byte(token+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "aegis.yaml")
	configDocument := fmt.Sprintf("state_dir: %q\nprincipal:\n  id: principal\n  name: Local operator\n  uid: %q\n  user: %q\n  auth_ttl: 5m\napi:\n  token_file: %q\n  unix_socket: %q\n  console:\n    origin: http://127.0.0.1:18443\naudit:\n  checkpoint_dir: %q\n", filepath.Join(root, "state"), current.Uid, current.Username, tokenPath, socketPath, filepath.Join(root, "checkpoints"))
	if err = os.WriteFile(configPath, []byte(configDocument), 0600); err != nil {
		t.Fatal(err)
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/readyz" || request.Header.Get("Authorization") != "Bearer "+token {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"status":"ready","audit":{"state":"current","current":true,"verifiable":true}}`))
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		_ = server.Close()
		_ = listener.Close()
	})

	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg-config"))
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := userservice.Preview(executable, configPath)
	if err != nil {
		t.Fatal(err)
	}
	runner := &exactGatewayRunner{
		unitPath:  plan.UnitPath,
		execStart: fmt.Sprintf("{ path=%s ; argv[]=%s serve --config %s ; ignore_errors=no ; start_time=[n/a] ; stop_time=[n/a] ; pid=0 ; code=(null) ; status=0/0 }", plan.Executable, plan.Executable, plan.ConfigPath),
	}
	if err = userservice.Apply(context.Background(), plan, runner, time.Second); err != nil {
		t.Fatal(err)
	}
	runsAfterActivation := len(runner.runs)

	var output bytes.Buffer
	command := NewRoot(Dependencies{
		In:          strings.NewReader("\n"),
		Out:         &output,
		Err:         io.Discard,
		Version:     "test",
		Profile:     ProductionProfile,
		UserService: runner,
		IsTerminal:  func(io.Reader, io.Writer) bool { return true },
	})
	command.SetArgs([]string{"--config", configPath})
	if err = command.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); !strings.Contains(got, "gateway_healthy") || strings.Contains(got, "AEGIS / bootstrap") || strings.Contains(got, "CLEAN does not authenticate ACTIVE") {
		t.Fatalf("second bare-run output did not preserve healthy gateway ownership: %s", got)
	}
	if len(runner.runs) != runsAfterActivation {
		t.Fatalf("second bare run mutated gateway lifecycle: before=%d after=%d calls=%v", runsAfterActivation, len(runner.runs), runner.runs)
	}
	authorityPath := filepath.Join(root, "state", "persistence", "authority-v1")
	if _, statErr := os.Stat(authorityPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("second bare run opened writable authority persistence at %s: %v", authorityPath, statErr)
	}
}
