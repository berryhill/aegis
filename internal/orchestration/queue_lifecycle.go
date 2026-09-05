package orchestration

import (
	"context"
	"fmt"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/disposition"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	queue "github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
)

const MaxRetryBackoff = 24 * time.Hour

type QueueLifecycleReason string

const (
	ReasonRuntimeRetry      QueueLifecycleReason = "runtime_retry"
	ReasonLeaseReclaimed    QueueLifecycleReason = "lease_reclaimed"
	ReasonOperatorCancelled QueueLifecycleReason = "operator_cancelled"
	ReasonExecutionExpired  QueueLifecycleReason = "execution_expired"
	ReasonRetryExhausted    QueueLifecycleReason = "retry_exhausted"
	ReasonAuthorityRevoked  QueueLifecycleReason = "authority_revoked"
)

type QueueArtifact = evidence.RuntimeArtifact
type QueueReceipt = evidence.VerificationReceipt
type QueueDisposition = disposition.Record

// QueueRetryRequest is a controller-authenticated decision over one active
// claim. Reclaimed distinguishes an expired lease from an acknowledged stopped
// runtime. Caller prose never controls durable lifecycle vocabulary.
type QueueRetryRequest struct {
	Subject          core.Subject         `json:"-"`
	Authority        reference.DigestRef  `json:"authority"`
	QueueItemID      string               `json:"queue_item_id"`
	RetryID          string               `json:"retry_id"`
	TransitionID     string               `json:"transition_id"`
	Backoff          time.Duration        `json:"backoff"`
	Reclaimed        bool                 `json:"reclaimed"`
	ReasonCode       QueueLifecycleReason `json:"reason_code"`
	WorkspaceAgentID string               `json:"agent_id,omitempty"`
	Workspace        *WorkspaceAuthority  `json:"workspace,omitempty"`
}

type QueueTerminalRequest struct {
	Subject          core.Subject         `json:"-"`
	Authority        reference.DigestRef  `json:"authority"`
	QueueItemID      string               `json:"queue_item_id"`
	CancellationID   string               `json:"cancellation_id"`
	TransitionID     string               `json:"transition_id"`
	ReasonCode       QueueLifecycleReason `json:"reason_code"`
	WorkspaceAgentID string               `json:"agent_id,omitempty"`
	Workspace        *WorkspaceAuthority  `json:"workspace,omitempty"`
}

type BindQueueRuntimeRequest struct {
	Subject      core.Subject
	Workspace    *WorkspaceAuthority
	Authority    reference.DigestRef
	QueueItemID  string
	BindingID    string
	TransitionID string
}

