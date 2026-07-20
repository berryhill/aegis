package hermes

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/berryhill/aegis/internal/buildinfo"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/credentials/broker"
	"github.com/berryhill/aegis/internal/store"
	"go.yaml.in/yaml/v3"
)

var AdapterVersion = buildinfo.Version

var versionRE = regexp.MustCompile(`Hermes Agent v([0-9]+)\.([0-9]+)\.([0-9]+)[^\n]*`)
var supportedToolsets = map[string]bool{
	"web": true, "browser": true, "terminal": true, "file": true,
	"code_execution": true, "vision": true, "session_search": true,
	"skills": true, "todo": true, "memory": true, "clarify": true,
	"no_mcp": true, "aegis": true,
}

type processState struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	home  string
	done  chan error
}
type Adapter struct {
	executable string
	log        *slog.Logger
	mu         sync.Mutex
	processes  map[string]*processState
}

// Credential is a resolved, explicitly selected environment credential. The
// value is process input only and must never be logged, persisted, or returned.
type Credential struct {
	Reference string
	TargetEnv string
	Value     string
}

// BrokerBridge describes the sole Aegis-owned MCP server permitted in a
// broker-enabled operational session. It contains paths only, never a bearer
// capability or credential value.
type BrokerBridge struct {
	Enabled    bool
	Executable string
	Timeout    time.Duration
}

