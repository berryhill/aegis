// Package userservice owns deterministic user-scoped Aegis service lifecycle.
// It never installs a system service, enables linger, or embeds credentials.
package userservice

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/berryhill/aegis/internal/config"
)

const (
	UnitName     = "aegis.service"
	Confirmation = "INSTALL AEGIS USER SERVICE"
)

var (
	ErrForeignUnit         = errors.New("existing user unit is not owned by this exact Aegis plan")
	ErrServiceNotInstalled = errors.New("gateway_not_installed: Aegis gateway is not installed; run `aegis gateway install`")
)

// ActivationError identifies the failed phase and preserves rollback evidence.
type ActivationError struct {
	Phase       string
	Err         error
	RollbackErr error
}

func (e *ActivationError) Error() string {
	if e.RollbackErr != nil {
		return fmt.Sprintf("user service activation phase %q failed: %v; rollback failed: %v", e.Phase, e.Err, e.RollbackErr)
	}
	return fmt.Sprintf("user service activation phase %q failed: %v", e.Phase, e.Err)
}

func (e *ActivationError) Unwrap() []error { return []error{e.Err, e.RollbackErr} }

type Plan struct {
	UnitPath     string `json:"unit_path"`
	UnitDigest   string `json:"unit_digest"`
	Executable   string `json:"executable"`
	ConfigPath   string `json:"config_path"`
	UnixSocket   string `json:"unix_socket"`
	Origin       string `json:"console_origin"`
	Principal    string `json:"principal"`
	Confirmation string `json:"confirmation"`
	unit         []byte
}

// LifecycleResult is emitted only after the exact owned unit reaches its
// observed terminal lifecycle state. Readiness fields are false for stop.
type LifecycleResult struct {
	Action       string `json:"action"`
	Unit         string `json:"unit"`
	UnitPath     string `json:"unit_path"`
	UnitDigest   string `json:"unit_digest"`
	Active       bool   `json:"active"`
	Ready        bool   `json:"ready"`
	AuditCurrent bool   `json:"audit_current"`
}

type GatewayState string

const (
	GatewayNotInstalled GatewayState = "not_installed"
	GatewayStopped      GatewayState = "stopped"
	GatewayHealthy      GatewayState = "healthy"
	GatewayUnhealthy    GatewayState = "unhealthy"
	GatewayMismatched   GatewayState = "mismatched"
)

// GatewayObservation is an observational startup admission result. Healthy is
// returned only for the exact installed and loaded unit after authenticated,
// audit-current readiness succeeds. Observation never starts or rewrites a unit.
type GatewayObservation struct {
	State  GatewayState `json:"state"`
	Reason string       `json:"reason"`
	Err    error        `json:"-"`
}

type Runner interface {
	Run(context.Context, ...string) error
	Output(context.Context, ...string) ([]byte, error)
}

type Systemctl struct{ Executable string }

func (s Systemctl) Run(ctx context.Context, args ...string) error {
	path := s.Executable
	if path == "" {
		path = "systemctl"
	}
	command := exec.CommandContext(ctx, path, append([]string{"--user"}, args...)...)
	command.Stdin = nil
	command.Stdout = nil
	var diagnostic bytes.Buffer
	command.Stderr = &diagnostic
	if err := command.Run(); err != nil {
		return fmt.Errorf("systemctl --user %s: %s: %w", strings.Join(args, " "), strings.TrimSpace(diagnostic.String()), err)
	}
	return nil
}

func (s Systemctl) Output(ctx context.Context, args ...string) ([]byte, error) {
	path := s.Executable
	if path == "" {
		path = "systemctl"
	}
	command := exec.CommandContext(ctx, path, append([]string{"--user"}, args...)...)
	command.Stdin = nil
	var output, diagnostic bytes.Buffer
	command.Stdout = &output
	command.Stderr = &diagnostic
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("systemctl --user %s: %s: %w", strings.Join(args, " "), strings.TrimSpace(diagnostic.String()), err)
	}
	return output.Bytes(), nil
}

