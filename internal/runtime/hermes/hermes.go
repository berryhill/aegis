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
	"github.com/berryhill/aegis/internal/store"
)

var AdapterVersion = buildinfo.Version

var versionRE = regexp.MustCompile(`Hermes Agent v([0-9]+)\.([0-9]+)\.([0-9]+)[^\n]*`)
var supportedToolsets = map[string]bool{
	"web": true, "browser": true, "terminal": true, "file": true,
	"code_execution": true, "vision": true, "session_search": true,
	"skills": true, "todo": true, "memory": true, "clarify": true,
	"no_mcp": true,
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
func (a *Adapter) launch(ctx context.Context, id, home string, tools []string, model, provider string, credentials []Credential) (int, []string, error) {
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
	cmd.Dir = home
	cmd.Env = minimalEnv(home, credentials)
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
	// Runtime/model output is untrusted and may contain secrets. Drain it to
	// avoid pipe deadlock, but never copy it into Aegis logs or audit records.
	go func() { _, _ = io.Copy(io.Discard, stdout) }()
	go func() { _, _ = io.Copy(io.Discard, stderr) }()
	go func() { ps.done <- cmd.Wait(); close(ps.done); a.mu.Lock(); delete(a.processes, id); a.mu.Unlock() }()
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
func minimalEnv(home string, credentials []Credential) []string {
	keep := map[string]bool{"PATH": true, "LANG": true, "LC_ALL": true, "TERM": true, "SSL_CERT_FILE": true, "SSL_CERT_DIR": true, "HTTP_PROXY": true, "HTTPS_PROXY": true, "NO_PROXY": true}
	var out []string
	for _, x := range os.Environ() {
		k, _, _ := strings.Cut(x, "=")
		if keep[k] {
			out = append(out, x)
		}
	}
	out = append(out, "HERMES_HOME="+home, "HERMES_ENABLE_PROJECT_PLUGINS=false", "HERMES_YOLO_MODE=0", "HERMES_SKIP_VERSION_CHECK=1", "PYTHONDONTWRITEBYTECODE=1")
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
	pid, _, err := a.launch(ctx, id, home, nil, "", "", nil)
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

func (a *Adapter) Launch(ctx context.Context, stateRoot string, m core.Mandate, credentials []Credential) (string, string, int, []string, error) {
	id := store.ID("hermes-session")
	runtimeRoot := filepath.Join(stateRoot, "runtime")
	if err := os.MkdirAll(runtimeRoot, 0700); err != nil {
		return "", "", 0, nil, err
	}
	home, err := os.MkdirTemp(runtimeRoot, "stanza-"+m.StanzaID+"-")
	if err != nil {
		return "", "", 0, nil, err
	}
	pid, configuredToolsets, err := a.launch(ctx, id, home, m.Hermes.Toolsets, m.Hermes.Model, m.Hermes.Provider, credentials)
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
