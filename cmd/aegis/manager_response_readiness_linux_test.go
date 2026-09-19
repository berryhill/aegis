//go:build linux

package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/command"
	"golang.org/x/sys/unix"
)

func quitManagerAfterResponse(t *testing.T, master *os.File, capture []byte, promptCount int) []byte {
	t.Helper()
	capture = readPTYUntil(t, master, capture, "The local Aegis management model is unavailable (", 3*time.Second)
	// Response output is not input readiness: Read restores canonical mode
	// between turns, where an early CR becomes the rich editor's newline.
	capture = readPTYUntilCount(t, master, capture, "Enter submit; Ctrl+J newline", promptCount, 5*time.Second)
	if _, err := master.Write([]byte("/quit\r")); err != nil {
		t.Fatal(err)
	}
	return capture
}

// Hold the real command's response write after its visible marker, before the
// next Composer.Read. This models a descheduled writer without a slow model,
// production sleep, or changes to the process waiter's timeout.
type pausedManagerResponse struct {
	*os.File
	reached chan struct{}
	resume  chan struct{}
	once    sync.Once
}

func (output *pausedManagerResponse) release() { output.once.Do(func() { close(output.resume) }) }

func (output *pausedManagerResponse) Underlying() io.Writer { return output.File }
func (output *pausedManagerResponse) Write(data []byte) (int, error) {
	n, err := output.File.Write(data)
	if bytes.Contains(data, []byte("The local Aegis management model is unavailable (")) {
		close(output.reached)
		<-output.resume
	}
	return n, err
}

func TestManagerResponseSenderWaitsForComposerReadiness(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME"} {
		t.Setenv(name, filepath.Join(root, name))
	}
	t.Setenv("TERM", "xterm")
	t.Setenv("AEGIS_ACCESSIBLE", "0")
	t.Setenv("AEGIS_PLAIN", "0")
	configPath := lifecycleConfig(t, root+"/state")
	master, slave, initial := openManagerPTY(t)
	output := &pausedManagerResponse{File: slave, reached: make(chan struct{}), resume: make(chan struct{})}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := command.NewRoot(command.Dependencies{In: slave, Out: output, Err: slave, Version: "test", IsTerminal: func(io.Reader, io.Writer) bool { return true }})
	cmd.SetArgs([]string{"--config", configPath, "manager"})
	done := make(chan struct{})
	var commandErr error
	go func() {
		commandErr = cmd.ExecuteContext(ctx)
		close(done)
	}()
	// Always release the writer and join the command before closing the PTY.
	defer func() {
		cancel()
		output.release()
		select {
		case <-done:
			if commandErr != nil {
				t.Logf("manager returned: %v", commandErr)
			}
		case <-time.After(3 * time.Second):
			t.Error("manager failed to join after cancellation")
		}
	}()
	capture := readPTYUntil(t, master, nil, "Enter submit; Ctrl+J newline", 5*time.Second)
	if _, err := master.Write([]byte("//status\r")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-output.reached:
	case <-ctx.Done():
		t.Fatal("manager never reached response phase")
	}
	state, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil || state.Lflag&unix.ICANON == 0 || state.Iflag&unix.ICRNL == 0 {
		t.Fatalf("response phase must restore canonical CR-to-LF mode: state=%+v err=%v", state, err)
	}
	// Observe the sender while response completion is held. An early /quit CR
	// becomes LF here, so the next rich editor treats it as multiline input.
	type observation struct {
		ready int
		err   error
	}
	observed := make(chan observation, 1)
	go func() {
		poll := []unix.PollFd{{Fd: int32(slave.Fd()), Events: unix.POLLIN}}
		ready, err := unix.Poll(poll, 150)
		observed <- observation{ready, err}
		output.release()
	}()
	capture = quitManagerAfterResponse(t, master, capture, 2)
	result := <-observed
	if result.err != nil || result.ready != 0 {
		t.Fatalf("sender wrote exit before composer readiness (canonical CR becomes LF): ready=%d err=%v", result.ready, result.err)
	}
	capture = readPTYUntil(t, master, capture, "Aegis manager stopped; cleanup complete.", 3*time.Second)
	select {
	case <-done:
		if commandErr != nil {
			t.Fatalf("manager exit: %v", commandErr)
		}
	case <-ctx.Done():
		t.Fatal("manager did not exit after ready-state quit")
	}
	final, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil || *final != *initial {
		t.Fatalf("terminal not restored: err=%v initial=%+v final=%+v", err, initial, final)
	}
}
