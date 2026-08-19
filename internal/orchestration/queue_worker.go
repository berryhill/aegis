package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/disposition"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	queue "github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

var ErrWorkerDenied = errors.New("queue worker denied")

const (
	MinLeaseDuration = time.Second
	MaxLeaseDuration = time.Hour
)

// RuntimeAdapter is deliberately narrower than the Hermes adapter. It receives
// only immutable execution input after controller-side admission and returns
// untrusted bytes. It cannot select identity, authority, credentials, or stanza.
type RuntimeAdapter interface {
	Execute(context.Context, RuntimeRequest) (RuntimeResult, error)
}

type RuntimeRequest struct {
	GraphRunID      string
	LoopExecutionID string
	GraphNodeID     string
	Authority       reference.DigestRef
	Inputs          []graph.NormalizedInput
	Participant     registry.AgentRevision
	Launch          execution.LaunchContract
	Admission       execution.AdmissionChecker
}

type RuntimeResult struct {
	Output    []byte
	MediaType string
}

type BlobStore interface {
	PutBlob([]byte) (string, error)
	GetBlob(string) ([]byte, error)
}

type EvidenceVerifier interface {
	Verify(context.Context, evidence.RuntimeArtifact, evidence.VerificationPolicy) (evidence.VerificationReceipt, error)
	AuthorizeCompletion(context.Context, evidence.RuntimeArtifact, []evidence.VerificationReceipt) (evidence.CompletionProvenance, error)
}

// NoKeyAdapter is the credential-independent deterministic MVI adapter. It
// emits canonical normalized Graph inputs and performs no network, provider,
// credential, shell, or filesystem effect.
type NoKeyAdapter struct{}

func (NoKeyAdapter) Execute(ctx context.Context, request RuntimeRequest) (RuntimeResult, error) {
	if err := ctx.Err(); err != nil {
		return RuntimeResult{}, err
	}
	if request.GraphRunID == "" || request.LoopExecutionID == "" || request.GraphNodeID == "" || request.Authority.Validate() != nil || request.Participant.Runtime.Adapter != "no-key" {
		return RuntimeResult{}, errors.New("exact no-key runtime binding is required")
	}
	wire, err := json.Marshal(request.Inputs)
	if err != nil {
		return RuntimeResult{}, err
	}
	return RuntimeResult{Output: wire, MediaType: "application/json"}, nil
}

type QueueWorker struct {
	repository fleet.Repository
	service    *FleetService
	blobs      BlobStore
	verifier   EvidenceVerifier
	adapter    RuntimeAdapter
	now        func() time.Time
}

