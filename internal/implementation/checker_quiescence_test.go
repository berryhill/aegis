package implementation

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"syscall"
	"testing"

	"github.com/berryhill/aegis/internal/execution"
)

// This independent producer has no concrete native diagnostic type. Its message
// and unrecognized stage are private input, not safe diagnostic text.
type checkerDiagnosticFixture struct {
	stage string
	cause error
}

func (e *checkerDiagnosticFixture) Error() string        { return "private child output" }
func (e *checkerDiagnosticFixture) Stage() string        { return e.stage }
func (e *checkerDiagnosticFixture) Errno() syscall.Errno { return syscall.EACCES }
func (e *checkerDiagnosticFixture) Unwrap() error        { return e.cause }

func TestCheckerQuiescenceStructuralDiagnostic(t *testing.T) {
	private := &os.PathError{Op: "private operation", Path: "private path", Err: syscall.EACCES}
	for _, tc := range []struct {
		name  string
		cause error
		stage string
		errno syscall.Errno
	}{
		{"native", execution.NativeQuiescenceFailure(execution.NativeQuiescenceCensusRecheck, private), "census_recheck", syscall.EACCES},
		{"structural", &checkerDiagnosticFixture{stage: "census_recheck", cause: private}, "census_recheck", syscall.EACCES},
		{"private_stage", &checkerDiagnosticFixture{stage: "private stage /path pid=123", cause: private}, "unknown", syscall.EACCES},
		{"raw_errno", private, "unknown", syscall.EACCES},
		{"nil", nil, "unknown", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cause := tc.cause
			if cause != nil {
				cause = fmt.Errorf("private outer message: %w", cause)
			}
			err := CheckerQuiescenceFailure(cause)
			want := ErrCheckerQuiescenceUnavailable.Error() + ": stage=" + tc.stage + " errno=" + strconv.FormatUint(uint64(tc.errno), 10)
			if got := err.Error(); got != want {
				t.Fatalf("unsafe or lost structural diagnostic: got %q want %q", got, want)
			}
			if !errors.Is(err, ErrCheckerQuiescenceUnavailable) {
				t.Fatal("checker denial identity lost")
			}
			if tc.cause != nil && (!errors.Is(err, tc.cause) || !errors.Is(err, private) || !errors.Is(err, syscall.EACCES)) {
				t.Fatal("underlying identities lost")
			}
			if tc.name == "native" {
				var native *execution.NativeQuiescenceError
				if !errors.As(err, &native) || native != tc.cause || !errors.Is(err, execution.ErrNativeQuiescenceUnavailable) {
					t.Fatal("concrete native origin or sentinel lost")
				}
			}
			if tc.name == "structural" || tc.name == "private_stage" {
				var diagnostic interface {
					error
					Stage() string
					Errno() syscall.Errno
				}
				if !errors.As(err, &diagnostic) || diagnostic != tc.cause {
					t.Fatal("structural origin lost")
				}
			}
		})
	}
}
