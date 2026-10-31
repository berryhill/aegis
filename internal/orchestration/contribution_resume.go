package orchestration

import (
	"context"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/queue"
)

type ContributionCurrentAdmission func(context.Context, core.Subject, contribution.Contract, queue.Claim) (execution.LaunchContract, execution.AdmissionChecker, error)

func (w *QueueWorker) SetContributionCurrentAdmission(f ContributionCurrentAdmission) {
	w.contributionCurrent = f
}

// ResumeContribution reconstructs the SAME immutable claim/attempt/execution.
// No ClaimQueueItem, retry, new LoopExecution or runtime binding is created.
func (w *QueueWorker) ResumeContribution(ctx context.Context, sub core.Subject, queueID string) (WorkResult, error) {
	return w.resumeContribution(ctx, sub, queueID)
}

type contributionRepairAdmissionKey struct{}
type ContributionDeliveryRepair = implementation.ContributionDeliveryRepair
type ContributionDeliveryRepairAdmission func(context.Context, ContributionRecord) (ContributionDeliveryRepair, error)

// ResumeContributionDeliveryRepair is controller composition, not a transport
// input. The callback re-admits canonical history under the native source lock.
func (w *QueueWorker) ResumeContributionDeliveryRepair(ctx context.Context, sub core.Subject, queueID string, admit ContributionDeliveryRepairAdmission) (WorkResult, error) {
	if admit == nil {
		return WorkResult{}, ErrWorkerDenied
	}
	return w.resumeContribution(context.WithValue(ctx, contributionRepairAdmissionKey{}, admit), sub, queueID)
}

func (w *QueueWorker) resumeContribution(ctx context.Context, sub core.Subject, queueID string) (WorkResult, error) {
	deny := ErrWorkerDenied
	item, err := w.repository.GetQueueItem(ctx, queueID)
	if err != nil {
		return WorkResult{}, err
	}
	projection, err := w.repository.GetQueueProjection(ctx, queueID)
	if err != nil || projection.State != queue.StateClaimed || projection.Attempts != 1 || w.contributionCurrent == nil {
		return WorkResult{}, deny
	}
	claim, err := w.repository.GetClaim(ctx, projection.ActiveClaimID)
	if err != nil || claim.QueueItem != digestRef(item.ItemID, item.Digest) {
		return WorkResult{}, deny
	}
	attempt, err := w.repository.GetAttempt(ctx, claim.AttemptID)
	if err != nil || attempt.ClaimID != claim.ClaimID || attempt.QueueItem != claim.QueueItem || attempt.AttemptNumber != 1 || attempt.GraphRunID != item.GraphRunID {
		return WorkResult{}, deny
	}
	child, err := w.repository.GetLoopExecution(ctx, attempt.LoopExecutionID)
	if err != nil || child.GraphRunID != item.GraphRunID {
		return WorkResult{}, deny
	}
	revision, err := w.repository.GetLoopRevision(ctx, child.Loop.ID, child.Loop.Revision)
	if err != nil || revision.Digest != child.Loop.Digest || managedContributionProfile(revision) != nil {
		return WorkResult{}, deny
	}
	_, sealed, err := w.contributionContract(ctx, queueID, revision)
	if err != nil || child.Participant != sealed.Controller || sealed.OwnerID != sub.PrincipalID {
		return WorkResult{}, deny
	}
	participant, err := w.repository.GetAgentRevision(ctx, child.Participant.ID, child.Participant.Revision)
	if err != nil || participant.Digest != child.Participant.Digest || w.authorizeImplementationRevision(revision, participant) != nil {
		return WorkResult{}, deny
	}
	binding, err := w.repository.GetQueueRuntimeBinding(ctx, queueID)
	if err != nil || binding.Authority != claim.Authority || binding.Submission != item.Submission || binding.OwnerAgent != sealed.Controller {
		return WorkResult{}, deny
	}
	record, err := w.ContributionExecutionReadback(sealed.RunID)
	_, repair := ctx.Value(contributionRepairAdmissionKey{}).(ContributionDeliveryRepairAdmission)
	if err != nil || record.Contract != sealed.Digest || record.QueueAttempt != attempt.AttemptID || (record.State != "phase_boundary" && !(repair && record.State == "candidate_accepted")) {
		return WorkResult{}, deny
	}
	launch, admission, err := w.contributionCurrent(ctx, sub, sealed, claim)
	if err != nil {
		return WorkResult{}, err
	}
	runtime := RuntimeRequest{GraphRunID: item.GraphRunID, LoopExecutionID: child.LoopExecutionID, GraphNodeID: child.GraphNodeID, Authority: claim.Authority, Participant: participant, Launch: launch, LoopRevision: revision, Admission: admission}
	return w.processContribution(ctx, WorkRequest{Subject: sub, QueueItemID: queueID, Authority: claim.Authority, ClaimID: claim.ClaimID, AttemptID: attempt.AttemptID, LoopExecutionID: child.LoopExecutionID, WorkerID: claim.WorkerID}, WorkResult{Claim: claim, Attempt: attempt}, runtime)
}
