package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http/httptest"

	managerdomain "github.com/berryhill/aegis/internal/manager"
	"github.com/berryhill/aegis/internal/onboarding"
	authoritybadger "github.com/berryhill/aegis/internal/persistence/authority/badger"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/userservice"
)

type bootstrapGatewayRunner struct {
	exactGatewayRunner
	stop  func() error
	start func() error
}

func (r *bootstrapGatewayRunner) Run(ctx context.Context, args ...string) error {
	if err := r.exactGatewayRunner.Run(ctx, args...); err != nil {
		return err
	}
	if len(args) == 2 && args[0] == "start" && r.start != nil {
		if err := r.start(); err != nil {
			return err
		}
		r.active = true
	}
	if len(args) == 2 && args[0] == "stop" {
		if r.stop != nil {
			if err := r.stop(); err != nil {
				return err
			}
		}
		r.active = false
	}
	return nil
}

func bootstrapGatewayFixture(t *testing.T) (string, *bootstrapGatewayRunner, *net.UnixListener) {
	t.Helper()
	configPath := managerTestConfig(t)
	root := filepath.Dir(configPath)
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))
	socketRoot, err := os.MkdirTemp("", "aegis-bg-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketRoot) })
	socket := filepath.Join(socketRoot, "a.sock")
	token := strings.Repeat("b", 64)
	tokenPath := filepath.Join(root, "api.token")
	if err = os.WriteFile(tokenPath, []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
	document := string(mustCommandRead(t, configPath)) + fmt.Sprintf("api:\n  token_file: %q\n  unix_socket: %q\n", tokenPath, socket)
	if err = os.WriteFile(configPath, []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(socket, 0600); err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/readyz" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ready","audit":{"state":"current","current":true,"verifiable":true}}`))
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close(); _ = listener.Close() })
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := userservice.Preview(executable, configPath)
	if err != nil {
		t.Fatal(err)
	}
	runner := &bootstrapGatewayRunner{exactGatewayRunner: exactGatewayRunner{
		unitPath:  plan.UnitPath,
		execStart: fmt.Sprintf("{ path=%s ; argv[]=%s serve --config %s ; ignore_errors=no ; start_time=[n/a] ; stop_time=[n/a] ; pid=0 ; code=(null) ; status=0/0 }", plan.Executable, plan.Executable, plan.ConfigPath),
	}, stop: server.Close}
	if err = userservice.Apply(context.Background(), plan, runner, time.Second); err != nil {
		t.Fatal(err)
	}
	runner.runs = nil
	return configPath, runner, listener
}

func runBootstrapGatewayInit(t *testing.T, path string, runner *bootstrapGatewayRunner, input string, terminal bool) (string, error) {
	t.Helper()
	var out bytes.Buffer
	root := NewRoot(Dependencies{In: strings.NewReader(input), Out: &out, Err: io.Discard, UserService: runner, IsTerminal: func(io.Reader, io.Writer) bool { return terminal }})
	root.SetArgs([]string{"--config", path, "init"})
	err := root.Execute()
	return out.String(), err
}

func TestBootstrapGatewayApprovedResumePreservesModelAndCertificationGates(t *testing.T) {
	path, runner, _ := bootstrapGatewayFixture(t)
	root := filepath.Dir(path)
	candidate := managerdomain.Candidates()[0]
	digest := strings.Repeat("c", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/version":
			_, _ = w.Write([]byte(`{"version":"0.32.0"}`))
		case "/api/tags":
			_, _ = fmt.Fprintf(w, `{"models":[{"name":%q,"model":%q,"digest":%q,"details":{"quantization_level":"Q4"}}]}`, candidate.OllamaName, candidate.OllamaName, digest)
		default:
			t.Errorf("unexpected Ollama effect: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	hermes := filepath.Join(root, "hermes")
	if err := os.WriteFile(hermes, []byte("#!/bin/sh\n[ \"$1\" = \"--version\" ] || exit 1\nprintf 'Hermes Agent v0.18.0\\nInstall directory: /test/hermes\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	initial, err := config.Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	initial.Manager.Inference.Mode = "external-local"
	initial.Manager.Inference.Endpoint = server.URL
	managerJSON, err := json.Marshal(initial.Manager)
	if err != nil {
		t.Fatal(err)
	}
	document := string(mustCommandRead(t, path)) + fmt.Sprintf("hermes_executable: %q\nmanager: %s\n", hermes, managerJSON)
	if err := os.WriteFile(path, []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	if inspected := config.Inspect(path); inspected.State != config.StateValid {
		t.Fatalf("fixture config: state=%s reason=%s err=%v", inspected.State, inspected.ReasonCode, inspected.Err)
	}
	plan, err := onboarding.PreviewAuthority(path, "host-file")
	if err != nil {
		t.Fatal(err)
	}
	if err = onboarding.InitializeHostAuthority(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if err = onboarding.ApplyAuthority(plan); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	authorityPath := filepath.Join(cfg.StateDir, "persistence", "authority-v1")
	// Hold the real operational store as a daemon would. The exact stop must
	// release it before bootstrap can inspect or construct a local service.
	authority, err := authoritybadger.Open(context.Background(), authorityPath)
	if err != nil {
		t.Fatal(err)
	}
	defer authority.Close()
	stop := runner.stop
	runner.stop = func() error { return errors.Join(stop(), authority.Close()) }
	original := mustCommandRead(t, path)
	output, err := runBootstrapGatewayInit(t, path, runner, "yes\n"+server.URL+"\nno\n", true)
	if err != nil {
		t.Fatalf("resume failed: %v\n%s", err, output)
	}
	for _, want := range []string{"Exact gateway stopped", "3/5", "manager_model_absent", "Bind exact local model", "Model binding declined"} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing %q: %s", want, output)
		}
	}
	if runner.active || len(runner.runs) != 1 || runner.runs[0][0] != "stop" || !bytes.Equal(original, mustCommandRead(t, path)) {
		t.Fatal("resume changed unapproved state or restarted the gateway")
	}
	output, err = runBootstrapGatewayInit(t, path, runner, server.URL+"\nyes\nno\n", true)
	if err != nil {
		t.Fatalf("model approval failed: %v\n%s", err, output)
	}
	cfg, err = config.Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Manager.Inference.Model != candidate.OllamaName || cfg.Manager.Inference.ModelDigest != "sha256:"+digest {
		t.Fatal("exact selected model was not persisted")
	}
	if !strings.Contains(output, "4/5") || !strings.Contains(output, "Certification declined") || strings.Contains(output, "READY / verified") {
		t.Fatalf("certification gate bypassed: %s", output)
	}
	if _, err = os.Lstat(cfg.Manager.Inference.Certification); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unapproved certification exists: %v", err)
	}
	snapshot := inspectOnboarding(context.Background(), path, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if snapshot.State != onboarding.ModelPresent {
		t.Fatalf("persisted resume state=%s reason=%s", snapshot.State, snapshot.Reason)
	}
	output, err = runBootstrapGatewayInit(t, path, runner, "no\n", true)
	if err != nil || !strings.Contains(output, "Certification declined") || strings.Contains(output, "Bind exact local model") || len(runner.runs) != 1 {
		t.Fatalf("certification resume replayed completed stages: %v\n%s", err, output)
	}
}

func TestBootstrapGatewayUnsafeOrStaleTransportNeverFallsBack(t *testing.T) {
	for _, kind := range []string{"stale", "regular", "symlink", "unsafe-mode", "foreign-unit", "foreign-exec", "stop-failed", "transport-remains", "non-terminal"} {
		t.Run(kind, func(t *testing.T) {
			path, runner, listener := bootstrapGatewayFixture(t)
			cfg, err := config.Load(path, nil)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "stale":
				listener.SetUnlinkOnClose(false)
				_ = listener.Close()
			case "regular", "symlink":
				_ = listener.Close()
				if kind == "regular" {
					err = os.WriteFile(cfg.API.UnixSocket, []byte("not a socket"), 0600)
				} else {
					err = os.Symlink(path, cfg.API.UnixSocket)
				}
				if err != nil {
					t.Fatal(err)
				}
			case "unsafe-mode":
				if err = os.Chmod(cfg.API.UnixSocket, 0666); err != nil {
					t.Fatal(err)
				}
			case "foreign-unit":
				runner.unitPath += ".foreign"
			case "foreign-exec":
				runner.execStart = strings.ReplaceAll(runner.execStart, " serve ", " manager ")
			case "stop-failed":
				runner.stop = func() error { return errors.New("injected stop failure") }
			case "transport-remains":
				listener.SetUnlinkOnClose(false)
			}
			original := mustCommandRead(t, path)
			before, err := os.Lstat(cfg.API.UnixSocket)
			if err != nil {
				t.Fatal(err)
			}
			output, err := runBootstrapGatewayInit(t, path, runner, "yes\n", kind != "non-terminal")
			if err == nil {
				t.Fatalf("unsafe bootstrap admitted: %s", output)
			}
			if strings.Contains(output, "artifact-derived bootstrap state:") || !bytes.Equal(original, mustCommandRead(t, path)) {
				t.Fatalf("local bootstrap inspection/mutation crossed denied boundary: %s", output)
			}
			after, statErr := os.Lstat(cfg.API.UnixSocket)
			if statErr != nil || !os.SameFile(before, after) {
				t.Fatalf("socket was removed or replaced: %v", statErr)
			}
			wantRuns := 0
			if kind == "stop-failed" || kind == "transport-remains" {
				wantRuns = 1
			}
			if len(runner.runs) != wantRuns {
				t.Fatalf("unexpected lifecycle calls: %v", runner.runs)
			}
		})
	}
}

func TestBootstrapGatewayBareStartupResumesMissingModelBeforeLocalInspection(t *testing.T) {
	path, runner, _ := bootstrapGatewayFixture(t)
	var out bytes.Buffer
	root := NewRoot(Dependencies{Profile: ProductionProfile, In: strings.NewReader("no\n"), Out: &out, Err: io.Discard, UserService: runner, IsTerminal: func(io.Reader, io.Writer) bool { return true }})
	root.SetArgs([]string{"--config", path})
	err := root.Execute()
	if err != nil || !strings.Contains(out.String(), "Gateway recovery declined") {
		t.Fatalf("bare startup bypassed incomplete bootstrap: %v\n%s", err, out.String())
	}
	if !runner.active || len(runner.runs) != 0 {
		t.Fatal("decline changed the gateway")
	}
}

func TestBootstrapGatewayRejectsPrincipalMismatchBeforeStop(t *testing.T) {
	path, runner, _ := bootstrapGatewayFixture(t)
	cfg, err := config.Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	document := strings.ReplaceAll(string(mustCommandRead(t, path)), cfg.Principal.User, "not-the-authenticated-host-user")
	if err = os.WriteFile(path, []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	output, err := runBootstrapGatewayInit(t, path, runner, "yes\n", true)
	if err == nil || !strings.Contains(err.Error(), "principal") || len(runner.runs) != 0 || !runner.active {
		t.Fatalf("unauthenticated stop: err=%v runs=%v output=%s", err, runner.runs, output)
	}
}

func TestBootstrapGatewayDeclinePreservesLiveOwner(t *testing.T) {
	for _, input := range []string{"no\n", "\n", ""} {
		t.Run(fmt.Sprintf("input-%q", input), func(t *testing.T) {
			path, runner, _ := bootstrapGatewayFixture(t)
			original := mustCommandRead(t, path)
			output, err := runBootstrapGatewayInit(t, path, runner, input, true)
			if err != nil || !strings.Contains(output, "Stop exact gateway to resume bootstrap") || !strings.Contains(output, "Gateway recovery declined") {
				t.Fatalf("bootstrap did not offer safe recovery before local inspection: err=%v output=%s", err, output)
			}
			if !runner.active || len(runner.runs) != 0 || !bytes.Equal(original, mustCommandRead(t, path)) {
				t.Fatal("decline changed gateway or configuration")
			}
			cfg, err := config.Load(path, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = os.Lstat(cfg.API.UnixSocket); err != nil {
				t.Fatal("decline removed live socket", err)
			}
		})
	}
}
