//go:build linux

package command

import (
	"bytes"
	"context"
	"github.com/spf13/cobra"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	resetdomain "github.com/berryhill/aegis/internal/reset"
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
	l.Close()
	os.Chmod(socket, 0600)
	var out bytes.Buffer
	cmd := resetCmdWithHooks(f.service, func(io.Reader, io.Writer) bool { return true }, &rootOptions{configFile: f.config, stateDir: f.state}, DevelopmentProfile, func(_ *cobra.Command, _ resetdomain.Plan) error { return nil }, func(context.Context, string) (bool, error) { return false, nil })
	cmd.SetIn(strings.NewReader("yes\n"))
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "reset_complete") {
		t.Fatal(out.String())
	}
	out.Reset()
	root := NewRoot(Dependencies{UserService: absentUserService(t), In: bootstrapApprovalReader{strings.NewReader("no\n")}, Out: &out, Err: io.Discard, Version: "test", IsTerminal: func(io.Reader, io.Writer) bool { return true }})
	root.SetArgs([]string{"--config", f.config, "--state-dir", f.state})
	_ = root.Execute()
	if !strings.Contains(out.String(), "local identity and configuration") {
		t.Fatal(out.String())
	}
}