func Preview(executable, configPath string) (Plan, error) {
	executable, err := filepath.Abs(executable)
	if err != nil {
		return Plan{}, err
	}
	if info, statErr := os.Stat(executable); statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return Plan{}, errors.New("Aegis executable must be one absolute executable regular file")
	}
	inspection := config.Inspect(configPath)
	if inspection.State != config.StateValid || !inspection.FilePresent {
		return Plan{}, errors.New("user service requires one secure file-backed valid configuration")
	}
	cfg := inspection.Config
	if cfg.API.Token == "" || cfg.API.TokenFile == "" || cfg.API.UnixSocket == "" {
		return Plan{}, errors.New("serve-ready protected token_file and unix_socket are required")
	}
	current, err := user.Current()
	if err != nil || current.Uid != cfg.Principal.UID || current.Username != cfg.Principal.User {
		return Plan{}, errors.New("configured principal does not match freshly authenticated host identity")
	}
	unitDir, err := userUnitDirectory()
	if err != nil {
		return Plan{}, err
	}
	unit := []byte(fmt.Sprintf("# Generated by Aegis; digest-bound user service.\n[Unit]\nDescription=Aegis authenticated local control plane\nAfter=network.target\n\n[Service]\nType=simple\nExecStart=%s serve --config %s\nRestart=on-failure\nRestartSec=2s\nTimeoutStopSec=%s\nKillMode=control-group\nKillSignal=SIGTERM\nUMask=0077\nNoNewPrivileges=true\n\n[Install]\nWantedBy=default.target\n", systemdQuote(executable), systemdQuote(inspection.Path), cfg.API.ShutdownTimeout))
	digest := sha256.Sum256(unit)
	return Plan{UnitPath: filepath.Join(unitDir, UnitName), UnitDigest: hex.EncodeToString(digest[:]), Executable: executable, ConfigPath: inspection.Path, UnixSocket: cfg.API.UnixSocket, Origin: cfg.API.Console.Origin, Principal: current.Username + " (UID " + current.Uid + ")", Confirmation: Confirmation, unit: unit}, nil
}

func Installed(plan Plan) (bool, error) {
	if _, err := os.Lstat(plan.UnitPath); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if err := inspectUnit(plan.UnitPath, plan.unit); err != nil {
		return false, err
	}
	return true, nil
}

func ObserveExactGateway(ctx context.Context, runner Runner, plan Plan) GatewayObservation {
	installed, err := Installed(plan)
	if err != nil {
		return GatewayObservation{State: GatewayMismatched, Reason: "installed_gateway_mismatch", Err: err}
	}
	if !installed {
		return GatewayObservation{State: GatewayNotInstalled, Reason: "gateway_not_installed"}
	}
	if runner == nil {
		return GatewayObservation{State: GatewayUnhealthy, Reason: "gateway_service_manager_unavailable", Err: errors.New("user service manager is unavailable")}
	}
	if err = validateLoadedUnit(ctx, runner, plan); err != nil {
		return GatewayObservation{State: GatewayMismatched, Reason: "loaded_gateway_mismatch", Err: err}
	}
	if err = validateLoadedExecStart(ctx, runner, plan); err != nil {
		return GatewayObservation{State: GatewayMismatched, Reason: "loaded_gateway_mismatch", Err: err}
	}
	active, err := serviceState(ctx, runner, "ActiveState")
	if err != nil {
		return GatewayObservation{State: GatewayUnhealthy, Reason: "gateway_activity_unavailable", Err: err}
	}
	if !active {
		return GatewayObservation{State: GatewayStopped, Reason: "exact_gateway_stopped"}
	}
	cfg, err := config.Load(plan.ConfigPath, nil)
	if err != nil {
		return GatewayObservation{State: GatewayUnhealthy, Reason: "gateway_configuration_unavailable", Err: err}
	}
	if err = probeReady(ctx, cfg); err != nil {
		return GatewayObservation{State: GatewayUnhealthy, Reason: "authenticated_gateway_unhealthy", Err: err}
	}
	return GatewayObservation{State: GatewayHealthy, Reason: "authenticated_exact_gateway_ready"}
}