func New(executable string, log *slog.Logger) *Adapter {
	return &Adapter{executable: executable, log: log.With("component", "runtime.hermes"), processes: map[string]*processState{}}
}
func (a *Adapter) Discover(ctx context.Context) (core.RuntimeDescriptor, error) {
	p, err := exec.LookPath(a.executable)
	if err != nil {
		return core.RuntimeDescriptor{}, fmt.Errorf("discover Hermes: %w", err)
	}
	p, err = filepath.Abs(p)
	if err != nil {
		return core.RuntimeDescriptor{}, err
	}
	cmd := exec.CommandContext(ctx, p, "--version")
	// Discovery must not trigger Hermes's updater or load project plugins. In
	// particular, querying the runtime must not mutate the normal Hermes home.
	cmd.Env = append(os.Environ(), "HERMES_SKIP_VERSION_CHECK=1", "HERMES_ENABLE_PROJECT_PLUGINS=false")
	b, err := cmd.CombinedOutput()
	if err != nil {
		return core.RuntimeDescriptor{}, fmt.Errorf("Hermes version: %w", err)
	}
	m := versionRE.FindStringSubmatch(string(b))
	if len(m) != 4 {
		return core.RuntimeDescriptor{}, fmt.Errorf("unsupported Hermes version output")
	}
	maj, _ := strconv.Atoi(m[1])
	min, _ := strconv.Atoi(m[2])
	if maj != 0 || min != 18 {
		return core.RuntimeDescriptor{}, fmt.Errorf("unsupported Hermes version %s: adapter supports >=0.18.0,<0.19.0", m[1]+"."+m[2]+"."+m[3])
	}
	install := ""
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "Install directory:") {
			install = strings.TrimSpace(strings.TrimPrefix(line, "Install directory:"))
		}
	}
	caps := []string{"design-stdio", "process-isolation", "disposable-home", "lifecycle-termination", "toolset-selection", "safe-mode", "no-ambient-mcp", "no-ambient-plugins"}
	return core.RuntimeDescriptor{Name: "Hermes Agent", Runtime: "hermes-agent", Version: m[1] + "." + m[2] + "." + m[3], Executable: p, Installation: install, AdapterVersion: AdapterVersion, Capabilities: caps}, nil
}
func ResolveTools(requested []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, t := range requested {
		if t == "*" || strings.EqualFold(t, "all") || !supportedToolsets[t] {
			return nil, fmt.Errorf("Hermes toolset %q is unknown or unsupported", t)
		}
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out, nil
}
func (a *Adapter) launch(ctx context.Context, id, home string, tools []string, model, provider string, credentials []Credential, bridge BrokerBridge) (int, []string, error) {
	resolved, err := ResolveTools(tools)
	if err != nil {
		return 0, nil, err
	}
	if err = os.MkdirAll(home, 0700); err != nil {
		return 0, nil, err
	}
	desc, err := a.Discover(ctx)
	if err != nil {
		return 0, nil, err
	}
	args := []string{"--safe-mode", "--tui", "--toolsets"}
	if bridge.Enabled {
		if len(resolved) != 1 || resolved[0] != "aegis" {
			return 0, nil, errors.New("broker-enabled Hermes sessions require exactly the Aegis toolset")
		}
		if err = writeBrokerBridgeConfig(home, bridge); err != nil {
			return 0, nil, err
		}
		// Safe mode disables all MCP, including the Aegis-owned bridge. The
		// disposable home and exact toolset retain isolation without that flag.
		args = []string{"--ignore-rules", "--tui", "--toolsets"}
	}
	if len(resolved) == 0 {
		args = append(args, "no_mcp")
	} else {
		args = append(args, strings.Join(resolved, ","))
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if provider != "" {
		args = append(args, "--provider", provider)
	}
	cmd := exec.Command(desc.Executable, args...)
	if bridge.Enabled {
		python := gatewayPython(desc)
		if python == "" {
			return 0, nil, errors.New("Hermes TUI gateway Python executable not found")
		}
		cmd = exec.Command(python, "-m", "tui_gateway.entry")
	}
	cmd.Dir = home
	cmd.Env = minimalEnv(home, credentials)
	if bridge.Enabled {
		cmd.Env = append(cmd.Env, "HERMES_PYTHON_SRC_ROOT="+desc.Installation, "HERMES_TUI_TOOLSETS=aegis", "HERMES_TUI_SKILLS=", "HERMES_DISABLE_AUTO_SKILLS=1")
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return 0, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return 0, nil, err
	}
	if err = cmd.Start(); err != nil {
		return 0, nil, err
	}
	ps := &processState{cmd: cmd, stdin: stdin, home: home, done: make(chan error, 1)}
	a.mu.Lock()
	a.processes[id] = ps
	a.mu.Unlock()
	go func() { _, _ = io.Copy(io.Discard, stderr) }()
	go func() { ps.done <- cmd.Wait(); close(ps.done); a.mu.Lock(); delete(a.processes, id); a.mu.Unlock() }()
	if bridge.Enabled {
		messages := make(chan gatewayMessage, 32)
		readErrors := make(chan error, 1)
		go readGateway(stdout, messages, readErrors)
		verifyContext, cancel := context.WithTimeout(ctx, 20*time.Second)
		err = verifyBrokerGateway(verifyContext, stdin, messages, readErrors, model, provider)
		cancel()
		if err != nil {
			_ = stdin.Close()
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
			return 0, nil, fmt.Errorf("Hermes Aegis-tool verification: %w", err)
		}
		go func() {
			for {
				select {
				case <-messages:
				case <-readErrors:
					return
				}
			}
		}()
	} else {
		// Runtime/model output is untrusted and may contain secrets. Drain it to
		// avoid pipe deadlock, but never copy it into Aegis logs or audit records.
		go func() { _, _ = io.Copy(io.Discard, stdout) }()
	}
	// Starting the executable is not enough: reject immediate provider,
	// configuration, and toolset failures instead of recording fake success.
	select {
	case waitErr := <-ps.done:
		if waitErr == nil {
			waitErr = errors.New("Hermes exited during startup")
		}
		return 0, nil, fmt.Errorf("Hermes startup: %w", waitErr)
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		return 0, nil, ctx.Err()
	case <-time.After(300 * time.Millisecond):
	}
	return cmd.Process.Pid, append([]string(nil), resolved...), nil
}

func verifyBrokerGateway(ctx context.Context, stdin io.Writer, messages <-chan gatewayMessage, failures <-chan error, _, _ string) error {
	if _, err := waitGateway(ctx, messages, failures, func(message gatewayMessage) bool {
		return message.Method == "event" && message.Params.Type == "gateway.ready"
	}); err != nil {
		return err
	}
	var lastTotal any
	for attempt := 0; ; attempt++ {
		requestID := fmt.Sprintf("bridge-tools-%d", attempt)
		if err := writeGateway(stdin, requestID, "tools.show", map[string]any{}); err != nil {
			return err
		}
		response, waitErr := waitGateway(ctx, messages, failures, func(message gatewayMessage) bool { return fmt.Sprint(message.ID) == requestID })
		if waitErr != nil {
			return waitErr
		}
		if response.Error != nil {
			return errors.New("Hermes tool inspection failed")
		}
		lastTotal = response.Result["total"]
		if total, ok := response.Result["total"].(float64); ok && total == 1 && gatewayHasOnlyBrokerTool(response.Result) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w (last registered tool count=%v)", ctx.Err(), lastTotal)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func gatewayHasOnlyBrokerTool(result map[string]any) bool {
	sections, ok := result["sections"].([]any)
	if !ok || len(sections) != 1 {
		return false
	}
	section, ok := sections[0].(map[string]any)
	if !ok || section["name"] != "mcp-aegis" {
		return false
	}
	tools, ok := section["tools"].([]any)
	if !ok || len(tools) != 1 {
		return false
	}
	tool, ok := tools[0].(map[string]any)
	return ok && tool["name"] == "mcp__aegis__github_get_repository"
}

func writeBrokerBridgeConfig(home string, bridge BrokerBridge) error {
	executable, err := filepath.Abs(bridge.Executable)
	if err != nil || !filepath.IsAbs(executable) || bridge.Timeout <= 0 || bridge.Timeout > 30*time.Second {
		return errors.New("invalid Aegis credential bridge configuration")
	}
	info, err := os.Stat(executable)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		return errors.New("Aegis credential bridge executable is unavailable")
	}
	configuration := map[string]any{
		"mcp_servers": map[string]any{
			"aegis": map[string]any{
				"command": executable,
				"args": []string{
					"credential-bridge",
					"--material", filepath.Join(home, broker.CapabilityFileName),
					"--timeout", bridge.Timeout.String(),
				},
				"enabled":                      true,
				"timeout":                      int(bridge.Timeout.Seconds()),
				"connect_timeout":              5,
				"supports_parallel_tool_calls": false,
				"tools": map[string]any{
					"include":   []string{"github_get_repository"},
					"resources": false,
					"prompts":   false,
				},
			},
		},
		"platform_toolsets": map[string]any{"cli": []string{"aegis"}},
		"plugins":           map[string]any{"enabled": []string{}},
	}
	encoded, err := yaml.Marshal(configuration)
	if err != nil {
		return errors.New("encode Aegis-owned Hermes bridge configuration")
	}
	if err = os.WriteFile(filepath.Join(home, "config.yaml"), encoded, 0600); err != nil {
		return fmt.Errorf("write Aegis-owned Hermes bridge configuration: %w", err)
	}
	return nil
}
func minimalEnv(home string, credentials []Credential) []string {
	keep := map[string]bool{"PATH": true, "LANG": true, "LC_ALL": true, "TERM": true, "SSL_CERT_FILE": true, "SSL_CERT_DIR": true, "HTTP_PROXY": true, "HTTPS_PROXY": true, "NO_PROXY": true}
	var out []string
	for _, x := range os.Environ() {
		k, _, _ := strings.Cut(x, "=")
		if keep[k] {
			out = append(out, x)
		}
	}
	out = append(out, "HOME="+home, "HERMES_HOME="+home, "HERMES_ENABLE_PROJECT_PLUGINS=false", "HERMES_YOLO_MODE=0", "HERMES_SKIP_VERSION_CHECK=1", "PYTHONDONTWRITEBYTECODE=1")
	for _, credential := range credentials {
		out = append(out, credential.TargetEnv+"="+credential.Value)
	}
	return out
}

func (a *Adapter) StartDesign(ctx context.Context, stateRoot string, retain bool) (string, string, int, error) {
	id := store.ID("hermes-design")
	runtimeRoot := filepath.Join(stateRoot, "runtime")
	if err := os.MkdirAll(runtimeRoot, 0700); err != nil {
		return "", "", 0, err
	}
	home, err := os.MkdirTemp(runtimeRoot, "design-")
	if err != nil {
		return "", "", 0, err
	}
	pid, _, err := a.launch(ctx, id, home, nil, "", "", nil, BrokerBridge{})
	if err != nil {
		if !retain {
			_ = os.RemoveAll(home)
		}
		return "", "", 0, err
	}
	return id, home, pid, nil
}

// RunDesignForeground runs the documented Hermes TUI attached to the caller's
// terminal. Safe mode plus no_mcp removes ambient config, rules, memory,
// plugins, MCP servers, and normal CLI toolsets. It never uses one-shot/YOLO.
func (a *Adapter) RunDesignForeground(ctx context.Context, stateRoot string, retain bool, in io.Reader, out, errOut io.Writer) (string, error) {
	runtimeRoot := filepath.Join(stateRoot, "runtime")
	if err := os.MkdirAll(runtimeRoot, 0700); err != nil {
		return "", err
	}
	home, err := os.MkdirTemp(runtimeRoot, "design-")
	if err != nil {
		return "", err
	}
	if !retain {
		defer os.RemoveAll(home)
	} //nolint:errcheck
	desc, err := a.Discover(ctx)
	if err != nil {
		return home, err
	}
	cmd := exec.CommandContext(ctx, desc.Executable, "--safe-mode", "--tui", "--toolsets", "no_mcp")
	cmd.Dir, cmd.Env, cmd.Stdin, cmd.Stdout, cmd.Stderr = home, minimalEnv(home, nil), in, out, errOut
	if err := cmd.Run(); err != nil {
		return home, fmt.Errorf("Hermes design session: %w", err)
	}
	return home, nil
}

func (a *Adapter) Launch(ctx context.Context, stateRoot string, m core.Mandate, credentials []Credential, bridge BrokerBridge) (string, string, int, []string, error) {
	id := store.ID("hermes-session")
	runtimeRoot := filepath.Join(stateRoot, "runtime")
	if err := os.MkdirAll(runtimeRoot, 0700); err != nil {
		return "", "", 0, nil, err
	}
	home, err := os.MkdirTemp(runtimeRoot, "stanza-"+m.StanzaID+"-")
	if err != nil {
		return "", "", 0, nil, err
	}
	pid, configuredToolsets, err := a.launch(ctx, id, home, m.Hermes.Toolsets, m.Hermes.Model, m.Hermes.Provider, credentials, bridge)
	if err != nil {
		_ = os.RemoveAll(home)
		return "", "", 0, nil, err
	}
	return id, home, pid, configuredToolsets, nil
}
func (a *Adapter) Alive(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	p := a.processes[id]
	if p == nil {
		return false
	}
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}
func (a *Adapter) Terminate(ctx context.Context, id string, remove bool) error {
	a.mu.Lock()
	p := a.processes[id]
	a.mu.Unlock()
	if p == nil {
		return nil
	}
	_ = p.stdin.Close()
	if p.cmd.Process != nil {
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGTERM)
	}
	select {
	case <-ctx.Done():
		if p.cmd.Process != nil {
			_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
		}
		return ctx.Err()
	case <-p.done:
	case <-time.After(5 * time.Second):
		if p.cmd.Process != nil {
			_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
		}
		select {
		case <-p.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if remove {
		if err := os.RemoveAll(p.home); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
