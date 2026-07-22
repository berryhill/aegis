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
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/tui"
	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
)

func openCommandPTY(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	masterFD, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = unix.IoctlSetPointerInt(masterFD, unix.TIOCSPTLCK, 0); err != nil {
		_ = unix.Close(masterFD)
		t.Fatal(err)
	}
	number, err := unix.IoctlGetInt(masterFD, unix.TIOCGPTN)
	if err != nil {
		_ = unix.Close(masterFD)
		t.Fatal(err)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		_ = unix.Close(masterFD)
		t.Fatal(err)
	}
	return os.NewFile(uintptr(masterFD), "ptmx"), slave
}

func readCommandPTYUntil(t *testing.T, master *os.File, capture *bytes.Buffer, marker string, timeout time.Duration) {
	t.Helper()
	buffer := make([]byte, 1024)
	poll := []unix.PollFd{{Fd: int32(master.Fd()), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR}}
	deadline := time.Now().Add(timeout)
	for !strings.Contains(capture.String(), marker) && time.Now().Before(deadline) {
		ready, err := unix.Poll(poll, 50)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			t.Fatal(err)
		}
		if ready == 0 {
			continue
		}
		count, _ := master.Read(buffer)
		capture.Write(buffer[:count])
	}
	if !strings.Contains(capture.String(), marker) {
		t.Fatalf("PTY output did not contain %q: %q", marker, capture.String())
	}
}

func assertCommandPTYRestored(t *testing.T, slave *os.File, initial *unix.Termios) {
	t.Helper()
	final, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	if final.Lflag != initial.Lflag {
		t.Fatalf("terminal flags not restored: before=%#x after=%#x", initial.Lflag, final.Lflag)
	}
}

func TestProtectedManagerIntakeDoesNotEchoOnPTY(t *testing.T) {
	master, slave := openCommandPTY(t)
	defer master.Close()
	defer slave.Close()
	initial, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	cmd := &cobra.Command{}
	cmd.SetIn(slave)
	cmd.SetOut(slave)
	cmd.SetErr(slave)
	random := make([]byte, 24)
	_, _ = rand.Read(random)
	canary := "pty-canary-" + hex.EncodeToString(random)
	result := make(chan []byte, 1)
	failures := make(chan error, 1)
	go func() {
		value, readErr := readSecret(cmd, false, "Secret value: ", "Confirm secret value: ")
		if readErr != nil {
			failures <- readErr
			return
		}
		result <- value
	}()
	var capture bytes.Buffer
	readCommandPTYUntil(t, master, &capture, "Secret value: ", 3*time.Second)
	_, _ = master.Write([]byte(canary + "\n"))
	readCommandPTYUntil(t, master, &capture, "Confirm secret value: ", 3*time.Second)
	if strings.Contains(capture.String(), canary) {
		t.Fatal("first protected value was echoed")
	}
	_, _ = master.Write([]byte(canary + "\n"))
	select {
	case err = <-failures:
		t.Fatal(err)
	case value := <-result:
		if string(value) != canary {
			t.Fatal("protected intake changed the value")
		}
		wipeSecret(value)
	case <-time.After(3 * time.Second):
		t.Fatal("protected intake timed out")
	}
	if strings.Contains(capture.String(), canary) {
		t.Fatal("protected value appeared in PTY capture")
	}
	assertCommandPTYRestored(t, slave, initial)
}

func TestProtectedManagerIntakeAcceptsBracketedMultilinePasteWithoutLeakingTrailingInput(t *testing.T) {
	master, slave := openCommandPTY(t)
	defer master.Close()
	defer slave.Close()
	initial, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	cmd := &cobra.Command{}
	cmd.SetIn(slave)
	cmd.SetOut(slave)
	cmd.SetErr(slave)
	random := make([]byte, 24)
	_, _ = rand.Read(random)
	canary := "aws_secret_access_key=" + hex.EncodeToString(random)
	value := []byte("aws_access_key_id=synthetic\n" + canary + "\nregion=us-east-1")
	framed := append([]byte("\x1b[200~"), value...)
	framed = append(framed, []byte("\x1b[201~\n")...)
	result := make(chan []byte, 1)
	failures := make(chan error, 1)
	go func() {
		secret, readErr := readSecret(cmd, false, "Secret value: ", "Confirm secret value: ")
		if readErr != nil {
			failures <- readErr
			return
		}
		result <- secret
	}()
	var capture bytes.Buffer
	readCommandPTYUntil(t, master, &capture, "Secret value: ", 3*time.Second)
	_, _ = master.Write(framed)
	readCommandPTYUntil(t, master, &capture, "Confirm secret value: ", 3*time.Second)
	_, _ = master.Write(framed)
	select {
	case err = <-failures:
		t.Fatal(err)
	case secret := <-result:
		if !bytes.Equal(secret, value) {
			t.Fatalf("multiline protected intake changed value: got %d bytes want %d", len(secret), len(value))
		}
		wipeSecret(secret)
	case <-time.After(3 * time.Second):
		t.Fatal("multiline protected intake timed out")
	}
	if strings.Contains(capture.String(), canary) {
		t.Fatal("multiline protected value appeared in PTY capture")
	}
	_, _ = master.Write([]byte("next-command\n"))
	next, eof, err := newTerminalInput(slave).ReadLine(context.Background(), 128)
	if err != nil || eof || next != "next-command" {
		t.Fatalf("protected paste leaked trailing input: next=%q eof=%v err=%v", next, eof, err)
	}
	assertCommandPTYRestored(t, slave, initial)
}