func Apply(ctx context.Context, plan Plan, runner Runner, timeout time.Duration) error {
	if runner == nil {
		return errors.New("user service manager is unavailable")
	}
	current, err := Preview(plan.Executable, plan.ConfigPath)
	if err != nil {
		return err
	}
	if !samePlan(current, plan) {
		return errors.New("user service plan drifted after preview")
	}
	if err = inspectUnit(plan.UnitPath, plan.unit); err != nil {
		return err
	}
	active, err := serviceState(ctx, runner, "ActiveState")
	if err != nil {
		return activationFailure("capture_state", err, nil)
	}
	enabled, err := serviceState(ctx, runner, "UnitFileState")
	if err != nil {
		return activationFailure("capture_state", err, nil)
	}
	if err = os.MkdirAll(filepath.Dir(plan.UnitPath), 0700); err != nil {
		return activationFailure("publish", err, nil)
	}
	published, err := publishUnit(plan.UnitPath, plan.unit)
	if err != nil {
		return activationFailure("publish", err, nil)
	}
	rollback := func() error {
		var rollbackErrs []error
		if !enabled {
			rollbackErrs = appendError(rollbackErrs, runner.Run(context.Background(), "disable", UnitName))
		}
		if !active {
			rollbackErrs = appendError(rollbackErrs, runner.Run(context.Background(), "stop", UnitName))
		}
		if published {
			rollbackErrs = appendError(rollbackErrs, os.Remove(plan.UnitPath))
		}
		rollbackErrs = appendError(rollbackErrs, runner.Run(context.Background(), "daemon-reload"))
		return errors.Join(rollbackErrs...)
	}
	if err = runner.Run(ctx, "daemon-reload"); err != nil {
		return activationFailure("daemon_reload", err, rollback())
	}
	if err = runner.Run(ctx, "enable", "--now", UnitName); err != nil {
		return activationFailure("enable_start", err, rollback())
	}
	if err = validateLoadedUnit(ctx, runner, plan); err != nil {
		return activationFailure("exact_unit_validation", err, rollback())
	}
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	readyCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cfg, err := config.Load(plan.ConfigPath, nil)
	if err != nil {
		return activationFailure("authenticated_readiness", err, rollback())
	}
	if err = waitReady(readyCtx, cfg); err != nil {
		return activationFailure("audit_current_readiness", err, rollback())
	}
	return nil
}

// EnsureReady restarts an already-installed exact Aegis service and requires its
// authenticated, audit-current readiness before returning. Restart is required
// because the active process may still hold a state root that was removed and
// recreated during first-run recovery.
func EnsureReady(ctx context.Context, plan Plan, runner Runner, timeout time.Duration) error {
	if runner == nil {
		return errors.New("user service manager is unavailable")
	}
	current, err := Preview(plan.Executable, plan.ConfigPath)
	if err != nil {
		return err
	}
	if !samePlan(current, plan) {
		return errors.New("user service plan drifted after preview")
	}
	installed, err := Installed(plan)
	if err != nil {
		return err
	}
	if !installed {
		return ErrServiceNotInstalled
	}
	// systemd may still have a stale or foreign unit definition loaded even
	// when the approved unit bytes are present on disk. Establish the exact
	// loaded fragment and ExecStart identity before any lifecycle mutation.
	if err = validateLoadedIdentity(ctx, runner, plan); err != nil {
		return activationFailure("exact_unit_validation", err, nil)
	}
	if err = runner.Run(ctx, "restart", UnitName); err != nil {
		return activationFailure("restart", err, nil)
	}
	// Revalidate after restart so readiness is bound to the exact unit that
	// systemd actually activated, not only the preflight observation.
	if err = validateLoadedIdentity(ctx, runner, plan); err != nil {
		return activationFailure("exact_unit_validation", err, nil)
	}
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	readyCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cfg, err := config.Load(plan.ConfigPath, nil)
	if err != nil {
		return activationFailure("authenticated_readiness", err, nil)
	}
	if err = waitReady(readyCtx, cfg); err != nil {
		return activationFailure("audit_current_readiness", err, nil)
	}
	return nil
}

func samePlan(left, right Plan) bool {
	return left.UnitPath == right.UnitPath && left.UnitDigest == right.UnitDigest &&
		left.Executable == right.Executable && left.ConfigPath == right.ConfigPath &&
		left.UnixSocket == right.UnixSocket && left.Origin == right.Origin &&
		left.Principal == right.Principal && left.Confirmation == right.Confirmation &&
		bytes.Equal(left.unit, right.unit)
}

func activationFailure(phase string, err, rollbackErr error) error {
	return &ActivationError{Phase: phase, Err: err, RollbackErr: rollbackErr}
}

func appendError(errs []error, err error) []error {
	if err != nil {
		return append(errs, err)
	}
	return errs
}

func serviceState(ctx context.Context, runner Runner, property string) (bool, error) {
	output, err := runner.Output(ctx, "show", UnitName, "--property", property, "--value")
	if err != nil {
		return false, err
	}
	switch strings.TrimSpace(string(output)) {
	case "active", "enabled", "enabled-runtime", "linked", "linked-runtime":
		return true, nil
	case "", "inactive", "failed", "activating", "deactivating", "disabled", "static", "masked", "masked-runtime", "not-found":
		return false, nil
	default:
		return false, fmt.Errorf("unexpected %s state %q", property, strings.TrimSpace(string(output)))
	}
}

