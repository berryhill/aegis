package implementation

import (
	"errors"
	"strconv"
	"syscall"
)

// checkerQuiescenceDiagnostic is metadata only, never completion proof. The
// producer owns its concrete error type; the checker does not depend on it.
type checkerQuiescenceDiagnostic interface {
	Stage() string
	Errno() syscall.Errno
}

type checkerQuiescenceError struct {
	cause error
	stage string
	errno syscall.Errno
}

func (e *checkerQuiescenceError) Error() string {
	return ErrCheckerQuiescenceUnavailable.Error() + ": stage=" + e.stage + " errno=" + strconv.FormatUint(uint64(e.errno), 10)
}

func (e *checkerQuiescenceError) Is(target error) bool {
	return target == ErrCheckerQuiescenceUnavailable
}

func (e *checkerQuiescenceError) Unwrap() error { return e.cause }

// CheckerQuiescenceFailure classifies an unknown native join without printing
// the arbitrary underlying error. Both checker/native identities and the typed
// origin remain available to errors.Is/As; this grants no completion proof.
func CheckerQuiescenceFailure(cause error) error {
	stage := "unknown"
	var errno syscall.Errno
	errors.As(cause, &errno)
	var diagnostic checkerQuiescenceDiagnostic
	if errors.As(cause, &diagnostic) {
		// A structural implementation is not trusted text. Only this closed
		// diagnostic vocabulary may be rendered; no lifecycle/authority policy
		// is inferred from it, and unknown producer stages remain private.
		switch candidate := diagnostic.Stage(); candidate {
		case "leader_waitid", "barrier", "barrier_waitid", "barrier_signal",
			"census", "census_directory", "census_context", "census_stat",
			"census_open", "census_recheck", "census_identity", "census_signal",
			"census_close", "census_deadline", "wait_join":
			stage = candidate
		}
		errno = diagnostic.Errno()
	}
	return &checkerQuiescenceError{cause: cause, stage: stage, errno: errno}
}
