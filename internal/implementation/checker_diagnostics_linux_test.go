//go:build linux

package implementation

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/execution"
	"golang.org/x/sys/unix"
)

func TestCheckerDiagnosticGuards(t *testing.T) {
	for _, guard := range []string{"native", "unjoined"} {
		t.Run(guard, func(t *testing.T) {
			private := &os.PathError{Op: "read", Path: "private-path-must-not-appear", Err: unix.EACCES}
			cause := error(private)
			cmd := &exec.Cmd{}
			joined := true
			if guard == "native" {
				cause = execution.NativeQuiescenceFailure(execution.NativeQuiescenceCensusRecheck, private)
			} else {
				cmd.Process = &os.Process{}
				joined = false
			}
			err := checkerNativeResult(cmd, joined, cause)
			if !errors.Is(err, unix.EACCES) || !errors.Is(err, ErrCheckerQuiescenceUnavailable) {
				t.Fatalf("adapter origin lost (%s): %v", guard, err)
			}
			if guard == "native" {
				var diagnostic *execution.NativeQuiescenceError
				if !errors.As(err, &diagnostic) || diagnostic.Stage() != "census_recheck" || !errors.Is(err, execution.ErrNativeQuiescenceUnavailable) {
					t.Fatalf("typed native origin lost: %v", err)
				}
			}
			if strings.Contains(err.Error(), private.Path) {
				t.Fatal("adapter leaked private path")
			}
		})
	}
	for _, joined := range []bool{false, true} {
		if err := checkerNativeResult(&exec.Cmd{}, joined, unix.EPERM); err != unix.EPERM {
			t.Fatal("not-started error changed")
		}
	}
	if err := checkerNativeResult(&exec.Cmd{Process: &os.Process{}}, true, nil); err != nil {
		t.Fatal("joined success denied")
	}
	if err := checkerNativeResult(&exec.Cmd{Process: &os.Process{}}, false, nil); !errors.Is(err, ErrCheckerQuiescenceUnavailable) {
		t.Fatal("missing join granted proof")
	}
}
