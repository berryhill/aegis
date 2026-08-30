package userservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/user"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type recordingRunner struct {
	calls               [][]string
	err                 error
	runErrors           map[string]error
	activeState         string
	unitState           string
	loadState           string
	fragmentPath        string
	execStart           string
	restartFragmentPath string
	restartExecStart    string
}

func (r *recordingRunner) Run(_ context.Context, args ...string) error {
	r.calls = append(r.calls, append([]string(nil), args...))
	if err := r.runErrors[strings.Join(args, " ")]; err != nil {
		return err
	}
	if len(args) > 0 {
		switch args[0] {
		case "start", "restart":
			r.activeState = "active"
			if args[0] == "restart" {
				if r.restartFragmentPath != "" {
					r.fragmentPath = r.restartFragmentPath
				}
				if r.restartExecStart != "" {
					r.execStart = r.restartExecStart
				}
			}
		case "stop":
			r.activeState = "inactive"
		}
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
		case "LoadState":
			return []byte(defaultString(r.loadState, "not-found") + "\n"), nil
		case "FragmentPath":
			return []byte(r.fragmentPath + "\n"), nil
		case "ExecStart":
			return []byte(r.execStart + "\n"), nil
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

func TestActionRejectsMissingInstallationBeforeSystemctl(t *testing.T) {
	plan := Plan{UnitPath: filepath.Join(t.TempDir(), UnitName), unit: []byte("expected")}
	for _, action := range []string{"start", "stop", "restart"} {
		t.Run(action, func(t *testing.T) {
			runner := &recordingRunner{err: errors.New("raw unit-not-found error")}
			_, err := Action(context.Background(), runner, plan, action, time.Second)
			if !errors.Is(err, ErrServiceNotInstalled) {
				t.Fatalf("missing service did not return stable Aegis error: %v", err)
			}
			if got, want := err.Error(), "gateway_not_installed: Aegis gateway is not installed; run `aegis gateway install`"; got != want {
				t.Fatalf("missing service error = %q, want %q", got, want)
			}
			if len(runner.calls) != 0 {
				t.Fatalf("systemctl invoked for missing service: %v", runner.calls)
			}
			if _, statErr := os.Lstat(plan.UnitPath); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("missing-service denial mutated unit path: %v", statErr)
			}
		})
	}
}

func TestObserveExactGatewayAdmitsOnlyActiveAuthenticatedExactUnitWithoutMutation(t *testing.T) {
	plan, stop := readyServicePlan(t, http.StatusOK, `{"status":"ready","audit":{"current":true,"verifiable":true}}`)
	defer stop()
	if err := os.MkdirAll(filepath.Dir(plan.UnitPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan.UnitPath, plan.unit, 0600); err != nil {
		t.Fatal(err)
	}
	runner := &recordingRunner{activeState: "active", fragmentPath: plan.UnitPath, execStart: loadedExecStartFixture(plan.Executable, plan.ConfigPath)}

	observation := ObserveExactGateway(context.Background(), runner, plan)
	if observation.State != GatewayHealthy || observation.Reason != "authenticated_exact_gateway_ready" {
		t.Fatalf("observation = %+v", observation)
	}
	for _, call := range runner.calls {
		if len(call) > 0 && (call[0] == "start" || call[0] == "restart" || call[0] == "enable") {
			t.Fatalf("observation mutated gateway lifecycle: %v", runner.calls)
		}
	}
}

func TestObserveExactGatewayDeniesStaleLoadedExecStartAtExpectedFragment(t *testing.T) {
	plan, stop := readyServicePlan(t, http.StatusOK, `{"status":"ready","audit":{"current":true,"verifiable":true}}`)
	defer stop()
	if err := os.MkdirAll(filepath.Dir(plan.UnitPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan.UnitPath, plan.unit, 0600); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name       string
		executable string
		configPath string
	}{
		{name: "stale executable", executable: filepath.Join(t.TempDir(), "stale-aegis"), configPath: plan.ConfigPath},
		{name: "stale config", executable: plan.Executable, configPath: filepath.Join(t.TempDir(), "stale.yaml")},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &recordingRunner{activeState: "active", fragmentPath: plan.UnitPath, execStart: loadedExecStartFixture(test.executable, test.configPath)}
			observation := ObserveExactGateway(context.Background(), runner, plan)
			if observation.State != GatewayMismatched || observation.Reason != "loaded_gateway_mismatch" || !errors.Is(observation.Err, ErrForeignUnit) {
				t.Fatalf("stale loaded unit was admitted: %+v", observation)
			}
			for _, call := range runner.calls {
				if len(call) >= 4 && call[3] == "ActiveState" {
					t.Fatalf("activity was inspected after identity denial: %v", runner.calls)
				}
			}
		})
	}
}

