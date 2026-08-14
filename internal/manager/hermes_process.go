package manager

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

type HermesProcess struct {
	command   *exec.Cmd
	stdin     io.WriteCloser
	client    *GatewayClient
	done      chan error
	home      string
	custody   *ProcessCustody
	closeOnce sync.Once
	closeErr  error
}

type HermesProcessConfig struct {
	Python              string
	Installation        string
	StateRoot           string
	ProxyEndpoint       string
	Model               string
	MaximumMessageBytes int
	StartTimeout        time.Duration
	// AuthorizeRelease must bind the exact pidfd-custodied process to its
	// inference route. Hermes remains behind a one-shot inherited pipe gate
	// until this durable authorization succeeds.
	AuthorizeRelease func(*ProcessCustody) error
}

func StartHermesProcess(ctx context.Context, config HermesProcessConfig) (*HermesProcess, error) {
	if config.Python == "" || config.Installation == "" || config.ProxyEndpoint == "" || config.Model == "" || config.AuthorizeRelease == nil {
		return nil, errors.New("Hermes manager process configuration is incomplete")
	}
	homeRoot := filepath.Join(config.StateRoot, "runtime")
	if err := os.MkdirAll(homeRoot, 0700); err != nil {
		return nil, err
	}
	home, err := os.MkdirTemp(homeRoot, "manager-hermes-")
	if err != nil {
		return nil, err
	}
	releaseReader, releaseWriter, err := os.Pipe()
	if err != nil {
		_ = os.RemoveAll(home)
		return nil, err
	}
	defer releaseReader.Close()
	defer releaseWriter.Close()
	// This process performs no runtime work: it blocks on an Aegis-owned pipe
	// and replaces itself with Hermes only after exact-process authorization.
	// EOF and malformed release input fail closed.
	command := exec.Command("/bin/sh", "-c", `IFS= read -r aegis_release <&3 && [ "$aegis_release" = "aegis-hermes-release-v1" ] && exec 3<&- && exec "$@"`, "aegis-hermes-gate", config.Python, "-m", "tui_gateway.entry")
	command.Dir = home
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.ExtraFiles = []*os.File{releaseReader}
	command.Env = []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + home, "HERMES_HOME=" + home,
		"HERMES_SAFE_MODE=1", "HERMES_IGNORE_USER_CONFIG=1", "HERMES_IGNORE_RULES=1",
		"HERMES_PYTHON_SRC_ROOT=" + config.Installation, "HERMES_ENABLE_PROJECT_PLUGINS=false",
		// context_engine is a real, empty Hermes 0.18 toolset. The no_mcp value is
		// only an MCP-selection sentinel; the TUI rejects it as an unknown toolset
		// and falls back to configured CLI tools, which widens the request.
		"HERMES_DISABLE_AUTO_SKILLS=1", "HERMES_TUI_TOOLSETS=context_engine", "HERMES_TUI_SKILLS=",
		"HERMES_SKIP_VERSION_CHECK=1", "HERMES_YOLO_MODE=0", "HERMES_MAX_TOKENS=192", "PYTHONDONTWRITEBYTECODE=1",
		"HERMES_MODEL=" + config.Model, "HERMES_TUI_PROVIDER=openrouter", "OPENROUTER_BASE_URL=" + config.ProxyEndpoint + "/v1",
		"OPENROUTER_API_KEY=" + HermesCompatibilityAPIKey, "HERMES_EPHEMERAL_SYSTEM_PROMPT=" + SystemInstruction,
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		_ = os.RemoveAll(home)
		return nil, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		_ = os.RemoveAll(home)
		return nil, err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		_ = os.RemoveAll(home)
		return nil, err
	}
	if err = command.Start(); err != nil {
		_ = os.RemoveAll(home)
		return nil, err
	}
	_ = releaseReader.Close()
	custody, err := AcquireProcessCustody(command.Process.Pid)
	if err != nil {
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		_ = command.Wait()
		_ = os.RemoveAll(home)
		return nil, err
	}
	abortBlocked := func(cause error) (*HermesProcess, error) {
		_ = releaseWriter.Close()
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		_ = command.Wait()
		_ = custody.Close()
		_ = os.RemoveAll(home)
		return nil, cause
	}
	if err = config.AuthorizeRelease(custody); err != nil {
		return abortBlocked(err)
	}
	if _, err = io.WriteString(releaseWriter, "aegis-hermes-release-v1\n"); err != nil {
		return abortBlocked(err)
	}
	if err = releaseWriter.Close(); err != nil {
		return abortBlocked(err)
	}
	process := &HermesProcess{command: command, stdin: stdin, done: make(chan error, 1), home: home, custody: custody}
	go func() { _, _ = io.Copy(io.Discard, stderr) }()
	go func() {
		waitErr := command.Wait()
		_ = custody.Close()
		process.done <- waitErr
		close(process.done)
	}()
	client, err := NewGatewayClient(stdout, stdin, config.MaximumMessageBytes)
	if err != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = process.Close(cleanup)
		cancel()
		return nil, err
	}
	process.client = client
	ready, cancel := context.WithTimeout(ctx, config.StartTimeout)
	defer cancel()
	if err = client.WaitReady(ready); err != nil {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = process.Close(cleanup)
		cleanupCancel()
		return nil, err
	}
	return process, nil
}

func (p *HermesProcess) Client() *GatewayClient   { return p.client }
func (p *HermesProcess) Done() <-chan error       { return p.done }
func (p *HermesProcess) Custody() *ProcessCustody { return p.custody }
func (p *HermesProcess) Close(ctx context.Context) error {
	if p == nil {
		return nil
	}
	p.closeOnce.Do(func() {
		if p.stdin != nil {
			_ = p.stdin.Close()
		}
		if p.command != nil && p.command.Process != nil {
			_ = p.custody.Signal(syscall.SIGTERM)
			_ = syscall.Kill(-p.command.Process.Pid, syscall.SIGTERM)
		}
		select {
		case <-p.done:
		case <-time.After(2 * time.Second):
			if p.command != nil && p.command.Process != nil {
				_ = p.custody.Signal(syscall.SIGKILL)
				_ = syscall.Kill(-p.command.Process.Pid, syscall.SIGKILL)
			}
			select {
			case <-p.done:
			case <-ctx.Done():
				p.closeErr = ctx.Err()
			}
		case <-ctx.Done():
			if p.command != nil && p.command.Process != nil {
				_ = p.custody.Signal(syscall.SIGKILL)
				_ = syscall.Kill(-p.command.Process.Pid, syscall.SIGKILL)
			}
			p.closeErr = ctx.Err()
		}
		if err := removeAllAndVerify(p.home); err != nil {
			p.closeErr = errors.Join(p.closeErr, err)
		}
	})
	return p.closeErr
}
