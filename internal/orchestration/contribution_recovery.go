package orchestration

import (
	"context"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/queue"
)

// WithContributionRecovery is a synchronous owning-service recovery port, not a
// Queue retry or authority grant. Badger's exclusive writer plus kernel flock
// exclude the old service and source writer before the controller stops roles.
func (w *QueueWorker) WithContributionRecovery(ctx context.Context, sub core.Subject, queueID string, action func(context.Context) error) error {
	if w.implementation == nil || action == nil {
		return ErrWorkerDenied
	}
	projection, err := w.repository.GetQueueProjection(ctx, queueID)
	if err != nil || projection.State != queue.StateClaimed || projection.Attempts != 1 {
		return ErrWorkerDenied
	}
	claim, err := w.repository.GetClaim(ctx, projection.ActiveClaimID)
	if err != nil {
		return err
	}
	attempt, err := w.repository.GetAttempt(ctx, claim.AttemptID)
	if err != nil || attempt.ClaimID != claim.ClaimID {
		return ErrWorkerDenied
	}
	child, err := w.repository.GetLoopExecution(ctx, attempt.LoopExecutionID)
	if err != nil {
		return err
	}
	revision, err := w.repository.GetLoopRevision(ctx, child.Loop.ID, child.Loop.Revision)
	if err != nil || revision.Digest != child.Loop.Digest || managedContributionProfile(revision) != nil {
		return ErrWorkerDenied
	}
	c, sealed, err := w.contributionContract(ctx, queueID, revision)
	if err != nil || sealed.OwnerID != sub.PrincipalID {
		return ErrWorkerDenied
	}
	custody, ok := w.repository.(interface{ ImplementationStore() implementation.Store })
	if !ok {
		return ErrWorkerDenied
	}
	kernel := &implementation.ManagedContribution{DB: custody.ImplementationStore(), GoBinary: w.implementation.config.GoBinary, Repository: sealed.Repository, Admit: func(context.Context, string) error { return nil }}
	return kernel.WithRecoveryBoundary(ctx, sealed.RunID, sealed.Digest, attempt.AttemptID, sealed.Base, c, *revision.Contribution.Managed, action)
}
