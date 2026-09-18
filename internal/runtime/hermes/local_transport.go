package hermes

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/localinference"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// prepareLocal gates exec behind exact process custody; the public token is not authority.
func prepareLocal(ctx context.Context, command *exec.Cmd, h core.HermesConfig, admit func(context.Context) error) (func() error, func(), error) {
	p, e := localinference.NewWithContext(ctx, *h.LocalInference, h.Model, admit)
	if e != nil {
		return nil, nil, e
	}
	reader, writer, e := os.Pipe()
	if e != nil {
		p.Close()
		return nil, nil, e
	}
	original := append([]string{command.Path}, command.Args[1:]...)
	command.Path = "/bin/sh"
	command.Args = append([]string{"/bin/sh", "-c", `IFS= read -r release <&3 && [ "$release" = "release" ] && exec 3<&- && exec "$@"`, "aegis-runtime-gate"}, original...)
	command.ExtraFiles = []*os.File{reader}
	var env []string
	for _, v := range command.Env {
		k, _, _ := strings.Cut(v, "=")
		switch k {
		case "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "HERMES_TUI_MODEL", "HERMES_TUI_PROVIDER":
			continue
		}
		env = append(env, v)
	}
	command.Env = append(env, "HERMES_SAFE_MODE=1", "HERMES_IGNORE_USER_CONFIG=1", "HERMES_IGNORE_RULES=1", "HERMES_TUI_TOOLSETS=context_engine", "HERMES_TUI_SKILLS=", "HERMES_DISABLE_AUTO_SKILLS=1", "HERMES_MODEL="+h.Model, "HERMES_TUI_MODEL="+h.Model, "HERMES_TUI_PROVIDER=openrouter", "OPENROUTER_BASE_URL="+p.Endpoint()+"/v1", "OPENROUTER_API_KEY="+localinference.CompatibilityToken)
	cleanup := func() { reader.Close(); writer.Close(); p.Close() }
	release := func() error {
		reader.Close()
		if e := ctx.Err(); e != nil {
			return e
		}
		if e := admit(ctx); e != nil {
			return e
		}
		if e := p.Bind(command.Process.Pid); e != nil {
			return e
		}
		_, e := io.WriteString(writer, "release\n")
		writer.Close()
		return e
	}
	return release, cleanup, nil
}
func (a *Adapter) launchLocal(ctx context.Context, id, home string, authority core.AuthorityContext, admit func(context.Context) error) (int, error) {
	if admit == nil {
		return 0, errors.New("local inference transport requires fresh authority callback")
	}
	d, e := a.Discover(ctx)
	if e != nil {
		return 0, e
	}
	if d.Runtime != authority.Runtime.Runtime || d.Version != authority.Runtime.Version {
		return 0, errors.New("runtime binding does not match authority context")
	}
	h := authority.Authority.Hermes
	lifetime, lifetimeCancel := context.WithDeadline(ctx, authority.ExpiresAt)
	successLifetime := false
	defer func() {
		if !successLifetime {
			lifetimeCancel()
		}
	}()
	python := gatewayPython(d)
	if python == "" {
		return 0, errors.New("Hermes gateway unavailable")
	}
	cmd := exec.Command(python, "-m", "tui_gateway.entry")
	cmd.Dir = home
	cmd.Env = append(minimalEnv(home, nil), "HERMES_PYTHON_SRC_ROOT="+d.Installation)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	release, cleanup, e := prepareLocal(lifetime, cmd, h, admit)
	if e != nil {
		return 0, e
	}
	success := false
	defer func() {
		if !success {
			cleanup()
		}
	}()
	stdin, e := cmd.StdinPipe()
	if e != nil {
		return 0, e
	}
	stdout, e := cmd.StdoutPipe()
	if e != nil {
		return 0, e
	}
	stderr, e := cmd.StderrPipe()
	if e != nil {
		return 0, e
	}
	if e = cmd.Start(); e != nil {
		return 0, e
	}
	if e = release(); e != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return 0, e
	}
	ps := &processState{cmd: cmd, stdin: stdin, home: home, done: make(chan error, 1), cancel: lifetimeCancel}
	a.mu.Lock()
	a.processes[id] = ps
	a.mu.Unlock()
	go io.Copy(io.Discard, stderr)
	go func() {
		err := cmd.Wait()
		lifetimeCancel()
		cleanup()
		os.RemoveAll(home)
		a.mu.Lock()
		delete(a.processes, id)
		a.mu.Unlock()
		ps.done <- err
		close(ps.done)
	}()
	messages := make(chan gatewayMessage, 32)
	failures := make(chan error, 1)
	go readGateway(stdout, messages, failures)
	ready, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	_, e = waitGateway(ready, messages, failures, func(m gatewayMessage) bool { return m.Method == "event" && m.Params.Type == "gateway.ready" })
	if e != nil {
		cmd.Process.Kill()
		<-ps.done
		return 0, e
	}
	go func() {
		for {
			select {
			case <-messages:
			case <-failures:
				return
			}
		}
	}()
	success = true
	successLifetime = true
	return cmd.Process.Pid, nil
}