func NewQueueWorker(repository fleet.Repository, service *FleetService, blobs BlobStore, verifier EvidenceVerifier, adapter RuntimeAdapter, now func() time.Time) (*QueueWorker, error) {
	if repository == nil || service == nil || blobs == nil || verifier == nil || adapter == nil {
		return nil, errors.New("fleet repository, service, blob store, verifier, and runtime adapter are required")
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &QueueWorker{repository: repository, service: service, blobs: blobs, verifier: verifier, adapter: adapter, now: now}, nil
}

type WorkRequest struct {
	Subject              core.Subject        `json:"-"`
	Authority            reference.DigestRef `json:"authority"`
	QueueItemID          string              `json:"queue_item_id"`
	WorkerID             string              `json:"worker_id"`
	LoopExecutionID      string              `json:"loop_execution_id"`
	ClaimID              string              `json:"claim_id"`
	AttemptID            string              `json:"attempt_id"`
	ClaimTransitionID    string              `json:"claim_transition_id"`
	TerminalTransitionID string              `json:"terminal_transition_id"`
	DispositionID        string              `json:"disposition_id"`
	ArtifactID           string              `json:"artifact_id"`
	LeaseDuration        time.Duration       `json:"lease_duration"`
}

type WorkResult struct {
	Claim       queue.Claim                    `json:"claim"`
	Attempt     execution.Attempt              `json:"attempt"`
	Artifact    *evidence.RuntimeArtifact      `json:"artifact,omitempty"`
	Receipts    []evidence.VerificationReceipt `json:"receipts"`
	Disposition disposition.Record             `json:"disposition"`
}

// Process executes the narrow one-node MVI graph. Unsupported graph shapes
// deny before claim rather than guessing an execution order.
func (worker *QueueWorker) Process(ctx context.Context, request WorkRequest) (WorkResult, error) {
	item, err := worker.repository.GetQueueItem(ctx, request.QueueItemID)
	if err != nil {
		return WorkResult{}, err
	}
	projection, err := worker.repository.GetQueueProjection(ctx, item.ItemID)
	if err != nil || projection.State != queue.StateQueued || projection.Attempts >= item.MaxAttempts || worker.now().Before(projection.AvailableAt) {
		return WorkResult{}, fmt.Errorf("%w: queue item is not claimable", ErrWorkerDenied)
	}
	if item.Authority != request.Authority || request.WorkerID == "" || request.LeaseDuration < MinLeaseDuration || request.LeaseDuration > MaxLeaseDuration {
		return WorkResult{}, fmt.Errorf("%w: exact queue and worker binding required", ErrWorkerDenied)
	}
	snapshot, err := worker.repository.GetGraphRunSnapshot(ctx, item.Snapshot.ID)
	if err != nil || snapshot.Digest != item.Snapshot.Digest {
		return WorkResult{}, fmt.Errorf("%w: exact snapshot unavailable", ErrWorkerDenied)
	}
	graphRevision, err := worker.repository.GetGraphRevision(ctx, snapshot.Graph.ID, snapshot.Graph.Revision)
	if err != nil || graphRevision.Digest != snapshot.Graph.Digest || len(graphRevision.Nodes) != 1 {
		return WorkResult{}, fmt.Errorf("%w: exact single-node graph required", ErrWorkerDenied)
	}
	node := graphRevision.Nodes[0]
	participant, err := worker.repository.GetAgentRevision(ctx, node.Participant.ID, node.Participant.Revision)
	if err != nil || participant.Digest != node.Participant.Digest || participant.Lifecycle != registry.LifecycleEnabled {
		return WorkResult{}, fmt.Errorf("%w: exact enabled participant unavailable", ErrWorkerDenied)
	}
	loopRevision, err := worker.repository.GetLoopRevision(ctx, node.Loop.ID, node.Loop.Revision)
	if err != nil || loopRevision.Digest != node.Loop.Digest {
		return WorkResult{}, fmt.Errorf("%w: exact Loop revision unavailable", ErrWorkerDenied)
	}
	actionID, err := executableAction(loopRevision)
	if err != nil {
		return WorkResult{}, fmt.Errorf("%w: %v", ErrWorkerDenied, err)
	}
	if readiness := worker.service.Readiness(ctx, ReadinessRequest{Action: FleetActionClaim, Subject: request.Subject, Authority: request.Authority, Agent: node.Participant, Loop: node.Loop, Graph: snapshot.Graph}); readiness.State != ReadinessReady {
		return WorkResult{}, fmt.Errorf("%w: claim %s", ErrWorkerDenied, readiness.ReasonCode)
	}
	now := worker.now()
	// One LoopExecution identifies this immutable Graph node across all bounded
	// attempts. A retry may add an Attempt, but cannot substitute a child identity.
	var loopExecution execution.LoopExecution
	loopExecutions, err := worker.repository.ListLoopExecutions(ctx)
	if err != nil {
		return WorkResult{}, err
	}
	for _, candidate := range loopExecutions {
		if candidate.GraphRunID == item.GraphRunID && candidate.GraphNodeID == node.ID {
			if loopExecution.LoopExecutionID != "" || candidate.Loop != node.Loop || candidate.Participant != node.Participant || candidate.LoopExecutionID != request.LoopExecutionID {
				return WorkResult{}, fmt.Errorf("%w: exact Loop execution binding required", ErrWorkerDenied)
			}
			loopExecution = candidate
		}
	}
	if loopExecution.LoopExecutionID == "" {
		loopExecution, err = execution.NewLoopExecution(execution.LoopExecution{LoopExecutionID: request.LoopExecutionID, GraphRunID: item.GraphRunID, GraphNodeID: node.ID, Loop: node.Loop, Participant: node.Participant, CreatedAt: now})
		if err != nil {
			return WorkResult{}, err
		}
		if _, err = worker.repository.CreateLoopExecution(ctx, loopExecution, worker.service.auditFact("fleet.loop-execution.created", request.Subject, "created exact Loop execution", node.Participant.ID, "", "")); err != nil {
			return WorkResult{}, err
		}
	}
	claim, err := queue.NewClaim(queue.Claim{ClaimID: request.ClaimID, QueueItem: digestRef(item.ItemID, item.Digest), AttemptID: request.AttemptID, WorkerID: request.WorkerID, Authority: request.Authority, ClaimedAt: now, ExpiresAt: now.Add(request.LeaseDuration)})
	if err != nil {
		return WorkResult{}, err
	}
	attempt, err := execution.NewAttempt(execution.Attempt{AttemptID: request.AttemptID, GraphRunID: item.GraphRunID, LoopExecutionID: loopExecution.LoopExecutionID, QueueItem: claim.QueueItem, ClaimID: claim.ClaimID, AttemptNumber: projection.Attempts + 1, CreatedAt: now})
	if err != nil {
		return WorkResult{}, err
	}
	claimTransition, err := queue.NewTransition(queue.QueueTransition{TransitionID: request.ClaimTransitionID, QueueItemID: item.ItemID, From: queue.StateQueued, To: queue.StateClaimed, ClaimID: claim.ClaimID, Reason: "worker lease acquired", OccurredAt: now})
	if err != nil {
		return WorkResult{}, err
	}
	if err = worker.repository.ClaimQueueItem(ctx, claim, attempt, claimTransition, worker.service.auditFact("fleet.queue.claimed", request.Subject, "worker lease acquired", node.Participant.ID, "", "")); err != nil {
		return WorkResult{}, err
	}
	base := WorkResult{Claim: claim, Attempt: attempt}

	if readiness := worker.service.Readiness(ctx, ReadinessRequest{Action: FleetActionRuntimeEffect, Subject: request.Subject, Authority: request.Authority, Agent: node.Participant, Loop: node.Loop, Graph: snapshot.Graph}); readiness.State != ReadinessReady {
		return worker.terminal(ctx, request, base, execution.StateDenied, "runtime_admission_denied", nil, nil)
	}
	authority, mandate, runtimeReadiness := worker.service.resolveAuthority(ctx, request.Subject, request.Authority)
	if runtimeReadiness.State != ReadinessReady || !agentMatchesAuthority(participant, authority, mandate) {
		return worker.terminal(ctx, request, base, execution.StateDenied, "runtime_binding_denied", nil, nil)
	}
	finished := now
	launch := execution.LaunchContract{
		OwnerID:          participant.Ownership.OwnerID,
		Mandate:          mandate,
		AuthorityContext: authority,
		ParentDispatch:   execution.Dispatch{ID: request.AttemptID, AuthorityContextID: authority.ID, State: execution.StateSucceeded, RequestedAt: now, StartedAt: &now, FinishedAt: &finished},
	}
	// Runtime custody is bounded by the immutable claim lease. WithDeadline also
	// preserves an earlier caller deadline, so neither bound can be extended.
	runtimeCtx, cancelRuntime := context.WithDeadline(ctx, claim.ExpiresAt)
	defer cancelRuntime()
	runtimeResult, runtimeErr := worker.adapter.Execute(runtimeCtx, RuntimeRequest{
		GraphRunID: item.GraphRunID, LoopExecutionID: loopExecution.LoopExecutionID, GraphNodeID: node.ID,
		Authority: request.Authority, Inputs: snapshot.Inputs, Participant: participant, Launch: launch,
		Admission: fleetRuntimeAdmission{service: worker.service, subject: request.Subject, authority: request.Authority},
	})
	if runtimeErr != nil {
		state, reason := execution.StateFailed, "runtime_effect_failed"
		var failure *RuntimeFailure
		if errors.As(runtimeErr, &failure) && executionQueueState(failure.State) != "" && failure.Reason != "" {
			state, reason = failure.State, failure.Reason
		}
		return worker.terminal(ctx, request, base, state, reason, nil, nil)
	}
	if len(runtimeResult.Output) == 0 || runtimeResult.MediaType == "" {
		return worker.terminal(ctx, request, base, execution.StateFailed, "runtime_output_invalid", nil, nil)
	}
	contentRef, err := worker.blobs.PutBlob(runtimeResult.Output)
	if err != nil {
		return worker.terminal(ctx, request, base, execution.StateFailed, "artifact_persistence_failed", nil, nil)
	}
	artifact := evidence.RuntimeArtifact{ID: request.ArtifactID, AttemptID: attempt.AttemptID, OwnerID: node.Participant.ID, ActionID: actionID, RunID: loopExecution.LoopExecutionID, AuthorityContextID: request.Authority.ID, AuthorityContextDigest: request.Authority.Digest, Digest: contentRef, ContentRef: contentRef, MediaType: runtimeResult.MediaType, CreatedAt: worker.now()}
	if err = artifact.Validate(); err != nil {
		return WorkResult{}, err
	}
	receipts := make([]evidence.VerificationReceipt, 0, len(loopRevision.RequiredEvidence))
	claims := evidenceClaims(loopRevision, actionID)
	for _, requirement := range loopRevision.RequiredEvidence {
		if readiness := worker.service.Readiness(ctx, ReadinessRequest{Action: FleetActionEvidenceVerify, Subject: request.Subject, Authority: request.Authority, Agent: node.Participant, Loop: node.Loop, Graph: snapshot.Graph}); readiness.State != ReadinessReady {
			return worker.terminal(ctx, request, base, execution.StateDenied, "evidence_admission_denied", nil, nil)
		}
		claim, found := claims[requirement.Claim]
		if !found {
			return worker.terminal(ctx, request, base, execution.StateFailed, "evidence_policy_missing", nil, nil)
		}
		policy := evidence.VerificationPolicy{VerifierID: claim.VerifierID, PolicyVersion: claim.PolicyVersion, Claim: claim.Claim, MediaType: claim.MediaType, ExpectedDigest: claim.ExpectedDigest}
		receipt, verifyErr := worker.verifier.Verify(ctx, artifact, policy)
		validReceipt := receipt.Validate() == nil && receipt.AttemptID == artifact.AttemptID && receipt.ArtifactID == artifact.ID && receipt.ActionID == artifact.ActionID && receipt.RunID == artifact.RunID && receipt.OwnerID == artifact.OwnerID && receipt.AuthorityContextID == artifact.AuthorityContextID && receipt.AuthorityContextDigest == artifact.AuthorityContextDigest && receipt.VerifierID == policy.VerifierID && receipt.PolicyVersion == policy.PolicyVersion && receipt.Claim == policy.Claim && receipt.MediaType == policy.MediaType && receipt.ExpectedDigest == policy.ExpectedDigest && receipt.ObservedDigest == artifact.Digest
		if validReceipt {
			receipts = append(receipts, receipt)
		}
		if verifyErr != nil || !validReceipt || receipt.Outcome != evidence.Passed {
			return worker.terminal(ctx, request, base, execution.StateFailed, "evidence_verification_failed", &artifact, receipts)
		}
	}
	base.Artifact = &artifact
	base.Receipts = receipts
	return worker.terminal(ctx, request, base, execution.StateSucceeded, "evidence_satisfied", &artifact, receipts)
}

func (worker *QueueWorker) terminal(ctx context.Context, request WorkRequest, result WorkResult, state execution.State, reason string, artifact *evidence.RuntimeArtifact, receipts []evidence.VerificationReceipt) (WorkResult, error) {
	// A request cancelled before disposition admission cannot gain authority.
	// Detachment is reserved for the bounded repository commit below.
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if readiness := worker.service.Readiness(ctx, ReadinessRequest{Action: FleetActionDisposition, Subject: request.Subject, Authority: request.Authority}); readiness.State != ReadinessReady {
		state, reason, artifact, receipts = execution.StateDenied, "disposition_admission_denied", nil, nil
	}
	artifactIDs := []string{}
	if artifact != nil {
		artifactIDs = append(artifactIDs, artifact.ID)
	}
	receiptIDs := make([]string, len(receipts))
	for index := range receipts {
		receiptIDs[index] = receipts[index].ID
	}
	dispositionRecord, err := disposition.New(disposition.Record{DispositionID: request.DispositionID, GraphRunID: result.Attempt.GraphRunID, LoopExecutionID: result.Attempt.LoopExecutionID, AttemptID: result.Attempt.AttemptID, QueueItem: result.Attempt.QueueItem, Authority: request.Authority, State: state, ReasonCode: reason, ArtifactIDs: artifactIDs, ReceiptIDs: receiptIDs, OccurredAt: worker.now()})
	if err != nil {
		return result, err
	}
	transition, err := queue.NewTransition(queue.QueueTransition{TransitionID: request.TerminalTransitionID, QueueItemID: result.Attempt.QueueItem.ID, From: queue.StateClaimed, To: executionQueueState(state), ClaimID: result.Claim.ClaimID, Reason: reason, OccurredAt: dispositionRecord.OccurredAt})
	if err != nil {
		return result, err
	}
	completion := fleet.Completion{Claim: result.Claim, Artifact: artifact, Receipts: receipts, Disposition: dispositionRecord, Transition: transition}
	if artifact != nil {
		completion.Provenance, err = worker.verifier.AuthorizeCompletion(ctx, *artifact, receipts)
		if err != nil {
			return result, err
		}
	}
	authority, mandate, identityOK := worker.service.authorityIdentity(ctx, request.Authority)
	if !identityOK {
		return result, fmt.Errorf("%w: terminal authority identity unavailable", ErrWorkerDenied)
	}
	fact := worker.service.auditFact("fleet.disposition.recorded", request.Subject, reason, authority.AgentID, authority.Authority.StanzaID, mandate.ID)
	fact.Event.Outcome = string(state)
	fact.Event.Metadata = map[string]string{
		"disposition_id": dispositionRecord.DispositionID,
		"transition_id":  transition.TransitionID,
		"terminal_at":    dispositionRecord.OccurredAt.UTC().Format(time.RFC3339Nano),
	}
	commitCtx := context.WithoutCancel(ctx)
	if err = worker.repository.CompleteQueueItem(commitCtx, completion, fact, worker.blobs); err != nil {
		return result, err
	}
	result.Artifact, result.Receipts, result.Disposition = artifact, receipts, dispositionRecord
	if state != execution.StateSucceeded {
		return result, fmt.Errorf("%w: %s", ErrWorkerDenied, reason)
	}
	return result, nil
}

type fleetRuntimeAdmission struct {
	service   *FleetService
	subject   core.Subject
	authority reference.DigestRef
}

func (check fleetRuntimeAdmission) CheckRuntimeAdmission(ctx context.Context, contract execution.LaunchContract, at time.Time) (execution.AdmissionDecision, error) {
	authority, mandate, result := check.service.resolveAuthority(ctx, check.subject, check.authority)
	allowed := result.State == ReadinessReady && authority.ID == contract.AuthorityContext.ID && authority.Digest == contract.AuthorityContext.Digest && mandate.ID == contract.Mandate.ID && at.Before(authority.ExpiresAt)
	return execution.AdmissionDecision{Allowed: allowed, AuthorityContextID: authority.ID, AuthorityContextDigest: authority.Digest, CheckedAt: at, Reason: result.ReasonCode}, nil
}

func evidenceClaims(revision loop.LoopRevision, actionID string) map[string]loop.EvidenceClaim {
	result := map[string]loop.EvidenceClaim{}
	for _, step := range revision.Steps {
		if step.ID != actionID {
			continue
		}
		for _, claim := range step.EvidenceClaims {
			result[claim.Claim] = claim
		}
	}
	return result
}

func executableAction(revision loop.LoopRevision) (string, error) {
	if len(revision.RequiredEvidence) == 0 {
		return "", errors.New("executable Loop requires at least one precommitted evidence policy")
	}
	actions := make(map[string]loop.Step)
	for _, step := range revision.Steps {
		if step.Kind == loop.StepAction {
			actions[step.ID] = step
		}
	}
	if len(actions) != 1 {
		return "", errors.New("narrow worker requires exactly one action step")
	}
	var actionID string
	for id := range actions {
		actionID = id
	}
	for _, requirement := range revision.RequiredEvidence {
		if requirement.ProducerStepID != actionID {
			return "", errors.New("evidence requirement is not produced by the executable action")
		}
		declared := false
		for _, claim := range actions[actionID].EvidenceClaims {
			if claim.Claim == requirement.Claim {
				declared = true
				break
			}
		}
		if !declared {
			return "", errors.New("required evidence claim is not declared by the executable action")
		}
	}
	return actionID, nil
}

func executionQueueState(state execution.State) queue.State {
	switch state {
	case execution.StateSucceeded:
		return queue.StateSucceeded
	case execution.StateFailed:
		return queue.StateFailed
	case execution.StateDenied:
		return queue.StateDenied
	case execution.StateCancelled:
		return queue.StateCancelled
	case execution.StateExpired:
		return queue.StateExpired
	case execution.StateRevoked:
		return queue.StateRevoked
	default:
		return ""
	}
}
