package execution

import (
	"errors"
	"strconv"
	"syscall"
)

// NativeQuiescenceStage is closed diagnostic metadata, never ownership proof.
type NativeQuiescenceStage uint8

const (
	NativeQuiescenceUnknown NativeQuiescenceStage = iota
	NativeQuiescenceLeaderWait
	NativeQuiescenceBarrier
	NativeQuiescenceBarrierWait
	NativeQuiescenceBarrierSignal
	NativeQuiescenceCensus
	NativeQuiescenceCensusDirectory
	NativeQuiescenceCensusContext
	NativeQuiescenceCensusStat
	NativeQuiescenceCensusOpen
	NativeQuiescenceCensusRecheck
	NativeQuiescenceCensusIdentity
	NativeQuiescenceCensusSignal
	NativeQuiescenceCensusClose
	NativeQuiescenceCensusDeadline
	NativeQuiescenceWaitJoin
)

var nativeQuiescenceStages = [...]string{"unknown", "leader_waitid", "barrier", "barrier_waitid", "barrier_signal", "census", "census_directory", "census_context", "census_stat", "census_open", "census_recheck", "census_identity", "census_signal", "census_close", "census_deadline", "wait_join"}

// NativeQuiescenceError retains error identity without formatting child output,
// paths, PIDs, command lines or arbitrary underlying error strings.
type NativeQuiescenceError struct {
	stage NativeQuiescenceStage
	cause error
}

func (e *NativeQuiescenceError) Stage() string { return nativeQuiescenceStages[e.stage] }
func (e *NativeQuiescenceError) Errno() syscall.Errno {
	var errno syscall.Errno
	errors.As(e.cause, &errno)
	return errno
}
func (e *NativeQuiescenceError) Error() string {
	return ErrNativeQuiescenceUnavailable.Error() + ": stage=" + e.Stage() + " errno=" + strconv.FormatUint(uint64(e.Errno()), 10)
}
func (e *NativeQuiescenceError) Unwrap() []error {
	return []error{ErrNativeQuiescenceUnavailable, e.cause}
}

// NativeQuiescenceFailure preserves the first typed origin through outer guards.
func NativeQuiescenceFailure(stage NativeQuiescenceStage, cause error) error {
	var origin *NativeQuiescenceError
	if errors.As(cause, &origin) {
		stage = origin.stage
	}
	if int(stage) >= len(nativeQuiescenceStages) {
		stage = NativeQuiescenceUnknown
	}
	return &NativeQuiescenceError{stage: stage, cause: cause}
}
