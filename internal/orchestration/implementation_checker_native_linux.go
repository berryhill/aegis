//go:build linux

package orchestration

import (
	"context"
	"errors"
	"os/exec"

	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/runtime/hermes"
)

// These callbacks track local lifecycle only. There is no durable contribution
// reservation or receipt: the checker home/workspace remain kernel-owned.
func runImplementationCheckerNative(ctx context.Context, cmd *exec.Cmd) error {
	joined := false
	err := hermes.RunContributionNative(ctx, execution.NativeBinding{CommandDigest: hermes.NativeCommandDigest(cmd)}, cmd,
		func(context.Context) error { return nil },
		func(_ context.Context, completion hermes.NativeCompletion) error {
			joined = completion.Joined()
			return nil
		})
	if errors.Is(err, execution.ErrNativeQuiescenceUnavailable) || (cmd.Process != nil && !joined) {
		return implementation.ErrCheckerQuiescenceUnavailable
	}
	return err
}
