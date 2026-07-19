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
	closeOnce sync.Once
	closeErr  error
}

type HermesProcessConfig struct {
	Python              string
	Installation        string
	StateRoot           string
	ProxyEndpoint       string
	ProxyToken          string
	Model               string
	MaximumMessageBytes int
	StartTimeout        time.Duration
}

func StartHermesProcess(ctx context.Context, config HermesProcessConfig) (*HermesProcess, error) {
	if config.Python == "" || config.Installation == "" || config.ProxyEndpoint == "" || config.ProxyToken == "" || config.Model == "" {
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
	command := exec.Command(config.Python, "-m", "tui_gateway.entry")
	command.Dir = home
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Env = []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + home, "HERMES_HOME=" + home,
		"HERMES_SAFE_MODE=1", "HERMES_IGNORE_USER_CONFIG=1", "HERMES_IGNORE_RULES=1",
		"HERMES_PYTHON_SRC_ROOT=" + config.Installation, "HERMES_ENABLE_PROJECT_PLUGINS=false",
		"HERMES_DISABLE_AUTO_SKILLS=1", "HERMES_TUI_TOOLSETS=no_mcp", "HERMES_TUI_SKILLS=",
		"HERMES_SKIP_VERSION_CHECK=1", "HERMES_YOLO_MODE=0", "PYTHONDONTWRITEBYTECODE=1",
		"HERMES_MODEL=" + config.Model, "HERMES_TUI_PROVIDER=custom", "OPENAI_BASE_URL=" + config.ProxyEndpoint + "/v1",
		"OPENAI_API_KEY=" + config.ProxyToken, "HERMES_EPHEMERAL_SYSTEM_PROMPT=" + SystemInstruction,
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
	process := &HermesProcess{command: command, stdin: stdin, done: make(chan error, 1), home: home}
	go func() { _, _ = io.Copy(io.Discard, stderr) }()
	go func() { process.done <- command.Wait(); close(process.done) }()
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

func (p *HermesProcess) Client() *GatewayClient { return p.client }
func (p *HermesProcess) Done() <-chan error     { return p.done }
func (p *HermesProcess) Close(ctx context.Context) error {
	if p == nil {
		return nil
	}
	p.closeOnce.Do(func() {
		if p.stdin != nil {
			_ = p.stdin.Close()
		}
		if p.command != nil && p.command.Process != nil {
			_ = syscall.Kill(-p.command.Process.Pid, syscall.SIGTERM)
		}
		select {
		case <-p.done:
		case <-time.After(2 * time.Second):
			if p.command != nil && p.command.Process != nil {
				_ = syscall.Kill(-p.command.Process.Pid, syscall.SIGKILL)
			}
			select {
			case <-p.done:
			case <-ctx.Done():
				p.closeErr = ctx.Err()
			}
		case <-ctx.Done():
			if p.command != nil && p.command.Process != nil {
				_ = syscall.Kill(-p.command.Process.Pid, syscall.SIGKILL)
			}
			p.closeErr = ctx.Err()
		}
		if err := os.RemoveAll(p.home); err != nil {
			p.closeErr = errors.Join(p.closeErr, err)
		}
	})
	return p.closeErr
}
