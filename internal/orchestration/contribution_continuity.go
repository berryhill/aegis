package orchestration

import (
	"context"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/implementation"
	"os/exec"
)

// ContributionExecutionObserver is installed once by the owning application.
// It sees actual role launches and exact native Cmds, never model assertions.
type ContributionExecutionObserver struct {
	ContinueBuilder func(context.Context, implementation.ContributionRoleIdentity, implementation.ContributionRoleIdentity) error
	Checkpoint      func(context.Context, implementation.ContributionCheckpoint) error
	Turn            func(context.Context, string) error
	Native          func(context.Context, string, *exec.Cmd) error
	Role            func(context.Context, int, string) error
	StopRole        func(context.Context, string) error
	Close           func(context.Context, bool) error
}
type ContributionContinuityFactory func(context.Context, core.Subject, contribution.Contract, WorkResult, execution.LaunchContract) (*ContributionExecutionObserver, error)

func (w *QueueWorker) SetContributionContinuityFactory(f ContributionContinuityFactory) {
	w.contributionContinuity = f
}

func (w *QueueWorker) SetContributionPreclaim(f func(context.Context, core.Subject, contribution.Contract, string) error) {
	w.contributionPreclaim = f
}

// Trusted host observer, never a WorkRequest/model input or authority port.
func (w *QueueWorker) SetContributionCheckpointObserver(f func(context.Context, implementation.ContributionCheckpoint) error) {
	w.contributionCheckpoint = f
}
