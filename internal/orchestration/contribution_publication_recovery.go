package orchestration

import (
	"context"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/persistence/fleet"
)

// WithTerminalPublicationRecovery is terminal-only safety cleanup. It reuses the
// existing exact workspace recovery lock and native record validation; it cannot
// claim, resume, execute, renew authority or change the original native record.
func (w *QueueWorker) WithTerminalPublicationRecovery(ctx context.Context, sub core.Subject, queueID string, action func(context.Context) error) error {
	if w == nil || w.implementation == nil || action == nil {
		return ErrWorkerDenied
	}
	owner, ok := w.repository.(fleet.ContributionRepository)
	if !ok {
		return ErrWorkerDenied
	}
	sealed, err := owner.GetContributionForQueue(ctx, queueID)
	if err != nil || sealed.OwnerID != sub.PrincipalID {
		return ErrWorkerDenied
	}
	d, err := w.repository.GetDispositionByGraphRun(ctx, sealed.RunID)
	if err != nil || d.QueueItem.ID != queueID || d.GraphRunID != sealed.RunID {
		return ErrWorkerDenied
	}
	projection, err := w.repository.GetQueueProjection(ctx, queueID)
	if err != nil || string(projection.State) != string(d.State) || projection.Attempts != 1 {
		return ErrWorkerDenied
	}
	switch string(d.State) {
	case "succeeded", "failed", "denied", "cancelled", "revoked", "expired":
	default:
		return ErrWorkerDenied
	}
	item, err := w.repository.GetQueueItem(ctx, queueID)
	if err != nil || digestRef(item.ItemID, item.Digest) != d.QueueItem {
		return ErrWorkerDenied
	}
	attempt, err := w.repository.GetAttempt(ctx, d.AttemptID)
	if err != nil || attempt.QueueItem != d.QueueItem || attempt.GraphRunID != sealed.RunID || attempt.LoopExecutionID != d.LoopExecutionID {
		return ErrWorkerDenied
	}
	claim, err := w.repository.GetClaim(ctx, attempt.ClaimID)
	if err != nil || claim.QueueItem != d.QueueItem || claim.AttemptID != attempt.AttemptID {
		return ErrWorkerDenied
	}
	child, err := w.repository.GetLoopExecution(ctx, attempt.LoopExecutionID)
	if err != nil || child.GraphRunID != sealed.RunID || child.Loop != sealed.Loop {
		return ErrWorkerDenied
	}
	revision, err := w.repository.GetLoopRevision(ctx, child.Loop.ID, child.Loop.Revision)
	if err != nil || revision.Digest != sealed.Loop.Digest || managedContributionProfile(revision) != nil {
		return ErrWorkerDenied
	}
	c, current, err := w.contributionContract(ctx, queueID, revision)
	if err != nil || current.Digest != sealed.Digest {
		return ErrWorkerDenied
	}
	custody, ok := w.repository.(interface{ ImplementationStore() implementation.Store })
	if !ok {
		return ErrWorkerDenied
	}
	kernel := &implementation.ManagedContribution{DB: custody.ImplementationStore(), GoBinary: w.implementation.config.GoBinary, Repository: sealed.Repository, Admit: func(context.Context, string) error { return nil }}
	return kernel.WithRecoveryBoundary(ctx, sealed.RunID, sealed.Digest, attempt.AttemptID, sealed.Base, c, *revision.Contribution.Managed, action)
}
