//go:build linux

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/principalauth"
	"golang.org/x/sys/unix"
)

// Exercise the documented absent-config enrollment, custody decline, and
// foreground console path. Copied configuration is deliberately not enrollment.
func TestQuickstartPrincipalOnlyInitializationCanServe(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "aegis")
	buildTestBinary(t, binary)
	configPath := filepath.Join(root, "installation", "aegis.yaml")
	statePath := filepath.Join(root, "installation", "state")
	secret := make([]byte, 24)
	if _, err := rand.Read(secret); err != nil {
		t.Fatal(err)
	}
	password := hex.EncodeToString(secret)
	process, master, slave := startAuthorityPTY(t, binary, configPath, statePath, "", "")
	defer master.Close()
	defer slave.Close()
	exited := make(chan struct{})
	var initErr error
	go func() { initErr = process.Wait(); close(exited) }()
	defer func() { _ = process.Process.Kill(); <-exited }()
	capture := readQuickstartProtectedPTY(t, master, nil, "Principal password (minimum 12 bytes):", 5*time.Second)
	_, _ = master.Write([]byte(password + "\r"))
	capture = readQuickstartProtectedPTY(t, master, capture, "Confirm principal password:", 5*time.Second)
	_, _ = master.Write([]byte(password + "\r"))
	capture = readQuickstartProtectedPTY(t, master, capture, "Approve? [Y/n/details/basic/advanced]:", 5*time.Second)
	_, _ = master.Write([]byte("yes\r"))
	capture = readQuickstartProtectedPTY(t, master, capture, "Choose custody [Y=gateway-ready/n=exit/advanced]:", 10*time.Second)
	_, _ = master.Write([]byte("n\r"))
	capture = readQuickstartProtectedPTY(t, master, capture, "Custody setup declined", 10*time.Second)
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("principal-only initialization did not exit after custody decline")
	}
	if initErr != nil {
		t.Fatalf("principal-only initialization exit: %v", initErr)
	}
	if bytes.Contains(capture, []byte(password)) {
		t.Fatal("password echoed")
	}
	for _, name := range []string{"authority.db", "authority.kek", "authority.kek.enc"} {
		if _, err := os.Stat(filepath.Join(statePath, "credentials", name)); !os.IsNotExist(err) {
			t.Fatal("custody decline created credential artifact")
		}
	}
	assertCanaryAbsentBelow(t, filepath.Dir(configPath), password)

	// Honor the same validated caller root as release-readiness, including when
	// TMPDIR is a long task-owned path. Never shorten a path with a symlink.
	socketRoot := os.Getenv("AEGIS_PROOF_SOCKET_DIR")
	if socketRoot == "" {
		socketRoot = os.TempDir()
	}
	canonicalRoot, err := filepath.EvalSymlinks(socketRoot)
	if err != nil || !filepath.IsAbs(socketRoot) || canonicalRoot != socketRoot {
		t.Fatal("proof socket root must be an existing absolute canonical directory")
	}
	socketDir, err := os.MkdirTemp(socketRoot, "aq-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(socketDir)
	if len(filepath.Join(socketDir, "console.sock")) >= 108 {
		t.Fatal("proof socket path too long; set AEGIS_PROOF_SOCKET_DIR to a short canonical directory")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	origin := "http://" + address
	environment := []string{"HOME=" + filepath.Join(root, "home"), "PATH=" + buildFakeRuntimePrerequisites(t, root), "AEGIS_API_LISTEN=" + address, "AEGIS_API_CONSOLE_ORIGIN=" + origin, "AEGIS_API_UNIX_SOCKET=" + filepath.Join(socketDir, "console.sock")}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	server := exec.CommandContext(ctx, binary, "--config", configPath, "serve")
	server.Env = environment
	var diagnostics bytes.Buffer
	server.Stdout, server.Stderr = &diagnostics, &diagnostics
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = server.Process.Kill(); _ = server.Wait() }()
	client := &http.Client{Timeout: time.Second}
	ready := false
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); {
		response, err := client.Get(origin + "/console")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				ready = true
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		server.Process.Kill()
		server.Wait()
		t.Fatalf("foreground console unavailable: %s", diagnostics.String())
	}
	loginClient := &http.Client{Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, valid := range []bool{false, true} {
		value := password + "wrong"
		if valid {
			value = password
		}
		request, err := http.NewRequest(http.MethodPost, origin+"/console/login", strings.NewReader(url.Values{"password": {value}}.Encode()))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Origin", origin)
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response, err := loginClient.Do(request)
		if err != nil {
			t.Fatal("password login transport failed")
		}
		response.Body.Close()
		if valid {
			if response.StatusCode != http.StatusSeeOther || len(response.Cookies()) == 0 {
				t.Fatal("enrolled password did not establish a browser session")
			}
		} else if response.StatusCode < 400 || len(response.Cookies()) != 0 {
			t.Fatal("incorrect password established a browser session")
		}
	}
	discovery := exec.CommandContext(ctx, binary, "--config", configPath, "console")
	discovery.Env = environment
	output, err := discovery.CombinedOutput()
	if err != nil || !bytes.Contains(output, []byte(origin+"/console")) || !bytes.Contains(output, []byte("principal_password")) {
		t.Fatalf("console discovery failed: %v", err)
	}
	if err := server.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if err := server.Wait(); err != nil {
		t.Fatalf("foreground shutdown: %v", err)
	}

	// Enrollment absence cannot silently degrade into an unauthenticated console.
	verifier := filepath.Join(statePath, "auth", principalauth.FileName)
	if err := os.Remove(verifier); err != nil {
		t.Fatal(err)
	}
	denied := exec.CommandContext(ctx, binary, "--config", configPath, "serve")
	denied.Env = environment
	output, err = denied.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "principal") {
		t.Fatal("missing password verifier did not deny serve")
	}
	if _, err := os.Stat(verifier); !os.IsNotExist(err) {
		t.Fatal("serve silently enrolled a verifier")
	}
}

// Never render protected capture on failure, including an echo regression.
func readQuickstartProtectedPTY(t *testing.T, master *os.File, capture []byte, marker string, timeout time.Duration) []byte {
	t.Helper()
	deadline := time.Now().Add(timeout)
	poll := []unix.PollFd{{Fd: int32(master.Fd()), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR}}
	for !bytes.Contains(capture, []byte(marker)) {
		if time.Now().After(deadline) || len(capture) > 1<<20 {
			t.Fatal("protected PTY capture exceeded time or byte bound; output withheld")
		}
		ready, err := unix.Poll(poll, 50)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			t.Fatal("protected PTY poll failed; output withheld")
		}
		if ready == 0 {
			continue
		}
		buffer := make([]byte, 1024)
		n, err := master.Read(buffer)
		capture = append(capture, buffer[:n]...)
		if err != nil {
			t.Fatal("protected PTY read failed; output withheld")
		}
	}
	return capture
}