func Action(ctx context.Context, runner Runner, plan Plan, action string, timeout time.Duration) (LifecycleResult, error) {
	if runner == nil {
		return LifecycleResult{}, errors.New("user service manager is unavailable")
	}
	switch action {
	case "start", "stop", "restart":
	default:
		return LifecycleResult{}, errors.New("unsupported user service action")
	}
	installed, err := Installed(plan)
	if err != nil {
		return LifecycleResult{}, err
	}
	if !installed {
		return LifecycleResult{}, ErrServiceNotInstalled
	}
	if err = runner.Run(ctx, action, UnitName); err != nil {
		return LifecycleResult{}, activationFailure(action, err, nil)
	}
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	observeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err = validateLoadedUnit(observeCtx, runner, plan); err != nil {
		return LifecycleResult{}, activationFailure("exact_unit_validation", err, nil)
	}
	wantActive := action != "stop"
	if err = waitActiveState(observeCtx, runner, wantActive); err != nil {
		return LifecycleResult{}, activationFailure("observed_state", err, nil)
	}
	result := LifecycleResult{Action: action, Unit: UnitName, UnitPath: plan.UnitPath, UnitDigest: plan.UnitDigest, Active: wantActive}
	if !wantActive {
		return result, nil
	}
	cfg, err := config.Load(plan.ConfigPath, nil)
	if err != nil {
		return LifecycleResult{}, activationFailure("authenticated_readiness", err, nil)
	}
	if err = waitReady(observeCtx, cfg); err != nil {
		return LifecycleResult{}, activationFailure("audit_current_readiness", err, nil)
	}
	result.Ready = true
	result.AuditCurrent = true
	return result, nil
}

func validateLoadedUnit(ctx context.Context, runner Runner, plan Plan) error {
	output, err := runner.Output(ctx, "show", UnitName, "--property", "FragmentPath", "--value")
	if err != nil {
		return err
	}
	loaded := strings.TrimSpace(string(output))
	if loaded == "" || filepath.Clean(loaded) != filepath.Clean(plan.UnitPath) {
		return fmt.Errorf("%w: loaded fragment does not match approved unit path", ErrForeignUnit)
	}
	return nil
}

func validateLoadedIdentity(ctx context.Context, runner Runner, plan Plan) error {
	if err := validateLoadedUnit(ctx, runner, plan); err != nil {
		return err
	}
	return validateLoadedExecStart(ctx, runner, plan)
}

func validateLoadedExecStart(ctx context.Context, runner Runner, plan Plan) error {
	output, err := runner.Output(ctx, "show", UnitName, "--property", "ExecStart", "--value")
	if err != nil {
		return err
	}
	loaded := strings.TrimSpace(string(output))
	pathStart := strings.Index(loaded, "path=")
	argvStart := strings.Index(loaded, " ; argv[]=")
	argvEnd := strings.Index(loaded, " ; ignore_errors=")
	if pathStart < 0 || argvStart < 0 || argvEnd < 0 || pathStart+len("path=") >= argvStart || argvStart+len(" ; argv[]=") >= argvEnd {
		return fmt.Errorf("%w: loaded ExecStart is missing exact path or argv identity", ErrForeignUnit)
	}
	paths, err := parseSystemdShowWords(strings.TrimSpace(loaded[pathStart+len("path=") : argvStart]))
	if err != nil || len(paths) != 1 {
		return fmt.Errorf("%w: loaded ExecStart path is malformed", ErrForeignUnit)
	}
	argv, err := parseSystemdShowWords(strings.TrimSpace(loaded[argvStart+len(" ; argv[]=") : argvEnd]))
	if err != nil {
		return fmt.Errorf("%w: loaded ExecStart argv is malformed", ErrForeignUnit)
	}
	want := []string{plan.Executable, "serve", "--config", plan.ConfigPath}
	if paths[0] != plan.Executable || len(argv) != len(want) {
		return fmt.Errorf("%w: loaded ExecStart does not match approved executable and config", ErrForeignUnit)
	}
	for index := range want {
		if argv[index] != want[index] {
			return fmt.Errorf("%w: loaded ExecStart does not match approved executable and config", ErrForeignUnit)
		}
	}
	return nil
}