// BindRuntime performs the explicit fail-closed handoff. The workspace owner
// authorizes the bind, while an independently admitted runtime authority must
// resolve to that exact Agent.
func (worker *QueueWorker) BindRuntime(ctx context.Context, request BindQueueRuntimeRequest) (queue.RuntimeBinding, bool, error) {
	if err := ctx.Err(); err != nil {
		return queue.RuntimeBinding{}, false, err
	}
	if request.Workspace == nil {
		return queue.RuntimeBinding{}, false, fmt.Errorf("%w: workspace owner required", ErrWorkerDenied)
	}
	workspace, _, err := worker.service.resolveWorkspace(ctx, request.Subject, request.Workspace.Ref(), request.Workspace, WorkspaceManageOwnQueue)
	if err != nil {
		return queue.RuntimeBinding{}, false, fmt.Errorf("%w: workspace owner denied", ErrWorkerDenied)
	}
	item, err := worker.repository.GetQueueItem(ctx, request.QueueItemID)
	if err != nil {
		return queue.RuntimeBinding{}, false, err
	}
	submission, err := worker.repository.GetSubmission(ctx, item.Submission.ID)
	if err != nil || submission.Digest != item.Submission.Digest || submission.AuthorityKind != "registered-agent-workspace" || submission.Authority != workspace.Ref() || submission.OwnerAgentID != workspace.Agent.ID || submission.OwnerID != workspace.OwnerID {
		return queue.RuntimeBinding{}, false, fmt.Errorf("%w: cross-Agent queue binding denied", ErrWorkerDenied)
	}
	authority, mandate, ready := worker.service.resolveAuthority(ctx, request.Subject, request.Authority)
	if ready.State != ReadinessReady || authority.AgentID != workspace.Agent.ID {
		return queue.RuntimeBinding{}, false, fmt.Errorf("%w: exact same-Agent runtime authority required", ErrWorkerDenied)
	}
	participant, err := worker.repository.GetAgentRevision(ctx, workspace.Agent.ID, workspace.Agent.Revision)
	if err != nil || participant.Digest != workspace.Agent.Digest || !agentMatchesAuthority(participant, authority, mandate) {
		return queue.RuntimeBinding{}, false, fmt.Errorf("%w: runtime authority does not bind workspace Agent", ErrWorkerDenied)
	}
	now := worker.now()
	binding, err := queue.NewRuntimeBinding(queue.RuntimeBinding{BindingID: request.BindingID, QueueItem: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: item.ItemID, Digest: item.Digest}, Submission: item.Submission, OwnerAgent: workspace.Agent, Authority: request.Authority, MandateID: mandate.ID, Runtime: runtimeID(authority.Runtime), BoundAt: now})
	if err != nil {
		return queue.RuntimeBinding{}, false, err
	}
	transition, err := queue.NewTransition(queue.QueueTransition{TransitionID: request.TransitionID, QueueItemID: item.ItemID, From: queue.StateAwaitingRuntime, To: queue.StateQueued, Reason: "exact runtime authority bound", OccurredAt: now})
	if err != nil {
		return queue.RuntimeBinding{}, false, err
	}
	created, err := worker.repository.BindQueueRuntime(context.WithoutCancel(ctx), binding, transition, worker.service.auditFact("fleet.queue.runtime-bound", request.Subject, "exact same-Agent runtime authority bound", workspace.Agent.ID, authority.Authority.StanzaID, mandate.ID))
	return binding, created, err
}

func (worker *QueueWorker) Retry(ctx context.Context, request QueueRetryRequest) (queue.Retry, error) {
	if err := ctx.Err(); err != nil {
		return queue.Retry{}, err
	}
	if request.Backoff < 0 || request.Backoff > MaxRetryBackoff {
		return queue.Retry{}, fmt.Errorf("%w: retry backoff is out of bounds", ErrWorkerDenied)
	}
	action := FleetActionQueueLifecycle
	if request.Reclaimed {
		action = FleetActionReclaim
	}
	var item queue.Item
	var projection queue.Projection
	var claim queue.Claim
	var err error
	if request.Workspace != nil {
		item, projection, claim, err = worker.activeWorkspaceClaim(ctx, request.Subject, request.Authority, request.QueueItemID, request.Workspace)
	} else {
		item, projection, claim, err = worker.activeClaim(ctx, action, request.Subject, request.Authority, request.QueueItemID)
	}
	if err != nil {
		return queue.Retry{}, err
	}
	now := worker.now()
	leaseExpired := !now.Before(claim.ExpiresAt)
	reason := request.ReasonCode
	if request.Reclaimed && !leaseExpired {
		return queue.Retry{}, fmt.Errorf("%w: reclaim requires an expired lease", ErrWorkerDenied)
	}
	if !request.Reclaimed && leaseExpired {
		return queue.Retry{}, fmt.Errorf("%w: expired lease requires reclaim", ErrWorkerDenied)
	}
	if reason == "" && request.Reclaimed {
		reason = ReasonLeaseReclaimed
	} else if reason == "" {
		reason = ReasonRuntimeRetry
	}
	if (request.Reclaimed && reason != ReasonLeaseReclaimed) || (!request.Reclaimed && reason != ReasonRuntimeRetry) {
		return queue.Retry{}, fmt.Errorf("%w: invalid retry reason", ErrWorkerDenied)
	}
	if projection.Attempts >= item.MaxAttempts {
		return queue.Retry{}, fmt.Errorf("%w: retry budget exhausted", ErrWorkerDenied)
	}
	retry, err := queue.NewRetry(queue.Retry{RetryID: request.RetryID, QueueItem: claim.QueueItem, ClaimID: claim.ClaimID, AttemptNumber: projection.Attempts, AvailableAt: now.Add(request.Backoff), Reclaimed: request.Reclaimed, Reason: string(reason), OccurredAt: now})
	if err != nil {
		return queue.Retry{}, err
	}
	transition, err := queue.NewTransition(queue.QueueTransition{TransitionID: request.TransitionID, QueueItemID: item.ItemID, From: queue.StateClaimed, To: queue.StateQueued, ClaimID: claim.ClaimID, Reason: string(reason), OccurredAt: now})
	if err != nil {
		return queue.Retry{}, err
	}
	event := "fleet.queue.retried"
	if request.Reclaimed {
		event = "fleet.queue.reclaimed"
	}
	commitCtx := context.WithoutCancel(ctx)
	if err = worker.repository.RetryQueueItem(commitCtx, fleet.RetryMutation{Retry: retry, Transition: transition}, worker.service.auditFact(event, request.Subject, string(reason), "", "", "")); err != nil {
		return queue.Retry{}, err
	}
	return retry, nil
}

