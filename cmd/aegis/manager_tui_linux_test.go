//go:build linux

package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestManagerRichComposerPTYMultilineHistoryHelpPasteAndRestoration(t *testing.T) {
	root := t.TempDir()
	binary := root + "/aegis"
	buildTestBinary(t, binary)
	configPath := lifecycleConfig(t, root+"/state")
	process, master, slave, initial := startManagerPTY(t, binary, configPath)
	defer master.Close()
	defer slave.Close()
	capture := readPTYUntil(t, master, nil, "Enter submit; Ctrl+J newline", 5*time.Second)

	// Ctrl+J inserts a logical newline; carriage return submits exactly once.
	_, _ = master.Write([]byte("first\nsecond\r"))
	capture = readPTYUntil(t, master, capture, "The local Aegis management model is unavailable (", 3*time.Second)
	capture = readPTYUntilCount(t, master, capture, "Enter submit; Ctrl+J newline", 2, 3*time.Second)

	// Up recalls the complete multiline item and carriage return submits it.
	_, _ = master.Write([]byte("\x1b[A\r"))
	capture = readPTYUntilCount(t, master, capture, "The local Aegis management model is unavailable (", 2, 3*time.Second)
	capture = readPTYUntilCount(t, master, capture, "Enter submit; Ctrl+J newline", 3, 3*time.Second)

	// Resize is discovered on the production loop and reflows the authoritative header.
	if err := unix.IoctlSetWinsize(int(master.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Col: 40, Row: 12}); err != nil {
		t.Fatal(err)
	}
	_, _ = master.Write([]byte("/status\r"))
	capture = readPTYUntil(t, master, capture, "terminal resized to 40x12", 3*time.Second)
	capture = readPTYUntilCount(t, master, capture, "Enter submit; Ctrl+J newline", 4, 3*time.Second)

	// Empty-input help is local and state-aware.
	_, _ = master.Write([]byte("?\r"))
	capture = readPTYUntil(t, master, capture, "Credential metadata commands unavailable", 3*time.Second)
	capture = readPTYUntilCount(t, master, capture, "Enter submit; Ctrl+J newline", 5, 3*time.Second)

	// Bracketed paste remains one guarded submission and escape framing is absent.
	_, _ = master.Write([]byte("\x1b[200~pasted line one\npasted line two\x1b[201~\r"))
	capture = readPTYUntilCount(t, master, capture, "The local Aegis management model is unavailable (", 3, 3*time.Second)
	if strings.Contains(string(capture), "[200~") || strings.Contains(string(capture), "[201~") {
		t.Fatalf("paste framing leaked: %q", capture)
	}
	capture = readPTYUntilCount(t, master, capture, "Enter submit; Ctrl+J newline", 6, 3*time.Second)
	_, _ = master.Write([]byte("/quit\r"))
	if err := process.Wait(); err != nil {
		t.Fatalf("exit=%v output=%q", err, capture)
	}
	final, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil || final.Lflag&unix.ECHO != initial.Lflag&unix.ECHO || final.Lflag&unix.ICANON != initial.Lflag&unix.ICANON {
		t.Fatalf("terminal not restored: err=%v initial=%+v final=%+v", err, initial, final)
	}
}

func TestManagerAccessiblePlainPTYStatusHelpAndExit(t *testing.T) {
	t.Setenv("AEGIS_ACCESSIBLE", "1")
	root := t.TempDir()
	binary := root + "/aegis"
	buildTestBinary(t, binary)
	process, master, slave, initial := startManagerPTY(t, binary, lifecycleConfig(t, root+"/state"))
	defer master.Close()
	defer slave.Close()
	capture := readPTYUntil(t, master, nil, "[composer] > ", 5*time.Second)
	_, _ = master.Write([]byte("/status\n"))
	capture = readPTYUntil(t, master, capture, "Origin: AEGIS / authoritative", 3*time.Second)
	capture = readPTYUntilCount(t, master, capture, "[composer] > ", 2, 5*time.Second)
	if strings.Contains(string(capture), "\x1b[") {
		t.Fatalf("plain profile emitted cursor controls: %q", capture)
	}
	_, _ = master.Write([]byte("/quit\n"))
	if err := process.Wait(); err != nil {
		t.Fatalf("plain exit=%v output=%q", err, capture)
	}
	final, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil || final.Lflag&unix.ECHO != initial.Lflag&unix.ECHO {
		t.Fatalf("plain terminal not restored: %v", err)
	}
}

func TestManagerRandomCredentialCanaryPasteIsNotCapturedOrRemembered(t *testing.T) {
	root := t.TempDir()
	binary := root + "/aegis"
	buildTestBinary(t, binary)
	process, master, slave, _ := startManagerPTY(t, binary, lifecycleConfig(t, root+"/state"))
	defer master.Close()
	defer slave.Close()
	random := make([]byte, 24)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	canary := "sk-" + hex.EncodeToString(random)
	capture := readPTYUntil(t, master, nil, "Conversational local inference unavailable", 5*time.Second)
	capture = readPTYUntil(t, master, capture, "Enter submit; Ctrl+J newline", 5*time.Second)
	_, _ = master.Write([]byte("\x1b[200~" + canary + "\x1b[201~\r"))
	capture = readPTYUntil(t, master, capture, "possible credential blocked", 3*time.Second)
	if strings.Contains(string(capture), canary) {
		t.Fatal("credential canary entered terminal capture")
	}
	capture = readPTYUntilCount(t, master, capture, "Enter submit; Ctrl+J newline", 2, 5*time.Second)
	_, _ = master.Write([]byte("/status\r"))
	capture = readPTYUntil(t, master, capture, "Origin: AEGIS / authoritative", 3*time.Second)
	capture = readPTYUntilCount(t, master, capture, "Enter submit; Ctrl+J newline", 3, 5*time.Second)
	if strings.Contains(string(capture), canary) {
		t.Fatal("credential canary was retained in history")
	}
	_, _ = master.Write([]byte("/quit\r"))
	if err := process.Wait(); err != nil {
		t.Fatalf("exit=%v output=%q", err, capture)
	}
}

