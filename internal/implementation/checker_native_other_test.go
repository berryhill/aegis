//go:build !linux

package implementation

import (
	"context"
	"errors"
	"os/exec"
)

func runCheckerNative(context.Context, *exec.Cmd) error {
	return errors.New("checker descendant quiescence unsupported on this platform")
}