func parseSystemdShowWords(value string) ([]string, error) {
	var words []string
	var word strings.Builder
	var quote byte
	started := false
	flush := func() {
		if started {
			words = append(words, word.String())
			word.Reset()
			started = false
		}
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if quote == 0 && (character == ' ' || character == '\t' || character == '\n' || character == '\r') {
			flush()
			continue
		}
		if character == '\'' || character == '"' {
			if quote == 0 {
				quote = character
				started = true
				continue
			}
			if quote == character {
				quote = 0
				continue
			}
		}
		if character == '\\' {
			if index+1 >= len(value) {
				return nil, errors.New("trailing escape")
			}
			if value[index+1] == 'x' {
				if index+3 >= len(value) {
					return nil, errors.New("short hexadecimal escape")
				}
				decoded, err := hex.DecodeString(value[index+2 : index+4])
				if err != nil {
					return nil, err
				}
				word.WriteByte(decoded[0])
				index += 3
				started = true
				continue
			}
			index++
			word.WriteByte(value[index])
			started = true
			continue
		}
		word.WriteByte(character)
		started = true
	}
	if quote != 0 {
		return nil, errors.New("unterminated quote")
	}
	flush()
	return words, nil
}

func waitActiveState(ctx context.Context, runner Runner, wantActive bool) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		active, err := serviceState(ctx, runner, "ActiveState")
		if err == nil && active == wantActive {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.Join(ctx.Err(), err)
		case <-ticker.C:
		}
	}
}

func Status(ctx context.Context, runner Runner, plan Plan) (map[string]any, error) {
	if err := inspectUnit(plan.UnitPath, plan.unit); err != nil {
		return nil, err
	}
	active, activeErr := runner.Output(ctx, "is-active", UnitName)
	linger := "unknown"
	if current, err := user.Current(); err == nil {
		if output, probeErr := exec.CommandContext(ctx, "loginctl", "show-user", current.Uid, "-p", "Linger", "--value").Output(); probeErr == nil {
			linger = strings.TrimSpace(string(output))
		}
	}
	return map[string]any{"unit": UnitName, "unit_path": plan.UnitPath, "unit_digest": plan.UnitDigest, "active": strings.TrimSpace(string(active)), "active_error": activeErr != nil, "linger": linger, "logout_survival_claimed": linger == "yes"}, nil
}

func Uninstall(ctx context.Context, plan Plan, runner Runner) error {
	if err := inspectUnit(plan.UnitPath, plan.unit); err != nil {
		return err
	}
	if err := runner.Run(ctx, "disable", "--now", UnitName); err != nil {
		return err
	}
	if err := os.Remove(plan.UnitPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return runner.Run(ctx, "daemon-reload")
}

// PurgeForReset stops and removes only the exact Aegis-owned gateway unit
// derived from the still-present configuration. An absent unit file is a no-op
// only when systemd authoritatively reports the exact unit not loaded and
// inactive; an unverified or foreign unit fails closed before mutation.
func PurgeForReset(ctx context.Context, executable, configPath string, runner Runner) (bool, error) {
	if runner == nil {
		return false, errors.New("user service manager is unavailable")
	}
	unitDir, err := userUnitDirectory()
	if err != nil {
		return false, err
	}
	unitPath := filepath.Join(unitDir, UnitName)
	if _, err = os.Lstat(unitPath); errors.Is(err, os.ErrNotExist) {
		if err = verifyAbsentUnitInactive(ctx, runner); err != nil {
			return false, err
		}
		return false, nil
	} else if err != nil {
		return false, err
	}
	plan, err := Preview(executable, configPath)
	if err != nil {
		return false, err
	}
	if err = inspectUnit(plan.UnitPath, plan.unit); err != nil {
		return false, err
	}
	if err = Uninstall(ctx, plan, runner); err != nil {
		return false, err
	}
	if _, err = os.Lstat(plan.UnitPath); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			err = errors.New("gateway unit remains after reset purge")
		}
		return false, err
	}
	return true, nil
}

func verifyAbsentUnitInactive(ctx context.Context, runner Runner) error {
	loadState, err := runner.Output(ctx, "show", UnitName, "--property", "LoadState", "--value")
	if err != nil {
		return fmt.Errorf("inspect absent gateway load state: %w", err)
	}
	activeState, err := runner.Output(ctx, "show", UnitName, "--property", "ActiveState", "--value")
	if err != nil {
		return fmt.Errorf("inspect absent gateway active state: %w", err)
	}
	load := strings.TrimSpace(string(loadState))
	active := strings.TrimSpace(string(activeState))
	if load == "not-found" && active == "inactive" {
		return nil
	}
	return fmt.Errorf("%w: gateway unit file is absent but systemd reports load=%q active=%q", ErrForeignUnit, load, active)
}