func TestManagerSlashRoutingConsumesUnknownMalformedAndLeadingWhitespaceLocally(t *testing.T) {
	root := t.TempDir()
	binary := root + "/aegis"
	buildTestBinary(t, binary)
	process, master, slave, _ := startManagerPTY(t, binary, lifecycleConfig(t, root+"/state"))
	defer master.Close()
	defer slave.Close()
	capture := readPTYUntil(t, master, nil, "Conversational local inference unavailable", 5*time.Second)
	capture = readPTYUntil(t, master, capture, "Enter submit; Ctrl+J newline", 5*time.Second)
	for index, test := range []struct{ input, marker string }{
		{"  /unknown\r", "unknown local slash command /unknown"},
		{"\t/status extra\r", "usage: /status"},
		{"/help a|b\r", "shell operator"},
	} {
		before := strings.Count(string(capture), "The local Aegis management model is unavailable (")
		_, _ = master.Write([]byte(test.input))
		capture = readPTYUntil(t, master, capture, test.marker, 3*time.Second)
		if strings.Count(string(capture), "The local Aegis management model is unavailable (") != before {
			t.Fatalf("local slash input reached ordinary/model path: %q", test.input)
		}
		capture = readPTYUntilCount(t, master, capture, "Enter submit; Ctrl+J newline", index+2, 5*time.Second)
	}
	_, _ = master.Write([]byte("//status\r"))
	capture = readPTYUntil(t, master, capture, "The local Aegis management model is unavailable (", 3*time.Second)
	_, _ = master.Write([]byte("/quit\r"))
	if err := process.Wait(); err != nil {
		t.Fatalf("exit=%v output=%q", err, capture)
	}
}

func TestManagerCore15ProductionPathInIsolatedDegradedSession(t *testing.T) {
	root := t.TempDir()
	binary := root + "/aegis"
	buildTestBinary(t, binary)
	process, master, slave, _ := startManagerPTY(t, binary, lifecycleConfig(t, root+"/state"))
	defer master.Close()
	defer slave.Close()
	capture := readPTYUntil(t, master, nil, "Conversational local inference unavailable", 5*time.Second)
	capture = readPTYUntil(t, master, capture, "Enter submit; Ctrl+J newline", 5*time.Second)
	commands := []string{"help", "status", "context", "authority", "limits", "scan", "watch", "findings", "investigate", "timeline", "report", "audit", "cancel", "clear"}
	for index, name := range commands {
		_, _ = master.Write([]byte("/" + name + "\r"))
		capture = readPTYUntil(t, master, capture, "\"operation\": \"manager."+name+"\"", 5*time.Second)
		capture = readPTYUntilCount(t, master, capture, "Enter submit; Ctrl+J newline", index+2, 8*time.Second)
	}
	_, _ = master.Write([]byte("/exit\r"))
	capture = readPTYUntil(t, master, capture, "Aegis manager stopped; cleanup complete.", 5*time.Second)
	if err := process.Wait(); err != nil {
		t.Fatalf("exit=%v output=%q", err, capture)
	}
	lowerCapture := strings.ToLower(string(capture))
	if !strings.Contains(string(capture), "missing_Aegis-owned_event_source_manager") || !strings.Contains(lowerCapture, "no findings") && !strings.Contains(lowerCapture, "degraded") {
		t.Fatalf("truthful watch/scan boundaries absent: %q", capture)
	}
}

func buildTestBinary(t *testing.T, path string) {
	t.Helper()
	command := exec.Command("go", "build", "-ldflags=-X=github.com/berryhill/aegis/internal/buildinfo.Version=test", "-o", path, ".")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
}

func readPTYUntilCount(t *testing.T, master *os.File, initial []byte, marker string, count int, timeout time.Duration) []byte {
	t.Helper()
	capture := bytes.NewBuffer(append([]byte(nil), initial...))
	deadline := time.Now().Add(timeout)
	poll := []unix.PollFd{{Fd: int32(master.Fd()), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR}}
	for strings.Count(capture.String(), marker) < count {
		if time.Now().After(deadline) {
			t.Fatalf("PTY timeout waiting for count %d of %q output=%q", count, marker, capture.String())
		}
		ready, err := unix.Poll(poll, 50)
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
		n, readErr := master.Read(buffer)
		if n > 0 {
			capture.Write(buffer[:n])
		}
		if readErr != nil {
			t.Fatalf("PTY read: %v output=%q", readErr, capture.String())
		}
	}
	return capture.Bytes()
}
