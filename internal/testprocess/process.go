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
	if command == nil || len(command.Args) == 0 || timeout <= 0 {
		return nil, errors.New("test process requires a command and positive timeout")
	}
	if command.Err != nil {
		return nil, command.Err
	}
	if command.Process != nil || command.ProcessState != nil || command.Cancel != nil ||
		command.SysProcAttr != nil || command.Stdout != nil || command.Stderr != nil || command.WaitDelay != 0 {
		return nil, errors.New("test process unsupported command settings: use an unstarted exec.Command without context, process attributes, output writers or WaitDelay")
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
	cleanup(bounded)
	return output.data.Bytes(), err
}
