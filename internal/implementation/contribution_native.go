package implementation

import (
	"context"
	"errors"
	"os/exec"
)

// commandOutput preserves bounded output while routing Git discovery and candidate
// object production through the same native custody port as Go build/test.
func (e *ManagedContribution) commandOutput(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	var stdout, stderr boundedOutput
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	var err error
	if e.Native != nil {
		err = e.Native(ctx, cmd)
	} else {
		err = cmd.Run()
	}
	if stdout.overflow || stderr.overflow {
		return nil, errors.New("native command output exceeds bound")
	}
	return stdout.bytes, err
}
