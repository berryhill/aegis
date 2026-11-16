//go:build !linux

package hermes

import (
	"errors"
	"io"
	"os/exec"
)

// Do not start a disposable turn without an owned-group cleanup implementation.
func prepareAttemptProcess() error {
	return errors.New("Hermes disposable-turn process custody requires Linux")
}

func watchAttemptProcess(_ *exec.Cmd, _ chan<- error) func() { return func() {} }
func stopOwnedAttemptProcess(_ *exec.Cmd, _ io.Closer) error { return ErrAttemptCleanup }
