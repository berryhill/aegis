//go:build linux

package implementation

import (
	"context"
	"errors"
	"os/exec"

	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/runtime/hermes"
)

// Test composition mirrors orchestration without reversing production imports.
func runCheckerNative(ctx context.Context, cmd *exec.Cmd) error {
	joined := false
	err := hermes.RunContributionNative(ctx, execution.NativeBinding{CommandDigest: hermes.NativeCommandDigest(cmd)}, cmd,
		func(context.Context) error { return nil },
		func(_ context.Context, completion hermes.NativeCompletion) error {
			joined = completion.Joined()
			return nil
		})
	return checkerNativeResult(cmd, joined, err)
}

func checkerNativeResult(cmd *exec.Cmd, joined bool, err error) error {
	if errors.Is(err, execution.ErrNativeQuiescenceUnavailable) || (cmd.Process != nil && !joined) {
		return CheckerQuiescenceFailure(err)
	}
	return err
}