func (worker *QueueWorker) Cancel(ctx context.Context, request QueueTerminalRequest) (queue.Cancellation, error) {
	return worker.terminalize(ctx, request, queue.StateCancelled, ReasonOperatorCancelled)
}

func (worker *QueueWorker) Expire(ctx context.Context, request QueueTerminalRequest) (queue.Cancellation, error) {
	return worker.terminalize(ctx, request, queue.StateExpired, ReasonExecutionExpired)
}

func (worker *QueueWorker) Exhaust(ctx context.Context, request QueueTerminalRequest) (queue.Cancellation, error) {
	return worker.terminalize(ctx, request, queue.StateFailed, ReasonRetryExhausted)
}

func (worker *QueueWorker) Revoke(ctx context.Context, request QueueTerminalRequest) (queue.Cancellation, error) {
	return worker.terminalize(ctx, request, queue.StateRevoked, ReasonAuthorityRevoked)
}

func (worker *QueueWorker) terminalize(ctx context.Context, request QueueTerminalRequest, target queue.State, requiredReason QueueLifecycleReason) (queue.Cancellation, error) {
	// Cancellation before admission must never acquire authority. Only the
	// bounded atomic commit is detached after successful fresh admission.
	if err := ctx.Err(); err != nil {
		return queue.Cancellation{}, err
	}
	item, err := worker.repository.GetQueueItem(ctx, request.QueueItemID)
	if err != nil {
		return queue.Cancellation{}, err
	}
	if request.Authority != item.Authority {
		return queue.Cancellation{}, fmt.Errorf("%w: exact queue authority required", ErrWorkerDenied)
	}
	if request.ReasonCode != "" && request.ReasonCode != requiredReason {
		return queue.Cancellation{}, fmt.Errorf("%w: invalid terminal reason", ErrWorkerDenied)
	}
	if request.Workspace != nil {
		workspace, _, workspaceErr := worker.service.resolveWorkspace(ctx, request.Subject, request.Authority, request.Workspace, WorkspaceManageOwnQueue)
		if workspaceErr != nil {
			return queue.Cancellation{}, fmt.Errorf("%w: workspace authority denied", ErrWorkerDenied)
		}
		submission, loadErr := worker.repository.GetSubmission(ctx, item.Submission.ID)
		if loadErr != nil || submission.Digest != item.Submission.Digest || submission.Authority != item.Authority || submission.AuthorityKind != "registered-agent-workspace" || submission.OwnerAgentID != workspace.Agent.ID || submission.OwnerID != workspace.OwnerID {
			return queue.Cancellation{}, fmt.Errorf("%w: cross-Agent queue mutation denied", ErrWorkerDenied)
		}
	} else if err = worker.lifecycleAdmission(ctx, FleetActionQueueLifecycle, request.Subject, item); err != nil {
		return queue.Cancellation{}, err
	}
	commitCtx := context.WithoutCancel(ctx)
	projection, err := worker.repository.GetQueueProjection(commitCtx, item.ItemID)
	if err != nil {
		return queue.Cancellation{}, err
	}
	awaitingWorkspaceTerminal := request.Workspace != nil && projection.State == queue.StateAwaitingRuntime && (target == queue.StateCancelled || target == queue.StateRevoked)
	if projection.State != queue.StateQueued && projection.State != queue.StateClaimed && !awaitingWorkspaceTerminal {
		return queue.Cancellation{}, fmt.Errorf("%w: terminal queue item cannot transition", ErrWorkerDenied)
	}
	if target == queue.StateFailed && projection.Attempts < item.MaxAttempts {
		return queue.Cancellation{}, fmt.Errorf("%w: retry budget remains", ErrWorkerDenied)
	}
	if target == queue.StateFailed && projection.State != queue.StateClaimed {
		return queue.Cancellation{}, fmt.Errorf("%w: retry exhaustion requires the final active attempt", ErrWorkerDenied)
	}
	if target == queue.StateExpired {
		if projection.State != queue.StateClaimed || projection.ActiveClaimID == "" {
			return queue.Cancellation{}, fmt.Errorf("%w: expiry requires an active lease", ErrWorkerDenied)
		}
		claim, loadErr := worker.repository.GetClaim(commitCtx, projection.ActiveClaimID)
		if loadErr != nil || worker.now().Before(claim.ExpiresAt) {
			return queue.Cancellation{}, fmt.Errorf("%w: active lease has not expired", ErrWorkerDenied)
		}
	}
	claimID := projection.ActiveClaimID
	now := worker.now()
	reason := string(requiredReason)
	cancellation, err := queue.NewCancellation(queue.Cancellation{CancellationID: request.CancellationID, QueueItem: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: item.ItemID, Digest: item.Digest}, ClaimID: claimID, Reason: reason, OccurredAt: now})
	if err != nil {
		return queue.Cancellation{}, err
	}
	transition, err := queue.NewTransition(queue.QueueTransition{TransitionID: request.TransitionID, QueueItemID: item.ItemID, From: projection.State, To: target, ClaimID: claimID, Reason: reason, OccurredAt: now})
	if err != nil {
		return queue.Cancellation{}, err
	}
	loopExecutionID, attemptID := "", ""
	if claimID != "" {
		claim, loadErr := worker.repository.GetClaim(commitCtx, claimID)
		if loadErr != nil {
			return queue.Cancellation{}, loadErr
		}
		attempt, loadErr := worker.repository.GetAttempt(commitCtx, claim.AttemptID)
		if loadErr != nil || attempt.ClaimID != claimID || attempt.GraphRunID != item.GraphRunID {
			return queue.Cancellation{}, fmt.Errorf("%w: exact attempt unavailable", ErrWorkerDenied)
		}
		loopExecutionID, attemptID = attempt.LoopExecutionID, attempt.AttemptID
	}
	state := execution.StateCancelled
	if target == queue.StateExpired {
		state = execution.StateExpired
	} else if target == queue.StateFailed {
		state = execution.StateFailed
	} else if target == queue.StateRevoked {
		state = execution.StateRevoked
	}
	dispositionRecord, err := disposition.New(disposition.Record{DispositionID: request.CancellationID + "/disposition", GraphRunID: item.GraphRunID, LoopExecutionID: loopExecutionID, AttemptID: attemptID, QueueItem: cancellation.QueueItem, Authority: item.Authority, State: state, ReasonCode: reason, OccurredAt: now})
	if err != nil {
		return queue.Cancellation{}, err
	}
	mutation := fleet.CancellationMutation{Cancellation: cancellation, Transition: transition, Disposition: dispositionRecord}
	if err = worker.repository.CancelQueueItem(commitCtx, mutation, worker.service.auditFact("fleet.queue."+string(target), request.Subject, reason, "", "", "")); err != nil {
		return queue.Cancellation{}, err
	}
	return cancellation, nil
}

