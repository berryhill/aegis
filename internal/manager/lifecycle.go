package manager

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

type ManagedOllama struct {
	command   *exec.Cmd
	done      chan error
	endpoint  string
	home      string
	closeOnce sync.Once
	closeErr  error
}

func StartManagedOllama(ctx context.Context, executable, stateRoot string, timeout time.Duration) (*ManagedOllama, error) {
	path, err := exec.LookPath(executable)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ReasonOllamaUnavailable, err)
	}
	home, err := os.MkdirTemp(filepath.Join(stateRoot, "runtime"), "ollama-")
	if err != nil {
		if os.IsNotExist(err) {
			if mkErr := os.MkdirAll(filepath.Join(stateRoot, "runtime"), 0700); mkErr == nil {
				home, err = os.MkdirTemp(filepath.Join(stateRoot, "runtime"), "ollama-")
			}
		}
		if err != nil {
			return nil, err
		}
	}
	modelStore := filepath.Join(stateRoot, "manager", "ollama-models")
	if err = os.MkdirAll(modelStore, 0700); err != nil {
		_ = os.RemoveAll(home)
		return nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = os.RemoveAll(home)
		return nil, err
	}
	endpoint := "http://" + listener.Addr().String()
	_ = listener.Close()
	command := exec.Command(path, "serve")
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "OLLAMA_HOST=" + endpoint[7:], "OLLAMA_MODELS=" + modelStore, "OLLAMA_NO_CLOUD=1"}
	command.Stdout, command.Stderr = io.Discard, io.Discard
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err = command.Start(); err != nil {
		_ = os.RemoveAll(home)
		return nil, err
	}
	managed := &ManagedOllama{command: command, done: make(chan error, 1), endpoint: endpoint, home: home}
	go func() { managed.done <- command.Wait(); close(managed.done) }()
	readyCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client, _ := NewOllamaClient(endpoint, time.Second)
	for {
		if _, err = client.Version(readyCtx); err == nil {
			return managed, nil
		}
		select {
		case waitErr := <-managed.done:
			_ = os.RemoveAll(home)
			return nil, fmt.Errorf("%s: process exited: %w", ReasonOllamaUnavailable, waitErr)
		case <-readyCtx.Done():
			cleanup, cleanupCancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = managed.Close(cleanup)
			cleanupCancel()
			return nil, fmt.Errorf("%s: readiness timeout", ReasonOllamaUnavailable)
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (m *ManagedOllama) Endpoint() string   { return m.endpoint }
func (m *ManagedOllama) Done() <-chan error { return m.done }
func (m *ManagedOllama) Close(ctx context.Context) error {
	if m == nil {
		return nil
	}
	m.closeOnce.Do(func() {
		if m.command != nil && m.command.Process != nil {
			_ = syscall.Kill(-m.command.Process.Pid, syscall.SIGTERM)
		}
		select {
		case <-m.done:
		case <-ctx.Done():
			if m.command != nil && m.command.Process != nil {
				_ = syscall.Kill(-m.command.Process.Pid, syscall.SIGKILL)
			}
			m.closeErr = ctx.Err()
		case <-time.After(2 * time.Second):
			if m.command != nil && m.command.Process != nil {
				_ = syscall.Kill(-m.command.Process.Pid, syscall.SIGKILL)
			}
			select {
			case <-m.done:
			case <-ctx.Done():
				m.closeErr = ctx.Err()
			}
		}
		if err := removeAllAndVerify(m.home); err != nil {
			m.closeErr = errors.Join(m.closeErr, err)
		}
	})
	return m.closeErr
}

func removeAllAndVerify(path string) error {
	if path == "" {
		return errors.New("disposable runtime state path is empty")
	}
	if err := os.RemoveAll(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		return errors.New("disposable runtime state still exists after removal")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