func TestProtectedManagerIntakeMismatchRestoresPTY(t *testing.T) {
	master, slave := openCommandPTY(t)
	defer master.Close()
	defer slave.Close()
	initial, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	cmd := &cobra.Command{}
	cmd.SetIn(slave)
	cmd.SetOut(slave)
	cmd.SetErr(slave)
	result := make(chan error, 1)
	go func() {
		_, readErr := readSecretContext(context.Background(), cmd, false, "Secret value: ", "Confirm secret value: ")
		result <- readErr
	}()
	var capture bytes.Buffer
	readCommandPTYUntil(t, master, &capture, "Secret value: ", 2*time.Second)
	_, _ = master.Write([]byte("first-random-value\n"))
	readCommandPTYUntil(t, master, &capture, "Confirm secret value: ", 2*time.Second)
	random := make([]byte, 24)
	_, _ = rand.Read(random)
	queuedCanary := "queued-canary-" + hex.EncodeToString(random)
	_, _ = master.Write([]byte("different-random-value\n" + queuedCanary + "\n"))
	select {
	case err = <-result:
		if err == nil || !strings.Contains(err.Error(), "does not match") {
			t.Fatalf("mismatch result=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("mismatch intake remained blocked")
	}
	if strings.Contains(capture.String(), queuedCanary) {
		t.Fatal("queued credential content appeared in PTY capture")
	}
	_, _ = master.Write([]byte("next-command\n"))
	next, eof, err := newTerminalInput(slave).ReadLine(context.Background(), 128)
	if err != nil || eof || next != "next-command" {
		t.Fatalf("mismatch left protected input queued: next=%q eof=%v err=%v", next, eof, err)
	}
	assertCommandPTYRestored(t, slave, initial)
}

func TestProtectedManagerIntakeCancellationRestoresPTYAndDiscardsCanary(t *testing.T) {
	for _, stage := range []string{"first", "confirmation"} {
		t.Run(stage, func(t *testing.T) {
			master, slave := openCommandPTY(t)
			defer master.Close()
			defer slave.Close()
			initial, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
			if err != nil {
				t.Fatal(err)
			}
			cmd := &cobra.Command{}
			cmd.SetIn(slave)
			cmd.SetOut(slave)
			var output bytes.Buffer
			cmd.SetErr(io.MultiWriter(slave, &output))
			ctx, cancel := context.WithCancel(context.Background())
			result := make(chan error, 1)
			go func() {
				_, readErr := readSecretContext(ctx, cmd, false, "Secret value: ", "Confirm secret value: ")
				result <- readErr
			}()
			var capture bytes.Buffer
			readCommandPTYUntil(t, master, &capture, "Secret value: ", 2*time.Second)
			if stage == "confirmation" {
				_, _ = master.Write([]byte("temporary-first-value\n"))
				readCommandPTYUntil(t, master, &capture, "Confirm secret value: ", 2*time.Second)
			}
			random := make([]byte, 24)
			_, _ = rand.Read(random)
			canary := "cancel-canary-" + hex.EncodeToString(random)
			_, _ = master.Write([]byte(canary))
			cancel()
			select {
			case readErr := <-result:
				if !errors.Is(readErr, context.Canceled) {
					t.Fatalf("cancel result=%v", readErr)
				}
			case <-time.After(time.Second):
				t.Fatal("protected intake did not cancel within one second")
			}
			assertCommandPTYRestored(t, slave, initial)
			for label, value := range map[string]string{"PTY": capture.String(), "command output": output.String(), "error": context.Canceled.Error()} {
				if strings.Contains(value, canary) {
					t.Fatalf("canceled canary appeared in %s", label)
				}
			}
		})
	}
}

func TestProtectedManagerIntakeOversizeAndEOFAreBounded(t *testing.T) {
	t.Run("oversized", func(t *testing.T) {
		master, slave := openCommandPTY(t)
		defer master.Close()
		defer slave.Close()
		initial, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
		if err != nil {
			t.Fatal(err)
		}
		cmd := &cobra.Command{}
		cmd.SetIn(slave)
		cmd.SetOut(slave)
		cmd.SetErr(slave)
		result := make(chan error, 1)
		go func() {
			_, readErr := readTerminalSecretBounded(context.Background(), slave, cmd.ErrOrStderr(), "Secret value: ", 32)
			result <- readErr
		}()
		var capture bytes.Buffer
		readCommandPTYUntil(t, master, &capture, "Secret value: ", 2*time.Second)
		canary := append(bytes.Repeat([]byte{'Z'}, 33), '\n')
		writeDone := make(chan error, 1)
		go func() {
			for len(canary) > 0 {
				count, writeErr := master.Write(canary)
				if writeErr != nil {
					writeDone <- writeErr
					return
				}
				canary = canary[count:]
			}
			writeDone <- nil
		}()
		select {
		case err = <-result:
			if err == nil || !strings.Contains(err.Error(), "configured limit") {
				t.Fatalf("oversized result=%v", err)
			}
		case <-time.After(8 * time.Second):
			t.Fatal("oversized protected intake did not finish")
		}
		select {
		case err = <-writeDone:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("oversized PTY writer remained blocked")
		}
		if bytes.Contains(capture.Bytes(), bytes.Repeat([]byte{'Z'}, 16)) {
			t.Fatal("oversized protected value echoed")
		}
		assertCommandPTYRestored(t, slave, initial)
	})

	t.Run("eof", func(t *testing.T) {
		master, slave := openCommandPTY(t)
		initial, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
		if err != nil {
			t.Fatal(err)
		}
		cmd := &cobra.Command{}
		cmd.SetIn(slave)
		cmd.SetOut(slave)
		cmd.SetErr(slave)
		result := make(chan error, 1)
		go func() {
			_, readErr := readSecretContext(context.Background(), cmd, false, "Secret value: ", "Confirm secret value: ")
			result <- readErr
		}()
		var capture bytes.Buffer
		readCommandPTYUntil(t, master, &capture, "Secret value: ", 2*time.Second)
		_ = master.Close()
		select {
		case err = <-result:
			if err == nil {
				t.Fatal("protected EOF unexpectedly succeeded")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("protected EOF did not unblock intake")
		}
		final, stateErr := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
		_ = slave.Close()
		if stateErr == nil && final.Lflag != initial.Lflag {
			t.Fatalf("terminal state changed after EOF: before=%#x after=%#x", initial.Lflag, final.Lflag)
		}
	})
}

func TestAuthorityFallbackAcceptsSynchronizedPresentationOutput(t *testing.T) {
	master, slave := openCommandPTY(t)
	defer master.Close()
	defer slave.Close()
	initial, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	service := newAuthorityPassphraseService(func() string { return "" })
	service.lookPath = func(string) (string, error) { return "", os.ErrNotExist }
	cmd := &cobra.Command{}
	cmd.SetContext(context.WithValue(context.Background(), authorityPassphraseContextKey{}, AuthorityPassphraseProvider(service)))
	cmd.SetIn(slave)
	cmd.SetOut(tui.NewSynchronizedWriter(slave))
	cmd.SetErr(slave)
	result := make(chan []byte, 1)
	failures := make(chan error, 1)
	go func() {
		value, readErr := readAuthorityPassphrase(cmd, false)
		if readErr != nil {
			failures <- readErr
			return
		}
		result <- value
	}()
	var capture bytes.Buffer
	readCommandPTYUntil(t, master, &capture, "Authority passphrase", 2*time.Second)
	canary := "synchronized-fallback-canary"
	_, _ = master.Write([]byte(canary + "\n"))
	select {
	case readErr := <-failures:
		t.Fatal(readErr)
	case value := <-result:
		if string(value) != canary {
			t.Fatal("fallback value mismatch")
		}
		wipeSecret(value)
	case <-time.After(2 * time.Second):
		t.Fatal("fallback remained blocked")
	}
	if strings.Contains(capture.String(), canary) {
		t.Fatal("fallback passphrase was echoed")
	}
	assertCommandPTYRestored(t, slave, initial)
}
