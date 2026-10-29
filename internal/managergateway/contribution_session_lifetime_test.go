package managergateway

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// The session leader must outlive native turns until the owning service revokes
// it. A wall-clock sleep can expire while the independently enforced logical
// authority is still live. A private FIFO keeps the shell blocked without a
// child timer; normal process-group termination/Wait and home cleanup still own
// its lifetime. Opening both ends avoids startup dependence on another writer.
const nativeSessionLifetimeScript = `
mkfifo "$HERMES_HOME/session-lifetime" || exit 70
exec 3<>"$HERMES_HOME/session-lifetime"
printf 'ready\n'
read -r lifetime_end <&3
`

func TestContributionSessionFixtureLifetime(t *testing.T) {
	home := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", nativeSessionLifetimeScript)
	cmd.Env = append(os.Environ(), "HERMES_HOME="+home)
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = cmd.Wait() })
	ready := make(chan error, 1)
	go func() { var b [6]byte; _, err := output.Read(b[:]); ready <- err }()
	select {
	case err := <-ready:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("fixture did not reach lifetime wait")
	}
	// Controllable normal exit, not an arbitrary timeout. The real integration
	// separately exercises RevokeSession and exact native stop acknowledgements.
	f, err := os.OpenFile(filepath.Join(home, "session-lifetime"), os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("end\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
}
