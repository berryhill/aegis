//go:build linux

package hermes

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/execution"
	"golang.org/x/sys/unix"
)

// A single exiting child, no descendants, TCP or listeners. The real barrier and
// sole Wait run; only the post-barrier census failure is synthetic.
func TestContributionNativeDiagnosticOrigin(t *testing.T) {
	cmd := exec.Command("/bin/true")
	joined := true
	private := &os.PathError{Op: "read", Path: "private-path-must-not-appear", Err: unix.EACCES}
	err := runContributionNative(context.Background(), execution.NativeBinding{CommandDigest: NativeCommandDigest(cmd)}, cmd,
		func(context.Context) error { return nil },
		func(_ context.Context, p NativeCompletion) error { joined = p.Joined(); return nil },
		func(context.Context, int) (bool, error) { return false, private })
	if !errors.Is(err, unix.EACCES) {
		t.Fatalf("native census origin lost: %v", err)
	}
	var diagnostic interface{ Stage() string }
	if !errors.As(err, &diagnostic) || diagnostic.Stage() != "census" {
		t.Fatalf("native census stage lost: %v", err)
	}
	if !errors.Is(err, execution.ErrNativeQuiescenceUnavailable) || joined || cmd.ProcessState == nil {
		t.Fatal("unknown census granted proof or skipped Wait")
	}
	if strings.Contains(err.Error(), private.Path) {
		t.Fatal("diagnostic leaked private path")
	}
}
