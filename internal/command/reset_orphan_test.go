//go:build linux

package command

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/config"
	resetdomain "github.com/berryhill/aegis/internal/reset"
	"github.com/spf13/cobra"
)

func TestResetOrphanThenBare(t *testing.T) {
	f := newResetCommandFixture(t, false)
	f.service.RepositoryResetRoot = filepath.Join(f.home, "r", ".aegis")
	f.config = filepath.Join(f.service.RepositoryResetRoot, "aegis.yaml")
	f.state = filepath.Join(f.service.RepositoryResetRoot, "state")
	socket := filepath.Join(f.state, "transport", "aegis.sock")
	if err := os.MkdirAll(filepath.Dir(socket), 0700); err != nil {
		t.Fatal(err)
	}
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	l.SetUnlinkOnClose(false)
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(socket, 0600); err != nil {
		t.Fatal(err)
	}
	stale, err := os.Lstat(socket)
	if err != nil || stale.Mode()&os.ModeSocket == 0 {
		t.Fatalf("stale socket fixture missing: %v", err)
	}
	var out bytes.Buffer
	newReset := func() *cobra.Command {
		return resetCmdWithHooks(f.service, func(io.Reader, io.Writer) bool { return true }, &rootOptions{configFile: f.config, stateDir: f.state}, DevelopmentProfile, func(_ *cobra.Command, _ resetdomain.Plan) error { return nil }, func(context.Context, string) (bool, error) { return false, nil })
	}
	declined := newReset()
	declined.SetIn(strings.NewReader("no\n"))
	declined.SetOut(&out)
	declined.SetErr(io.Discard)
	if err := declined.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), resetdomain.ReasonDeclined) || !strings.Contains(out.String(), "deletion_not_applied") {
		t.Fatal("missing explicit reset decline result")
	}
	out.Reset()
	preserved, err := os.Lstat(socket)
	if err != nil || !os.SameFile(stale, preserved) {
		t.Fatalf("declined reset changed exact stale socket: %v", err)
	}
	cmd := newReset()
	cmd.SetIn(strings.NewReader("yes\n"))
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "reset_complete") {
		t.Fatal(out.String())
	}
	if _, err := os.Lstat(socket); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("confirmed reset left exact stale socket %s: %v", socket, err)
	}
	out.Reset()
	// Exercise real first-run initialization, but never consult host pinentry.
	// Deliver each approval separately, then explicitly decline credential custody.
	provider := &sequencePassphrases{values: [][]byte{[]byte(rand.Text())}}
	root := NewRoot(Dependencies{UserService: absentUserService(t), In: bootstrapApprovalReader{strings.NewReader("yes\nno\n")}, Out: &out, Err: io.Discard, Version: "test", Passphrases: provider, IsTerminal: func(io.Reader, io.Writer) bool { return true }})
	root.SetArgs([]string{"--config", f.config, "--state-dir", f.state})
	if err := root.Execute(); err != nil {
		t.Fatalf("bare startup after reset: %v", err)
	}
	for _, expected := range []string{"local identity and configuration", "Initialization completed atomically", "Setup progress  1/5 verified", "DECISION / Choose credential authority custody"} {
		if !strings.Contains(out.String(), expected) {
			t.Fatalf("bare startup did not reach %q", expected)
		}
	}
	if provider.calls != 1 {
		t.Fatalf("protected input calls = %d, want exactly one initialization request", provider.calls)
	}
	if strings.Contains(out.String(), "bootstrap_gateway_recovery_required") {
		t.Fatal("bare startup still denied on transport")
	}
	if config.Inspect(f.config).State != config.StateValid {
		t.Fatal("bare startup did not persist valid configuration")
	}
	for _, path := range []string{socket, filepath.Join(f.state, "credentials", "authority.db"), filepath.Join(f.state, "credentials", "authority.kek"), filepath.Join(f.state, "credentials", "authority.kek.enc")} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("declined custody boundary created %s: %v", path, err)
		}
	}
}
