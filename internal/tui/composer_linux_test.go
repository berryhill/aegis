//go:build linux

package tui

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type composerResult struct {
	line string
	eof  bool
	err  error
}

func TestRichComposerEnablesBracketedPasteAndSubmitsMultilineOnce(t *testing.T) {
	master, slave := openComposerPTY(t)
	defer master.Close()
	defer slave.Close()

	composer := NewComposer(slave, slave, 4096)
	result := make(chan composerResult, 1)
	go func() {
		line, eof, err := composer.Read(context.Background(), "> ", Capabilities{Profile: RichInteractive})
		result <- composerResult{line: line, eof: eof, err: err}
	}()

	readComposerPTYUntil(t, master, bracketedPasteEnable, 2*time.Second)
	if _, err := master.Write([]byte("\x1b[200~first line\r\nsecond line\x1b[201~\r")); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-result:
		if got.err != nil || got.eof || got.line != "first line\nsecond line" {
			t.Fatalf("line=%q eof=%v err=%v", got.line, got.eof, got.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("multiline bracketed paste was not submitted once")
	}
	readComposerPTYUntil(t, master, bracketedPasteDisable, 2*time.Second)
}

func openComposerPTY(t *testing.T) (*os.File, *os.File) {
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

func readComposerPTYUntil(t *testing.T, master *os.File, marker string, timeout time.Duration) string {
	t.Helper()
	var capture bytes.Buffer
	deadline := time.Now().Add(timeout)
	poll := []unix.PollFd{{Fd: int32(master.Fd()), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR}}
	for !strings.Contains(capture.String(), marker) {
		if time.Now().After(deadline) {
			t.Fatalf("PTY timeout waiting for %q output=%q", marker, capture.String())
		}
		ready, err := unix.Poll(poll, 25)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if ready == 0 {
			continue
		}
		buffer := make([]byte, 1024)
		count, readErr := master.Read(buffer)
		if count > 0 {
			capture.Write(buffer[:count])
		}
		if readErr != nil {
			t.Fatalf("PTY read waiting for %q: %v output=%q", marker, readErr, capture.String())
		}
	}
	return capture.String()
}