func inspectUnit(path string, expected []byte) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || !ok || int(stat.Uid) != os.Geteuid() || stat.Nlink != 1 {
		return ErrForeignUnit
	}
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, expected) {
		return ErrForeignUnit
	}
	return nil
}

func publishUnit(path string, unit []byte) (bool, error) {
	if _, err := os.Lstat(path); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".aegis.service.*")
	if err != nil {
		return false, err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err = temporary.Chmod(0600); err == nil {
		_, err = temporary.Write(unit)
	}
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return false, err
	}
	if err = os.Link(temporaryPath, path); err != nil {
		return false, err
	}
	return true, nil
}

func probeReady(ctx context.Context, cfg config.Config) error {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", cfg.API.UnixSocket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/readyz", nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+cfg.API.Token)
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	var body struct {
		Status string `json:"status"`
		Audit  struct {
			State      string `json:"state"`
			Reason     string `json:"reason"`
			Current    bool   `json:"current"`
			Verifiable bool   `json:"verifiable"`
		} `json:"audit"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&body); err != nil {
		return fmt.Errorf("readiness returned HTTP %d with invalid response: %w", response.StatusCode, err)
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return fmt.Errorf("authenticated readiness denied: HTTP %d", response.StatusCode)
	}
	if response.StatusCode == http.StatusOK && body.Status == "ready" && body.Audit.Current && body.Audit.Verifiable {
		return nil
	}
	return fmt.Errorf("readiness not audit-current: HTTP %d status=%q audit_state=%q reason=%q", response.StatusCode, body.Status, body.Audit.State, body.Audit.Reason)
}

func waitReady(ctx context.Context, cfg config.Config) error {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", cfg.API.UnixSocket)
	}}
	client := &http.Client{Transport: transport, Timeout: time.Second}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/readyz", nil)
		request.Header.Set("Authorization", "Bearer "+cfg.API.Token)
		response, err := client.Do(request)
		if err == nil {
			var body struct {
				Status string `json:"status"`
				Audit  struct {
					State      string `json:"state"`
					Reason     string `json:"reason"`
					Pending    int    `json:"pending"`
					Current    bool   `json:"current"`
					Verifiable bool   `json:"verifiable"`
				} `json:"audit"`
			}
			decodeErr := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&body)
			_ = response.Body.Close()
			switch {
			case decodeErr != nil:
				lastErr = fmt.Errorf("readiness returned HTTP %d with invalid response: %w", response.StatusCode, decodeErr)
			case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
				return fmt.Errorf("authenticated readiness denied: HTTP %d", response.StatusCode)
			case response.StatusCode == http.StatusOK && body.Status == "ready" && body.Audit.Current && body.Audit.Verifiable:
				return nil
			case response.StatusCode == http.StatusServiceUnavailable && body.Audit.State == "pending" && body.Audit.Pending > 0 && body.Audit.Verifiable:
				if deliveryErr := deliverPendingAudit(ctx, client, cfg.API.Token); deliveryErr != nil {
					lastErr = deliveryErr
				} else {
					lastErr = fmt.Errorf("audit delivery progressed; awaiting audit-current readiness")
				}
			case response.StatusCode != http.StatusOK:
				lastErr = fmt.Errorf("readiness returned HTTP %d: status=%q audit_state=%q audit_current=%t audit_verifiable=%t reason=%q", response.StatusCode, body.Status, body.Audit.State, body.Audit.Current, body.Audit.Verifiable, body.Audit.Reason)
			default:
				lastErr = fmt.Errorf("readiness not audit-current: status=%q audit_state=%q reason=%q", body.Status, body.Audit.State, body.Audit.Reason)
			}
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return errors.Join(ctx.Err(), lastErr)
		case <-ticker.C:
		}
	}
}

func deliverPendingAudit(ctx context.Context, client *http.Client, token string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/v1/audit/delivery", strings.NewReader(`{"limit":100}`))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("audit delivery unavailable: %w", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("audit delivery denied: HTTP %d", response.StatusCode)
	}
	return nil
}

func userUnitDirectory() (string, error) {
	if value := os.Getenv("XDG_CONFIG_HOME"); value != "" {
		if !filepath.IsAbs(value) {
			return "", errors.New("XDG_CONFIG_HOME must be absolute")
		}
		return filepath.Join(value, "systemd", "user"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "systemd", "user"), nil
}

func systemdQuote(value string) string {
	return strconv.Quote(value)
}
