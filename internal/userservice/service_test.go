package userservice

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type recordingRunner struct {
	calls       [][]string
	err         error
	runErrors   map[string]error
	activeState string
	unitState   string
}

func (r *recordingRunner) Run(_ context.Context, args ...string) error {
	r.calls = append(r.calls, append([]string(nil), args...))
	if err := r.runErrors[strings.Join(args, " ")]; err != nil {
		return err
	}
	return r.err
}

func (r *recordingRunner) Output(_ context.Context, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	if len(args) >= 4 && args[0] == "show" && args[2] == "--property" {
		switch args[3] {
		case "ActiveState":
			return []byte(defaultString(r.activeState, "inactive") + "\n"), nil
		case "UnitFileState":
			return []byte(defaultString(r.unitState, "disabled") + "\n"), nil
		}
	}
	return nil, fmt.Errorf("unexpected output call: %v", args)
}

func defaultString(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
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

func TestApplyActivatesInOrderAndRequiresAuditCurrentReadiness(t *testing.T) {
	plan, stop := readyServicePlan(t, http.StatusOK, `{"status":"ready","audit":{"current":true,"verifiable":true}}`)
	defer stop()
	runner := &recordingRunner{}
	if err := Apply(context.Background(), plan, runner, time.Second); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"show", UnitName, "--property", "ActiveState", "--value"},
		{"show", UnitName, "--property", "UnitFileState", "--value"},
		{"daemon-reload"},
		{"enable", "--now", UnitName},
	}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("activation phases were not ordered deterministically:\n got: %v\nwant: %v", runner.calls, want)
	}
	if _, err := os.Stat(plan.UnitPath); err != nil {
		t.Fatalf("successful activation did not retain published unit: %v", err)
	}
}

func TestApplyRejectsPlanDriftBeforeMutation(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	executable, configPath := serviceFixture(t)
	plan, err := Preview(executable, configPath)
	if err != nil {
		t.Fatal(err)
	}
	plan.Origin = "http://drifted.invalid"
	runner := &recordingRunner{}
	err = Apply(context.Background(), plan, runner, time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "plan drifted") {
		t.Fatalf("apply-time drift was not denied: %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("systemctl was invoked before plan-drift denial: %v", runner.calls)
	}
	if _, statErr := os.Stat(plan.UnitPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("drift denial mutated unit path: %v", statErr)
	}
}

func TestApplyFailureRemovesNewPublicationAndReportsRollbackFailure(t *testing.T) {
	plan, stop := readyServicePlan(t, http.StatusServiceUnavailable, `{"status":"not_ready","audit":{"current":false,"verifiable":false,"reason":"checkpoint_stale"}}`)
	defer stop()
	rollbackErr := errors.New("rollback disable denied")
	runner := &recordingRunner{runErrors: map[string]error{"disable " + UnitName: rollbackErr}}
	err := Apply(context.Background(), plan, runner, 20*time.Millisecond)
	var activationErr *ActivationError
	if !errors.As(err, &activationErr) || activationErr.Phase != "audit_current_readiness" {
		t.Fatalf("readiness failure lacked activation phase: %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, rollbackErr) || !strings.Contains(err.Error(), "checkpoint_stale") {
		t.Fatalf("readiness diagnostics or joined rollback evidence missing: %v", err)
	}
	if _, statErr := os.Stat(plan.UnitPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("newly published unit survived failed activation: %v", statErr)
	}
	wantSuffix := [][]string{{"disable", UnitName}, {"stop", UnitName}, {"daemon-reload"}}
	if got := runner.calls[len(runner.calls)-len(wantSuffix):]; !reflect.DeepEqual(got, wantSuffix) {
		t.Fatalf("rollback was not deterministic: got %v want suffix %v", got, wantSuffix)
	}
}

func TestApplyFailurePreservesPreExistingExactState(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	executable, configPath := serviceFixture(t)
	plan, err := Preview(executable, configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(plan.UnitPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(plan.UnitPath, plan.unit, 0600); err != nil {
		t.Fatal(err)
	}
	runner := &recordingRunner{
		activeState: "active",
		unitState:   "enabled",
		runErrors:   map[string]error{"enable --now " + UnitName: errors.New("restart failed")},
	}
	err = Apply(context.Background(), plan, runner, time.Second)
	var activationErr *ActivationError
	if !errors.As(err, &activationErr) || activationErr.Phase != "enable_start" {
		t.Fatalf("enable/start failure lacked phase evidence: %v", err)
	}
	actual, readErr := os.ReadFile(plan.UnitPath)
	if readErr != nil || !reflect.DeepEqual(actual, plan.unit) {
		t.Fatalf("pre-existing exact unit was not preserved: read=%v", readErr)
	}
	for _, call := range runner.calls {
		if call[0] == "disable" || call[0] == "stop" {
			t.Fatalf("rollback removed pre-existing service state: %v", runner.calls)
		}
	}
}

func readyServicePlan(t *testing.T, status int, body string) (Plan, func()) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	executable, configPath := serviceFixture(t)
	initial, err := Preview(executable, configPath)
	if err != nil {
		t.Fatal(err)
	}
	shortRoot, err := os.MkdirTemp("", "aegis-ready-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(shortRoot) })
	shortSocket := filepath.Join(shortRoot, "a.sock")
	document, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	document = []byte(strings.Replace(string(document), initial.UnixSocket, shortSocket, 1))
	if err = os.WriteFile(configPath, document, 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := Preview(executable, configPath)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", plan.UnixSocket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") == "" {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(status)
		_, _ = writer.Write([]byte(body))
	})}
	go func() { _ = server.Serve(listener) }()
	return plan, func() {
		_ = server.Close()
		_ = listener.Close()
	}
}
