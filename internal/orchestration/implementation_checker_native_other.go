//go:build !linux

package orchestration

import (
	"context"
	"errors"
	"os/exec"
)

// Deny before Start on platforms without a qualified descendant join.
func runImplementationCheckerNative(context.Context, *exec.Cmd) error {
	return errors.New("checker descendant quiescence unsupported on this platform")
}