func (worker *QueueWorker) activeWorkspaceClaim(ctx context.Context, subject core.Subject, authority reference.DigestRef, itemID string, supplied *WorkspaceAuthority) (queue.Item, queue.Projection, queue.Claim, error) {
	item, err := worker.repository.GetQueueItem(ctx, itemID)
	if err != nil {
		return queue.Item{}, queue.Projection{}, queue.Claim{}, err
	}
	if item.Authority != authority {
		return queue.Item{}, queue.Projection{}, queue.Claim{}, fmt.Errorf("%w: exact queue authority required", ErrWorkerDenied)
	}
	workspace, _, err := worker.service.resolveWorkspace(ctx, subject, authority, supplied, WorkspaceManageOwnQueue)
	if err != nil {
		return queue.Item{}, queue.Projection{}, queue.Claim{}, fmt.Errorf("%w: workspace authority denied", ErrWorkerDenied)
	}
	submission, err := worker.repository.GetSubmission(ctx, item.Submission.ID)
	if err != nil || submission.Digest != item.Submission.Digest || submission.Authority != item.Authority || submission.AuthorityKind != "registered-agent-workspace" || submission.OwnerAgentID != workspace.Agent.ID || submission.OwnerID != workspace.OwnerID {
		return queue.Item{}, queue.Projection{}, queue.Claim{}, fmt.Errorf("%w: cross-Agent queue mutation denied", ErrWorkerDenied)
	}
	projection, err := worker.repository.GetQueueProjection(ctx, itemID)
	if err != nil {
		return queue.Item{}, queue.Projection{}, queue.Claim{}, err
	}
	if projection.State != queue.StateClaimed || projection.ActiveClaimID == "" {
		return queue.Item{}, queue.Projection{}, queue.Claim{}, fmt.Errorf("%w: queue item has no active claim", ErrWorkerDenied)
	}
	claim, err := worker.repository.GetClaim(ctx, projection.ActiveClaimID)
	return item, projection, claim, err
}

