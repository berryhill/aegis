// Package testprocess bounds subprocesses owned by integration fixtures.
package testprocess

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const MaxOutput = 4 << 20

// OwnedContextState is the helper-owned context state observed when Run returns.
// It does not prove exclusive causation of a process failure (for example, a
// deadline may race with an unrelated OS kill).
type OwnedContextState uint8

const (
	OwnedContextUnobserved OwnedContextState = iota
	OwnedContextLive
	OwnedContextDeadlineExceeded
	OwnedContextCanceled
)

// ObservedResult retains the private output and unchanged run error separately
// from closed, non-sensitive context metadata. Output and Err are not public
// diagnostics; callers must continue to withhold/sanitize them.
type ObservedResult struct {
	Output       []byte
	Err          error
	OwnedContext OwnedContextState
}

func observedResult(ctx context.Context, output []byte, err error) ObservedResult {
	result := ObservedResult{Output: output, Err: err}
	if ctx != nil {
		switch ctx.Err() {
		case nil:
			result.OwnedContext = OwnedContextLive
		case context.DeadlineExceeded:
			result.OwnedContext = OwnedContextDeadlineExceeded
		case context.Canceled:
			result.OwnedContext = OwnedContextCanceled
		}
	}
	return result
}

func conservativeEnv(environment []string) []string {
	if environment == nil {
		environment = os.Environ()
	}
	values := map[string]string{}
	for _, entry := range environment {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	if values["GOMAXPROCS"] == "" {
		values["GOMAXPROCS"] = "2"
	}
	if values["GOMEMLIMIT"] == "" {
		values["GOMEMLIMIT"] = "512MiB"
	}
	flags := values["GOFLAGS"]
	explicit := false
	for _, flag := range strings.Fields(flags) {
		if flag == "-p" || strings.HasPrefix(flag, "-p=") {
			explicit = true
		}
	}
	if !explicit {
		values["GOFLAGS"] = strings.TrimSpace(flags + " -p=1")
	}
	result := make([]string, 0, len(values))
	for key, value := range values {
		result = append(result, key+"="+value)
	}
	return result
}

type boundedOutput struct {
	mu   sync.Mutex
	data bytes.Buffer
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.data.Len()+len(p) > MaxOutput {
		return 0, errors.New("test process output limit exceeded")
	}
	return b.data.Write(p)
}

// CombinedOutput runs an unstarted exec.Command with a helper-owned deadline,
// output capture and process group. It preserves Path, Args (including argv[0]),
// Dir, Stdin and ExtraFiles; Env gains conservative Go defaults. CommandContext,
// custom cancellation/process attributes, output writers and WaitDelay are not
// supported and are rejected rather than silently discarded. The caller's Cmd
// is a specification only: its Process/ProcessState are not populated.
func CombinedOutput(command *exec.Cmd, timeout time.Duration) ([]byte, error) {
	result := CombinedOutputObserved(command, timeout)
	return result.Output, result.Err
}

// CombinedOutputObserved runs with the same contract as CombinedOutput and also
// snapshots the helper-owned context immediately after Run, before cancellation
// or cleanup. Cleanup still completes before the result is returned.
func CombinedOutputObserved(command *exec.Cmd, timeout time.Duration) ObservedResult {
	if command == nil || len(command.Args) == 0 || timeout <= 0 {
		return observedResult(nil, nil, errors.New("test process requires a command and positive timeout"))
	}
	if command.Err != nil {
		return observedResult(nil, nil, command.Err)
	}
	if command.Process != nil || command.ProcessState != nil || command.Cancel != nil ||
		command.SysProcAttr != nil || command.Stdout != nil || command.Stderr != nil || command.WaitDelay != 0 {
		return observedResult(nil, nil, errors.New("test process unsupported command settings: use an unstarted exec.Command without context, process attributes, output writers or WaitDelay"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	bounded := exec.CommandContext(ctx, command.Path, command.Args[1:]...)
	bounded.Env, bounded.Dir, bounded.Stdin = command.Env, command.Dir, command.Stdin
	bounded.Env = conservativeEnv(bounded.Env)
	bounded.Args = append([]string(nil), command.Args...)
	bounded.ExtraFiles = append([]*os.File(nil), command.ExtraFiles...)
	own(bounded)
	bounded.WaitDelay = 2 * time.Second
	var output boundedOutput
	bounded.Stdout, bounded.Stderr = &output, &output
	err := bounded.Run()
	result := observedResult(ctx, output.data.Bytes(), err)
	cleanup(bounded)
	return result
}
