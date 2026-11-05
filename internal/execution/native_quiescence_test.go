package execution

import (
	"context"
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"
)

func TestNativeQuiescenceDiagnosticSafety(t *testing.T) {
	private := &os.PathError{Op: "private operation", Path: "private-path", Err: syscall.EACCES}
	origin := NativeQuiescenceFailure(NativeQuiescenceCensusRecheck, private)
	err := NativeQuiescenceFailure(NativeQuiescenceWaitJoin, errors.Join(origin, context.DeadlineExceeded, errors.New("private child output")))
	var diagnostic *NativeQuiescenceError
	if !errors.As(err, &diagnostic) || diagnostic.Stage() != "census_recheck" || diagnostic.Errno() != syscall.EACCES {
		t.Fatalf("typed origin lost: %v", err)
	}
	for _, want := range []error{ErrNativeQuiescenceUnavailable, syscall.EACCES, private, context.DeadlineExceeded} {
		if !errors.Is(err, want) {
			t.Fatalf("error identity lost: %v", want)
		}
	}
	if got := err.Error(); got != "contribution native quiescence unavailable: stage=census_recheck errno=13" {
		t.Fatalf("unsafe or changed rendering: %q", got)
	}
	for _, stage := range []NativeQuiescenceStage{NativeQuiescenceUnknown, NativeQuiescenceLeaderWait, NativeQuiescenceBarrier, NativeQuiescenceCensusDeadline, NativeQuiescenceWaitJoin, 255} {
		err := NativeQuiescenceFailure(stage, errors.New("private child output"))
		if !errors.Is(err, ErrNativeQuiescenceUnavailable) || strings.Contains(err.Error(), "private") {
			t.Fatalf("unsafe diagnostic: %v", err)
		}
	}
	var unknown *NativeQuiescenceError
	if !errors.As(NativeQuiescenceFailure(255, nil), &unknown) || unknown.Stage() != "unknown" {
		t.Fatal("stage set is not closed")
	}
}
