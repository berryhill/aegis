//go:build linux

package implementation

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"testing"

	"golang.org/x/sys/unix"
)

// Only a PID already recorded by the checker fixture may disappear benignly.
// ESRCH from a proc read means no such process, not a surviving writer. Other
// read errors still fail; a successful read must pass the original state check.
func checkerProcDisappeared(err error) bool {
	return os.IsNotExist(err) || errors.Is(err, unix.ESRCH)
}

func TestCheckerProcDisappeared(t *testing.T) {
	pathError := func(err error) error {
		return &os.PathError{Op: "read", Path: "/proc/5202/stat", Err: err}
	}
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"ENOENT", unix.ENOENT, true},
		{"wrapped_ENOENT", pathError(unix.ENOENT), true},
		{"ESRCH", unix.ESRCH, true},
		{"wrapped_ESRCH", pathError(unix.ESRCH), true},
		{"nested_ESRCH", fmt.Errorf("proc read: %w", pathError(unix.ESRCH)), true},
		{"EPERM", pathError(unix.EPERM), false},
		{"EACCES", pathError(unix.EACCES), false},
		{"EIO", pathError(unix.EIO), false},
		{"parse", &strconv.NumError{Func: "Atoi", Num: "malformed", Err: strconv.ErrSyntax}, false},
		{"unknown", errors.New("unknown proc failure"), false},
		{"ESRCH_text_is_not_errno", errors.New("no such process"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := checkerProcDisappeared(tc.err); got != tc.want {
				t.Fatalf("checkerProcDisappeared(%v) = %t, want %t", tc.err, got, tc.want)
			}
		})
	}
}