func loadedExecStartFixture(executable, configPath string) string {
	return fmt.Sprintf("{ path=%s ; argv[]=%s serve --config %s ; ignore_errors=no ; start_time=[n/a] ; stop_time=[n/a] ; pid=0 ; code=(null) ; status=0/0 }", executable, executable, configPath)
}

func TestObserveExactGatewayKeepsStoppedUnhealthyAndMismatchedStatesDistinct(t *testing.T) {
	plan, stop := readyServicePlan(t, http.StatusServiceUnavailable, `{"status":"not_ready","audit":{"current":false,"verifiable":true,"reason":"checkpoint_stale"}}`)
	defer stop()
	if err := os.MkdirAll(filepath.Dir(plan.UnitPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan.UnitPath, plan.unit, 0600); err != nil {
		t.Fatal(err)
	}

	stopped := ObserveExactGateway(context.Background(), &recordingRunner{activeState: "inactive", fragmentPath: plan.UnitPath, execStart: loadedExecStartFixture(plan.Executable, plan.ConfigPath)}, plan)
	if stopped.State != GatewayStopped {
		t.Fatalf("stopped observation = %+v", stopped)
	}
	unhealthy := ObserveExactGateway(context.Background(), &recordingRunner{activeState: "active", fragmentPath: plan.UnitPath, execStart: loadedExecStartFixture(plan.Executable, plan.ConfigPath)}, plan)
	if unhealthy.State != GatewayUnhealthy {
		t.Fatalf("unhealthy observation = %+v", unhealthy)
	}
	mismatched := ObserveExactGateway(context.Background(), &recordingRunner{activeState: "active", fragmentPath: filepath.Join(t.TempDir(), UnitName), execStart: loadedExecStartFixture(plan.Executable, plan.ConfigPath)}, plan)
	if mismatched.State != GatewayMismatched {
		t.Fatalf("mismatched observation = %+v", mismatched)
	}
}

func TestActionAllowsOnlyBoundedUserServiceLifecycle(t *testing.T) {
	plan, stop := readyServicePlan(t, http.StatusOK, `{"status":"ready","audit":{"current":true,"verifiable":true}}`)
	defer stop()
	if err := os.MkdirAll(filepath.Dir(plan.UnitPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan.UnitPath, plan.unit, 0600); err != nil {
		t.Fatal(err)
	}
	runner := &recordingRunner{fragmentPath: plan.UnitPath, execStart: loadedExecStartFixture(plan.Executable, plan.ConfigPath)}
	for _, action := range []string{"start", "stop", "restart"} {
		if _, err := Action(context.Background(), runner, plan, action, time.Second); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Action(context.Background(), runner, plan, "enable-linger", time.Second); err == nil {
		t.Fatal("unsupported user-service authority was accepted")
	}
	if len(runner.calls) < 6 {
		t.Fatalf("lifecycle did not verify exact unit and observed state: %v", runner.calls)
	}
}

func TestActionStartProgressesOnlyPendingVerifiableAuditAndReturnsTypedResult(t *testing.T) {
	plan, stop := progressingReadyServicePlan(t)
	defer stop()
	if err := os.MkdirAll(filepath.Dir(plan.UnitPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan.UnitPath, plan.unit, 0600); err != nil {
		t.Fatal(err)
	}
	runner := &recordingRunner{fragmentPath: plan.UnitPath, execStart: loadedExecStartFixture(plan.Executable, plan.ConfigPath)}
	result, err := Action(context.Background(), runner, plan, "start", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "start" || result.Unit != UnitName || result.UnitDigest != plan.UnitDigest || !result.Active || !result.Ready || !result.AuditCurrent {
		t.Fatalf("unexpected lifecycle result: %+v", result)
	}
}

func TestProgressingReadyServicePlanValidatesAuditDeliveryRequest(t *testing.T) {
	for _, test := range []struct {
		name        string
		body        string
		contentType string
		want        bool
	}{
		{name: "valid", body: `{"limit":100}`, contentType: "application/json", want: true},
		{name: "malformed", body: `{\\"limit\\":100}`, contentType: "application/json", want: false},
		{name: "wrong limit", body: `{"limit":99}`, contentType: "application/json", want: false},
		{name: "wrong content type", body: `{"limit":100}`, contentType: "text/plain", want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/v1/audit/delivery", strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			if got := validAuditDeliveryRequest(request); got != test.want {
				t.Fatalf("validAuditDeliveryRequest() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestActionRejectsLoadedForeignFragment(t *testing.T) {
	plan, stop := readyServicePlan(t, http.StatusOK, `{"status":"ready","audit":{"current":true,"verifiable":true}}`)
	defer stop()
	if err := os.MkdirAll(filepath.Dir(plan.UnitPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan.UnitPath, plan.unit, 0600); err != nil {
		t.Fatal(err)
	}
	runner := &recordingRunner{fragmentPath: filepath.Join(t.TempDir(), UnitName)}
	if _, err := Action(context.Background(), runner, plan, "start", time.Second); !errors.Is(err, ErrForeignUnit) {
		t.Fatalf("foreign loaded fragment was accepted: %v", err)
	}
}

func TestApplyActivatesInOrderAndRequiresAuditCurrentReadiness(t *testing.T) {
	plan, stop := readyServicePlan(t, http.StatusOK, `{"status":"ready","audit":{"current":true,"verifiable":true}}`)
	defer stop()
	runner := &recordingRunner{fragmentPath: plan.UnitPath, execStart: loadedExecStartFixture(plan.Executable, plan.ConfigPath)}
	if err := Apply(context.Background(), plan, runner, time.Second); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"show", UnitName, "--property", "ActiveState", "--value"},
		{"show", UnitName, "--property", "UnitFileState", "--value"},
		{"daemon-reload"},
		{"enable", "--now", UnitName},
		{"show", UnitName, "--property", "FragmentPath", "--value"},
	}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("activation phases were not ordered deterministically:\n got: %v\nwant: %v", runner.calls, want)
	}
	if _, err := os.Stat(plan.UnitPath); err != nil {
		t.Fatalf("successful activation did not retain published unit: %v", err)
	}
}

func TestEnsureReadyRestartsExactInstalledServiceAndRequiresAuthenticatedReadiness(t *testing.T) {
	plan, stop := readyServicePlan(t, http.StatusOK, `{"status":"ready","audit":{"current":true,"verifiable":true}}`)
	defer stop()
	if err := os.MkdirAll(filepath.Dir(plan.UnitPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan.UnitPath, plan.unit, 0600); err != nil {
		t.Fatal(err)
	}
	// Model an active process whose configured state root was removed and
	// recreated. A start is a no-op for that stale process; a restart is required
	// to bind the newly initialized transport and authority state.
	runner := &recordingRunner{activeState: "active", fragmentPath: plan.UnitPath, execStart: loadedExecStartFixture(plan.Executable, plan.ConfigPath)}
	if err := EnsureReady(context.Background(), plan, runner, time.Second); err != nil {
		t.Fatal(err)
	}
	if want := [][]string{
		{"show", UnitName, "--property", "FragmentPath", "--value"},
		{"show", UnitName, "--property", "ExecStart", "--value"},
		{"restart", UnitName},
		{"show", UnitName, "--property", "FragmentPath", "--value"},
		{"show", UnitName, "--property", "ExecStart", "--value"},
	}; !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("existing service reconciliation calls = %v, want %v", runner.calls, want)
	}
}

func TestEnsureReadyDeniesMismatchedLoadedIdentityBeforeRestart(t *testing.T) {
	plan, stop := readyServicePlan(t, http.StatusOK, `{"status":"ready","audit":{"current":true,"verifiable":true}}`)
	defer stop()
	if err := os.MkdirAll(filepath.Dir(plan.UnitPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan.UnitPath, plan.unit, 0600); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name         string
		fragmentPath string
		executable   string
		configPath   string
	}{
		{name: "foreign fragment", fragmentPath: filepath.Join(t.TempDir(), UnitName), executable: plan.Executable, configPath: plan.ConfigPath},
		{name: "stale executable", fragmentPath: plan.UnitPath, executable: filepath.Join(t.TempDir(), "stale-aegis"), configPath: plan.ConfigPath},
		{name: "stale config", fragmentPath: plan.UnitPath, executable: plan.Executable, configPath: filepath.Join(t.TempDir(), "stale.yaml")},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &recordingRunner{
				activeState:  "active",
				fragmentPath: test.fragmentPath,
				execStart:    loadedExecStartFixture(test.executable, test.configPath),
			}
			err := EnsureReady(context.Background(), plan, runner, time.Second)
			var activationErr *ActivationError
			if !errors.As(err, &activationErr) || activationErr.Phase != "exact_unit_validation" || !errors.Is(err, ErrForeignUnit) {
				t.Fatalf("loaded-identity mismatch did not fail closed before restart: %v", err)
			}
			for _, call := range runner.calls {
				if len(call) > 0 && (call[0] == "start" || call[0] == "restart") {
					t.Fatalf("loaded-identity mismatch mutated service lifecycle: %v", runner.calls)
				}
			}
		})
	}
}

func TestEnsureReadyRevalidatesLoadedIdentityAfterRestart(t *testing.T) {
	plan, stop := readyServicePlan(t, http.StatusOK, `{"status":"ready","audit":{"current":true,"verifiable":true}}`)
	defer stop()
	if err := os.MkdirAll(filepath.Dir(plan.UnitPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan.UnitPath, plan.unit, 0600); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name                string
		restartFragmentPath string
		restartExecutable   string
		restartConfigPath   string
	}{
		{name: "foreign fragment", restartFragmentPath: filepath.Join(t.TempDir(), UnitName), restartExecutable: plan.Executable, restartConfigPath: plan.ConfigPath},
		{name: "stale executable", restartFragmentPath: plan.UnitPath, restartExecutable: filepath.Join(t.TempDir(), "stale-aegis"), restartConfigPath: plan.ConfigPath},
		{name: "stale config", restartFragmentPath: plan.UnitPath, restartExecutable: plan.Executable, restartConfigPath: filepath.Join(t.TempDir(), "stale.yaml")},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &recordingRunner{
				activeState:         "active",
				fragmentPath:        plan.UnitPath,
				execStart:           loadedExecStartFixture(plan.Executable, plan.ConfigPath),
				restartFragmentPath: test.restartFragmentPath,
				restartExecStart:    loadedExecStartFixture(test.restartExecutable, test.restartConfigPath),
			}
			err := EnsureReady(context.Background(), plan, runner, time.Second)
			var activationErr *ActivationError
			if !errors.As(err, &activationErr) || activationErr.Phase != "exact_unit_validation" || !errors.Is(err, ErrForeignUnit) {
				t.Fatalf("post-restart loaded-identity mismatch was admitted: %v", err)
			}
			if got := runner.calls[2]; !reflect.DeepEqual(got, []string{"restart", UnitName}) {
				t.Fatalf("identity changed outside expected restart boundary: %v", runner.calls)
			}
		})
	}
}

func TestEnsureReadyDeniesMissingOrUnauditableService(t *testing.T) {
	t.Run("missing exact unit", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
		executable, configPath := serviceFixture(t)
		plan, err := Preview(executable, configPath)
		if err != nil {
			t.Fatal(err)
		}
		runner := &recordingRunner{}
		if err = EnsureReady(context.Background(), plan, runner, time.Millisecond); !errors.Is(err, ErrServiceNotInstalled) {
			t.Fatalf("missing exact service was not denied: %v", err)
		}
		if len(runner.calls) != 0 {
			t.Fatalf("systemctl invoked before installation denial: %v", runner.calls)
		}
	})

	t.Run("audit not current", func(t *testing.T) {
		plan, stop := readyServicePlan(t, http.StatusServiceUnavailable, `{"status":"not_ready","audit":{"current":false,"verifiable":false,"reason":"checkpoint_stale"}}`)
		defer stop()
		if err := os.MkdirAll(filepath.Dir(plan.UnitPath), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(plan.UnitPath, plan.unit, 0600); err != nil {
			t.Fatal(err)
		}
		runner := &recordingRunner{fragmentPath: plan.UnitPath, execStart: loadedExecStartFixture(plan.Executable, plan.ConfigPath)}
		err := EnsureReady(context.Background(), plan, runner, 20*time.Millisecond)
		var activationErr *ActivationError
		if !errors.As(err, &activationErr) || activationErr.Phase != "audit_current_readiness" || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("unauditable readiness did not fail closed with phase evidence: %v", err)
		}
		if want := [][]string{
			{"show", UnitName, "--property", "FragmentPath", "--value"},
			{"show", UnitName, "--property", "ExecStart", "--value"},
			{"restart", UnitName},
			{"show", UnitName, "--property", "FragmentPath", "--value"},
			{"show", UnitName, "--property", "ExecStart", "--value"},
		}; !reflect.DeepEqual(runner.calls, want) {
			t.Fatalf("unexpected readiness-denial calls = %v, want %v", runner.calls, want)
		}
	})
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
	runner := &recordingRunner{fragmentPath: plan.UnitPath, runErrors: map[string]error{"disable " + UnitName: rollbackErr}}
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
		activeState:  "active",
		unitState:    "enabled",
		fragmentPath: plan.UnitPath,
		runErrors:    map[string]error{"enable --now " + UnitName: errors.New("restart failed")},
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

func TestPendingAuditReadinessClassificationIsNarrow(t *testing.T) {
	pending := &readinessNotCurrentError{status: http.StatusServiceUnavailable, statusText: "not_ready", auditState: "pending", reason: "audit_delivery_pending", pending: 1, verifiable: true}
	if !IsPendingAudit(pending) {
		t.Fatal("verifiable pending audit was not classified as recoverable")
	}
	for _, err := range []error{
		&readinessNotCurrentError{status: http.StatusServiceUnavailable, auditState: "pending", pending: 0, verifiable: true},
		&readinessNotCurrentError{status: http.StatusServiceUnavailable, auditState: "pending", pending: 1, verifiable: false},
		&readinessNotCurrentError{status: http.StatusInternalServerError, auditState: "pending", pending: 1, verifiable: true},
		errors.New("audit_delivery_pending"),
	} {
		if IsPendingAudit(err) {
			t.Fatalf("non-recoverable readiness was classified as pending audit: %v", err)
		}
	}
}

func TestPurgeForResetStopsAndRemovesExactInstalledGateway(t *testing.T) {
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
	runner := &recordingRunner{fragmentPath: plan.UnitPath, execStart: loadedExecStartFixture(plan.Executable, plan.ConfigPath)}
	purged, err := PurgeForReset(context.Background(), executable, configPath, runner)
	if err != nil {
		t.Fatal(err)
	}
	if !purged {
		t.Fatal("installed exact gateway was not reported as purged")
	}
	want := [][]string{{"disable", "--now", UnitName}, {"daemon-reload"}}
	if len(runner.calls) < len(want) || !reflect.DeepEqual(runner.calls[len(runner.calls)-len(want):], want) {
		t.Fatalf("gateway purge calls=%v want suffix %v", runner.calls, want)
	}
	if _, err = os.Lstat(plan.UnitPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("gateway unit survived purge: %v", err)
	}
}

func TestPurgeForResetRejectsAbsentUnitWithLoadedActiveGateway(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	executable, configPath := serviceFixture(t)
	runner := &recordingRunner{loadState: "loaded", activeState: "active"}
	purged, err := PurgeForReset(context.Background(), executable, configPath, runner)
	if !errors.Is(err, ErrForeignUnit) {
		t.Fatalf("absent unit with loaded active gateway error=%v", err)
	}
	if purged {
		t.Fatal("unverified loaded gateway was reported as purged")
	}
	want := [][]string{
		{"show", UnitName, "--property", "LoadState", "--value"},
		{"show", UnitName, "--property", "ActiveState", "--value"},
	}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("absent active gateway calls=%v want read-only %v", runner.calls, want)
	}
}

func TestPurgeForResetRejectsForeignGatewayWithoutStoppingIt(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	executable, configPath := serviceFixture(t)
	plan, err := Preview(executable, configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(plan.UnitPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(plan.UnitPath, []byte("foreign"), 0600); err != nil {
		t.Fatal(err)
	}
	runner := &recordingRunner{}
	if _, err = PurgeForReset(context.Background(), executable, configPath, runner); !errors.Is(err, ErrForeignUnit) {
		t.Fatalf("foreign gateway purge error=%v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("foreign gateway triggered systemctl: %v", runner.calls)
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

func validAuditDeliveryRequest(request *http.Request) bool {
	if request.Header.Get("Content-Type") != "application/json" {
		return false
	}
	var input struct {
		Limit int `json:"limit"`
	}
	return json.NewDecoder(request.Body).Decode(&input) == nil && input.Limit == 100
}

func progressingReadyServicePlan(t *testing.T) (Plan, func()) {
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
	var delivered atomic.Bool
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") == "" {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/v1/audit/delivery":
			if !validAuditDeliveryRequest(request) {
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			delivered.Store(true)
			_, _ = writer.Write([]byte(`{"delivered":1,"status":{"state":"healthy","current":true,"verifiable":true}}`))
		case request.Method == http.MethodGet && request.URL.Path == "/readyz" && delivered.Load():
			_, _ = writer.Write([]byte(`{"status":"ready","audit":{"state":"healthy","current":true,"verifiable":true}}`))
		default:
			writer.WriteHeader(http.StatusServiceUnavailable)
			_, _ = writer.Write([]byte(`{"status":"not_ready","audit":{"state":"pending","reason":"delivery_pending","pending":1,"current":false,"verifiable":true}}`))
		}
	})}
	go func() { _ = server.Serve(listener) }()
	return plan, func() {
		_ = server.Close()
		_ = listener.Close()
	}
}