func (worker *QueueWorker) activeClaim(ctx context.Context, action FleetAction, subject core.Subject, authority reference.DigestRef, itemID string) (queue.Item, queue.Projection, queue.Claim, error) {
	item, err := worker.repository.GetQueueItem(ctx, itemID)
	if err != nil {
		return queue.Item{}, queue.Projection{}, queue.Claim{}, err
	}
	if item.Authority != authority {
		return queue.Item{}, queue.Projection{}, queue.Claim{}, fmt.Errorf("%w: exact queue authority required", ErrWorkerDenied)
	}
	if err = worker.lifecycleAdmission(ctx, action, subject, item); err != nil {
		return queue.Item{}, queue.Projection{}, queue.Claim{}, err
	}
	projection, err := worker.repository.GetQueueProjection(ctx, itemID)
	if err != nil {
		return queue.Item{}, queue.Projection{}, queue.Claim{}, err
	}
	if projection.State != queue.StateClaimed || projection.ActiveClaimID == "" {
		return queue.Item{}, queue.Projection{}, queue.Claim{}, fmt.Errorf("%w: queue item has no active claim", ErrWorkerDenied)
	}
	claim, err := worker.repository.GetClaim(ctx, projection.ActiveClaimID)
	return item, projection, claim, err
}

// lifecycleAdmission reconstructs the immutable queue item's exact definition
// and authority context and repeats authoritative admission before any write.
func (worker *QueueWorker) lifecycleAdmission(ctx context.Context, action FleetAction, subject core.Subject, item queue.Item) error {
	snapshot, err := worker.repository.GetGraphRunSnapshot(ctx, item.Snapshot.ID)
	if err != nil || snapshot.Digest != item.Snapshot.Digest {
		return fmt.Errorf("%w: exact snapshot unavailable", ErrWorkerDenied)
	}
	revision, err := worker.repository.GetGraphRevision(ctx, snapshot.Graph.ID, snapshot.Graph.Revision)
	if err != nil || revision.Digest != snapshot.Graph.Digest || len(revision.Nodes) != 1 {
		return fmt.Errorf("%w: exact graph unavailable", ErrWorkerDenied)
	}
	node := revision.Nodes[0]
	ready := worker.service.Readiness(ctx, ReadinessRequest{Action: action, Subject: subject, Authority: item.Authority, Agent: node.Participant, Loop: node.Loop, Graph: snapshot.Graph})
	if ready.State != ReadinessReady {
		return fmt.Errorf("%w: lifecycle %s", ErrWorkerDenied, ready.ReasonCode)
	}
	return nil
}
