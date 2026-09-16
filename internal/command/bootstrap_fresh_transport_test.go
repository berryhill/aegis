package command

import (
	"bytes"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"fmt"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/credentials"
	"github.com/berryhill/aegis/internal/onboarding"
)

type bootstrapTransportWriter struct {
	bytes.Buffer
	trigger string
	action  func()
}

func (w *bootstrapTransportWriter) Write(p []byte) (int, error) {
	n, err := w.Buffer.Write(p)
	if w.action != nil && strings.Contains(w.String(), w.trigger) {
		action := w.action
		w.action = nil
		action()
	}
	return n, err
}

// Match terminal delivery rather than pre-buffering future approval responses.
type bootstrapApprovalReader struct{ io.Reader }

func (r bootstrapApprovalReader) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return r.Reader.Read(p)
}

func bootstrapTree(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		result[path] = info.Mode().String()
		if entry.Type().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			result[path] += ":" + string(data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestBootstrapTransportAppearsBeforeOtherAuthorityWrites(t *testing.T) {
	for _, phase := range []string{"operational authority", "delivered systemd", "replace systemd"} {
		t.Run(phase, func(t *testing.T) {
			path := managerTestConfig(t)
			root := filepath.Dir(path)
			socketRoot, err := os.MkdirTemp("", "aegis-at-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(socketRoot) })
			socket := filepath.Join(socketRoot, "a.sock")
			document := string(mustCommandRead(t, path)) + fmt.Sprintf("api:\n  unix_socket: %q\n", socket)
			if err = os.WriteFile(path, []byte(document), 0600); err != nil {
				t.Fatal(err)
			}
			trigger := ""
			input := "yes\n"
			if phase == "operational authority" {
				cfg := config.Inspect(path).Config
				if err = os.RemoveAll(filepath.Join(cfg.StateDir, "persistence", "authority-v1")); err != nil {
					t.Fatal(err)
				}
				trigger = "Initialize empty operational authority generation"
			} else {
				plan, err := onboarding.PreviewAuthority(path, "systemd")
				if err != nil {
					t.Fatal(err)
				}
				if err = onboarding.ApplyAuthority(plan); err != nil {
					t.Fatal(err)
				}
				t.Setenv("CREDENTIALS_DIRECTORY", "")
				trigger = "Create encrypted credential authority"
				input = "yes\nyes\n"
				if phase == "delivered systemd" {
					directory := filepath.Join(root, "delivered")
					if err = os.Mkdir(directory, 0700); err != nil {
						t.Fatal(err)
					}
					if err = credentials.CreateHostKey(filepath.Join(directory, plan.KEKCredential), "systemd-credential-kek"); err != nil {
						t.Fatal(err)
					}
					t.Setenv("CREDENTIALS_DIRECTORY", directory)
					trigger = "Initialize systemd-backed credential authority"
				}
			}
			var before map[string]string
			var listener *net.UnixListener
			out := &bootstrapTransportWriter{trigger: trigger}
			out.action = func() {
				listener, err = net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = listener.Close() })
				if err = os.Chmod(socket, 0600); err != nil {
					t.Fatal(err)
				}
				before = bootstrapTree(t, root)
			}
			runner := &bootstrapGatewayRunner{}
			command := NewRoot(Dependencies{In: bootstrapApprovalReader{strings.NewReader(input)}, Out: out, Err: io.Discard, Passphrases: &recordingPassphrases{value: []byte("synthetic-bootstrap-password")}, UserService: runner, IsTerminal: func(io.Reader, io.Writer) bool { return true }})
			command.SetArgs([]string{"--config", path, "init"})
			err = command.Execute()
			if listener == nil {
				t.Fatalf("phase not reached: %v\n%s", err, out.String())
			}
			if err == nil || !strings.Contains(err.Error(), "bootstrap_gateway_recovery_required") {
				t.Errorf("expected transport recovery error, got %v", err)
			}
			if !reflect.DeepEqual(before, bootstrapTree(t, root)) {
				t.Error("authority/configuration mutated despite transport appearing")
			}
			if _, err = os.Lstat(socket); err != nil {
				t.Errorf("socket removed: %v", err)
			}
			if len(runner.runs) != 0 {
				t.Errorf("unverified gateway actions: %v", runner.runs)
			}
		})
	}
}

func TestFreshBootstrapTransportOwnershipBeforeWrites(t *testing.T) {
	for _, phase := range []string{"before configuration", "after configuration", "after custody approval", "after passphrase approval", "after systemd plan approval"} {
		t.Run(phase, func(t *testing.T) {
			root, err := os.MkdirTemp("", "aegis-ft-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(root) })
			path := filepath.Join(root, "aegis.yaml")
			state := filepath.Join(root, "state")
			socket := filepath.Join(state, "transport", "aegis.sock")
			var before map[string]string
			var listener *net.UnixListener
			appear := func() {
				if err := os.MkdirAll(filepath.Dir(socket), 0700); err != nil {
					t.Fatal(err)
				}
				listener, err = net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(socket, 0600); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = listener.Close() })
				before = bootstrapTree(t, root)
			}
			out := &bootstrapTransportWriter{}
			input := "yes\nyes\nyes\n"
			switch phase {
			case "before configuration":
				appear()
			case "after configuration":
				out.trigger = "Initialization completed atomically."
				out.action = appear
			case "after custody approval":
				out.trigger = "Apply exact advanced custody plan"
				out.action = appear
			case "after passphrase approval":
				input = "yes\nadvanced\n2\nyes\n"
				out.trigger = "Create encrypted credential authority"
				out.action = appear
			case "after systemd plan approval":
				input = "yes\nadvanced\n3\nyes\n"
				out.trigger = "Apply exact advanced custody plan"
				out.action = appear
			}
			runner := &bootstrapGatewayRunner{}
			command := NewRoot(Dependencies{In: bootstrapApprovalReader{strings.NewReader(input)}, Out: out, Err: io.Discard, Passphrases: &recordingPassphrases{value: []byte("synthetic-bootstrap-password")}, UserService: runner, IsTerminal: func(io.Reader, io.Writer) bool { return true }})
			command.SetArgs([]string{"--config", path, "--state-dir", state, "init"})
			err = command.Execute()
			if listener == nil {
				t.Fatalf("did not reach transport phase: %v\n%s", err, out.String())
			}
			if err == nil || !strings.Contains(err.Error(), "bootstrap_gateway_recovery_required") {
				t.Errorf("expected actionable transport recovery error, got %v", err)
			}
			if !reflect.DeepEqual(before, bootstrapTree(t, root)) {
				t.Error("bootstrap mutated configuration or authority artifacts after transport appeared")
			}
			if _, err := os.Lstat(socket); err != nil {
				t.Errorf("transport removed: %v", err)
			}
			if len(runner.runs) != 0 {
				t.Errorf("unverified gateway action: %v", runner.runs)
			}
		})
	}
}
