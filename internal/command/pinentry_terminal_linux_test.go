//go:build linux

package command

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// This helper runs as session leader with a real controlling/foreground PTY,
// like an interactive SSH command. No gateway, principal state or service is used.
func TestProtectedPasswordPTYChild(t *testing.T) {
	mode := os.Getenv("AEGIS_TEST_PASSWORD_PTY")
	if mode == "" {
		return
	}
	expectedFile := os.NewFile(3, "expected")
	expected, err := io.ReadAll(expectedFile)
	expectedFile.Close()
	if err != nil {
		t.Fatal("fixture pipe failed")
	}
	defer wipeSecret(expected)
	service := newAuthorityPassphraseService(func() string { return "" })
	service.timeout = 3 * time.Second
	service.lookPath = func(string) (string, error) { t.Fatal("terminal route discovered helper"); return "", exec.ErrNotFound }
	service.command = func(context.Context, string, ...string) *exec.Cmd {
		t.Fatal("terminal route launched helper")
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if mode == "interrupt" {
		var stop context.CancelFunc
		ctx, stop = signal.NotifyContext(ctx, os.Interrupt)
		defer stop()
	}
	if mode == "timeout" {
		service.timeout = 200 * time.Millisecond
	}
	if mode == "cancel" {
		go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	}
	if strings.HasPrefix(mode, "explicit-") {
		scenario := strings.TrimPrefix(mode, "explicit-")
		service = testPassphraseService(t, func() string { return scenario }, func() string { return "unused-fixture-value" }, "")
		service.explicit = func() string { return os.Args[0] }
		if scenario == "hang" {
			service.timeout = 200 * time.Millisecond
		}
	}
	intent := PrincipalPasswordAuthenticate
	if mode == "create" {
		intent = PrincipalPasswordCreate
	}
	initial, err := unix.IoctlGetTermios(0, unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	value, err := service.Acquire(ctx, AuthorityPassphraseRequest{Intent: intent, Input: os.Stdin, Diagnostic: os.Stderr})
	defer wipeSecret(value)
	kind := PassphraseErrorKind("")
	if err != nil {
		var protected *PassphraseError
		if !errors.As(err, &protected) {
			t.Fatal("untyped protected failure")
		}
		kind = protected.Kind
		if strings.HasPrefix(mode, "explicit-") && protected.Stage != PinentryGetPIN {
			t.Fatal("explicit helper did not reach GETPIN")
		}
		if bytes.Contains([]byte(err.Error()), expected) {
			t.Fatal("secret in error")
		}
	}
	want := PassphraseErrorKind("")
	switch mode {
	case "cancel", "interrupt", "eof", "explicit-cancel":
		want = PassphraseCancelled
	case "timeout", "explicit-hang":
		want = PassphraseTimeout
	case "explicit-post-failure":
		want = PassphraseProtocol
	}
	if kind != want {
		t.Fatalf("kind=%s want=%s", kind, want)
	}
	if want == "" && !bytes.Equal(value, expected) {
		t.Fatal("protected roundtrip mismatch")
	}
	if want != "" && len(value) != 0 {
		t.Fatal("failure returned protected value")
	}
	final, err := unix.IoctlGetTermios(0, unix.TCGETS)
	if err != nil || *initial != *final {
		t.Fatal("terminal was not restored")
	}
	fmt.Println("PTY-RESULT-OK")
}

func TestProtectedPasswordForegroundPTY(t *testing.T) {
	for _, mode := range []string{"success", "create", "cancel", "interrupt", "eof", "timeout", "explicit-cancel", "explicit-hang", "explicit-post-failure"} {
		t.Run(mode, func(t *testing.T) {
			master, slave := openCommandPTY(t)
			defer master.Close()
			defer slave.Close()
			random := make([]byte, 24)
			if _, err := rand.Read(random); err != nil {
				t.Fatal(err)
			}
			expected := []byte(hex.EncodeToString(random))
			defer wipeSecret(expected)
			wipeSecret(random)
			readPipe, writePipe, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer readPipe.Close()
			defer writePipe.Close()
			child := exec.Command(os.Args[0], "-test.run=^TestProtectedPasswordPTYChild$", "-test.count=1")
			child.Env = append(os.Environ(), "AEGIS_TEST_PASSWORD_PTY="+mode, "DISPLAY=:unusable-ssh-display", "TERM=xterm")
			child.Stdin = slave
			child.Stdout = slave
			child.Stderr = slave
			child.ExtraFiles = []*os.File{readPipe}
			child.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
			if err = child.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = child.Process.Kill() }()
			readPipe.Close()
			_, err = writePipe.Write(expected)
			writePipe.Close()
			if err != nil {
				t.Fatal(err)
			}
			capture := []byte{}
			defer wipeSecret(capture)
			waitMarker := func(marker string) {
				deadline := time.Now().Add(15 * time.Second)
				for !bytes.Contains(capture, []byte(marker)) && time.Now().Before(deadline) {
					poll := []unix.PollFd{{Fd: int32(master.Fd()), Events: unix.POLLIN}}
					n, err := unix.Poll(poll, 50)
					if errors.Is(err, unix.EINTR) {
						continue
					}
					if err != nil {
						t.Fatal(err)
					}
					if n == 0 {
						continue
					}
					buffer := make([]byte, 4096)
					n, err = master.Read(buffer)
					if n > 0 {
						capture = append(capture, buffer[:n]...)
					}
					if bytes.Contains(capture, expected) {
						t.Fatal("protected value echoed on PTY")
					}
					if err != nil {
						break
					}
				}
				if !bytes.Contains(capture, []byte(marker)) {
					t.Fatal("PTY marker missing (capture withheld)")
				}
			}
			if !strings.HasPrefix(mode, "explicit-") {
				prompt := "Enrolled principal password: "
				if mode == "create" {
					prompt = "Principal password (minimum 12 bytes): "
				}
				waitMarker(prompt)
				state, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
				if err != nil || state.Lflag&unix.ECHO != 0 {
					t.Fatal("echo enabled at protected prompt")
				}
				if mode == "success" || mode == "create" {
					if _, err = master.Write(append(append([]byte{}, expected...), '\n')); err != nil {
						t.Fatal(err)
					}
					if mode == "create" {
						waitMarker("Confirm principal password: ")
						state, err = unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
						if err != nil || state.Lflag&unix.ECHO != 0 {
							t.Fatal("echo enabled at confirmation")
						}
						if _, err = master.Write(append(append([]byte{}, expected...), '\n')); err != nil {
							t.Fatal(err)
						}
					}
				} else if mode == "cancel" || mode == "timeout" {
					if _, err = master.Write(expected); err != nil {
						t.Fatal(err)
					}
				} else if mode == "eof" {
					_, _ = master.Write([]byte{4})
				} else if mode == "interrupt" {
					_, _ = master.Write([]byte{3})
				}
			}
			waitMarker("PTY-RESULT-OK")
			if err = child.Wait(); err != nil {
				t.Fatal("PTY child failed (capture withheld)")
			}
			if strings.HasPrefix(mode, "explicit-") && bytes.Contains(capture, []byte("Enrolled principal password: ")) {
				t.Fatal("second prompt after GETPIN")
			}
			if mode != "create" && bytes.Contains(capture, []byte("Confirm principal password: ")) {
				t.Fatal("unexpected confirmation")
			}
		})
	}
}

func TestProtectedPasswordRequiresTerminalPair(t *testing.T) {
	master, slave := openCommandPTY(t)
	defer master.Close()
	defer slave.Close()
	for _, request := range []AuthorityPassphraseRequest{
		{Intent: PrincipalPasswordAuthenticate, Input: slave, Diagnostic: io.Discard},
		{Intent: PrincipalPasswordAuthenticate, Input: strings.NewReader("not-a-terminal"), Diagnostic: slave},
	} {
		service := newAuthorityPassphraseService(func() string { return "" })
		service.lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
		value, err := service.Acquire(context.Background(), request)
		wipeSecret(value)
		if !IsPassphraseError(err, PassphraseUnavailable) {
			t.Fatal("non-terminal input accepted")
		}
	}
}
