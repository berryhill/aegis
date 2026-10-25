package orchestration

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/queue"
)

// SetContributionFetch supplies a controller-owned fixture transport only. It is
// not a WorkRequest/model input. Nil preserves strict production HTTPS fetch;
// repository origin, branch and base checks remain mandatory in either case.
func (w *QueueWorker) SetContributionFetch(fetch func(context.Context, loop.ContributionExecutionPolicy) error) {
	w.contributionFetch = fetch
}

// AcceptedContributionCandidate exports intermediate evidence under the original
// controller authority. It never issues replacement sessions or renews authority.
func (w *QueueWorker) AcceptedContributionCandidate(ctx context.Context, subject core.Subject, queueID string, revision loop.LoopRevision) (implementation.ContributionCandidate, error) {
	deny := fmt.Errorf("%w: exact current contribution handoff admission required", ErrWorkerDenied)
	if w == nil || w.repository == nil || w.service == nil || w.now == nil || w.implementation == nil || !filepath.IsAbs(w.implementation.config.GoBinary) || w.contributionRoles == nil {
		return implementation.ContributionCandidate{}, deny
	}
	_, sealed, err := w.contributionContract(ctx, queueID, revision)
	if err != nil {
		return implementation.ContributionCandidate{}, err
	}
	if revision.Contribution == nil || revision.Contribution.Managed == nil {
		return implementation.ContributionCandidate{}, deny
	}
	custody, ok := w.repository.(interface{ ImplementationStore() implementation.Store })
	if !ok || custody.ImplementationStore() == nil {
		return implementation.ContributionCandidate{}, deny
	}
	engine := &implementation.ManagedContribution{DB: custody.ImplementationStore(), GoBinary: w.implementation.config.GoBinary}
	record, err := engine.Read(sealed.RunID)
	if err != nil {
		return implementation.ContributionCandidate{}, err
	}
	engine.Admit = func(c context.Context, _ string) error {
		if c.Err() != nil || ctx.Err() != nil {
			return deny
		}
		_, current, err := w.contributionContract(c, queueID, revision)
		if err != nil || current.Digest != sealed.Digest {
			return deny
		}
		item, err := w.repository.GetQueueItem(c, queueID)
		if err != nil || item.GraphRunID != sealed.RunID || item.Submission.ID != sealed.SubmissionID || item.MaxAttempts != 1 {
			return fmt.Errorf("%w: queue", deny)
		}
		projection, err := w.repository.GetQueueProjection(c, queueID)
		if err != nil || projection.State != queue.StateClaimed || projection.Attempts != 1 || projection.ActiveClaimID == "" {
			return fmt.Errorf("%w: projection", deny)
		}
		claim, err := w.repository.GetClaim(c, projection.ActiveClaimID)
		if err != nil || claim.QueueItem != digestRef(item.ItemID, item.Digest) || claim.AttemptID != record.QueueAttempt || !w.now().Before(claim.ExpiresAt) {
			return fmt.Errorf("%w: claim", deny)
		}
		attempt, err := w.repository.GetAttempt(c, claim.AttemptID)
		if err != nil || attempt.QueueItem != claim.QueueItem || attempt.ClaimID != claim.ClaimID || attempt.GraphRunID != sealed.RunID || attempt.AttemptNumber != projection.Attempts {
			return fmt.Errorf("%w: attempt", deny)
		}
		execution, err := w.repository.GetLoopExecution(c, attempt.LoopExecutionID)
		if err != nil || execution.Loop != sealed.Loop || execution.Participant != sealed.Controller || execution.GraphRunID != sealed.RunID {
			return fmt.Errorf("%w: execution", deny)
		}
		agent, err := w.repository.GetAgentRevision(c, sealed.Controller.ID, sealed.Controller.Revision)
		if err != nil || agent.Digest != sealed.Controller.Digest || w.authorizeImplementationRevision(revision, agent) != nil {
			return fmt.Errorf("%w: host", deny)
		}
		authority, mandate, ready := w.service.resolveAuthority(c, subject, claim.Authority)
		if ready.State != ReadinessReady || authority.ID != claim.Authority.ID || authority.Digest != claim.Authority.Digest || !agentMatchesAuthority(agent, authority, mandate) {
			return fmt.Errorf("%w: authority", deny)
		}
		ready = w.service.Readiness(c, ReadinessRequest{Action: FleetActionRuntimeEffect, Subject: subject, Authority: claim.Authority, Agent: sealed.Controller, Loop: sealed.Loop})
		if ready.State != ReadinessReady {
			return fmt.Errorf("%w: readiness", deny)
		}
		return nil
	}
	if err := engine.Admit(ctx, "candidate-handoff"); err != nil {
		return implementation.ContributionCandidate{}, err
	}
	return engine.AcceptedCandidate(sealed.RunID, sealed.Digest, *revision.Contribution.Managed)
}
