package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
)

// RequeueQueueItemInput selects old immutable history and a new stable intent.
// Task, inputs, definition pins, evidence and budget are never caller inputs.
type RequeueQueueItemInput struct {
	QueueItem      reference.DigestRef `json:"queue_item"`
	IdempotencyKey string              `json:"idempotency_key"`
}

func requeueDenied(reason string) error { return fmt.Errorf("%w: %s", ErrDenied, reason) }

// reconstructQueueRequeue copies only canonical snapshot data. Detailed retained
// binding and cumulative-budget checks are repeated by deriveLoopReexecution.
func reconstructQueueRequeue(sub core.Subject, in RequeueQueueItemInput, old QueueExecutionView, snapshot graph.GraphRunSnapshot, topology graph.ExecutionTopology) (QueueLoopInput, error) {
	var out QueueLoopInput
	if in.QueueItem.Validate() != nil || in.IdempotencyKey == "" || len(in.IdempotencyKey) > 256 {
		return out, requeueDenied("requeue_invalid_selector")
	}
	if old.Item.ItemID != in.QueueItem.ID || old.Item.Digest != in.QueueItem.Digest || old.Submission.OwnerID != sub.PrincipalID {
		return out, requeueDenied("requeue_source_mismatch")
	}
	if old.Projection.State != queue.StateFailed || old.Projection.ActiveClaimID != "" {
		return out, requeueDenied("requeue_terminal_failure_required")
	}
	d := old.Disposition
	if d == nil || d.State != execution.StateFailed || d.ReasonCode != "doer_loop_failed" {
		return out, requeueDenied("requeue_failure_unsupported")
	}
	// QueueLoop creates direct runs; it cannot preserve a published Graph source.
	if snapshot.SchemaVersion != graph.DirectRunSnapshotSchemaVersion || snapshot.Execution == nil || snapshot.Execution.SourceKind != graph.SourceDirectLoop || snapshot.Graph != (reference.RevisionRef{}) {
		return out, requeueDenied("requeue_graph_source_unsupported")
	}
	if len(old.Attempts) != 1 || len(old.LoopExecutions) != 1 || len(topology.Nodes) != 1 || old.RuntimeAuthorityBinding == nil {
		return out, requeueDenied("requeue_history_unsupported")
	}
	if snapshot.SnapshotID != old.Item.Snapshot.ID || snapshot.Digest != old.Item.Snapshot.Digest {
		return out, requeueDenied("requeue_snapshot_mismatch")
	}
	node := topology.Nodes[0]
	// Re-seal with the original ID to prove normalized inputs and topology have
	// exactly the retained semantics, before a new intent can be reserved.
	direct, err := graph.NewDirectLoopSnapshot(snapshot.SnapshotID, node, snapshot.Inputs)
	if err != nil {
		return out, requeueDenied("requeue_snapshot_invalid")
	}
	rebuilt, err := graph.NewDirectRunSnapshot(direct)
	if err != nil || rebuilt.Digest != snapshot.Digest {
		return out, requeueDenied("requeue_snapshot_mismatch")
	}
	inputs := append([]graph.NormalizedInput(nil), snapshot.Inputs...)
	for i := range inputs {
		inputs[i].Value = append([]byte(nil), inputs[i].Value...)
	}
	out = QueueLoopInput{Agent: node.Participant, Loop: node.Loop, Inputs: inputs, IdempotencyKey: in.IdempotencyKey,
		Reexecution: &LoopReexecution{PreviousQueue: in.QueueItem, PreviousDisposition: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: d.DispositionID, Digest: d.Digest}}}
	return out, nil
}

func (s *Service) RequeueQueueItem(ctx context.Context, in RequeueQueueItemInput) (QueueLoopResult, error) {
	sub, err := s.Authenticate(ctx)
	if err != nil {
		return QueueLoopResult{}, err
	}
	return s.RequeueQueueItemAs(ctx, sub, in)
}

// RequeueQueueItemAs never reopens the old Queue. QueueLoopAs provides fresh
// admission, exact successor reservation, remaining budget and runtime checks.
// No old authority context, artifact or receipt is copied into the new intent.
func (s *Service) RequeueQueueItemAs(ctx context.Context, sub core.Subject, in RequeueQueueItemInput) (QueueLoopResult, error) {
	return s.queueRequeue(ctx, sub, in, true)
}

// CheckQueueRequeueAs checks eligibility without reserving or queueing work.
func (s *Service) CheckQueueRequeueAs(ctx context.Context, sub core.Subject, ref reference.DigestRef) (QueueLoopResult, error) {
	return s.queueRequeue(ctx, sub, RequeueQueueItemInput{QueueItem: ref, IdempotencyKey: "console-eligibility"}, false)
}

func (s *Service) queueRequeue(ctx context.Context, sub core.Subject, in RequeueQueueItemInput, execute bool) (QueueLoopResult, error) {
	if err := s.requireFleetPrincipal(sub); err != nil {
		return QueueLoopResult{Reason: "requeue_fresh_authority_required"}, err
	}
	input, _, err := s.PrepareQueueRequeueAs(ctx, sub, in)
	if err != nil {
		reason := "requeue_retained_bindings_or_kind_unsupported"
		if errors.Is(err, ErrDoerReexecutionBudgetExhausted) {
			reason = "doer_reexecution_budget_exhausted"
		}
		return QueueLoopResult{Reason: reason}, err
	}
	// Read-only eligibility must check the requester too, not merely historical
	// shape and budget. Rendering a control is not an execution-auth bridge.
	// A requester that cannot select the exact retained execution charter must
	// not consume the predecessor's single successor slot. In particular, a
	// Console password session is never relabeled as a local-OS executor.
	agent, err := s.FleetRepository.GetAgentRevision(ctx, input.Agent.ID, input.Agent.Revision)
	if err != nil {
		return QueueLoopResult{}, err
	}
	if agent.Digest != input.Agent.Digest {
		return QueueLoopResult{}, ErrDenied
	}
	charter, err := s.GetCharter(agent.Charter.ID, agent.Charter.Revision)
	if err != nil {
		return QueueLoopResult{}, err
	}
	if charter.Digest != agent.Charter.Digest {
		return QueueLoopResult{}, ErrDenied
	}
	selection, selectionErr := s.Select(charter, sub, "", core.Environment{Name: "local"})
	if selectionErr != nil || selection.Selected == nil {
		result := QueueLoopResult{}
		result.blockedDoer("session_selection_" + selection.Reason)
		return result, nil
	}
	if !execute {
		return QueueLoopResult{Reason: "eligible_fresh_admission_required"}, nil
	}
	return s.QueueLoopAs(ctx, sub, input)
}

// PrepareQueueRequeueAs reconstructs a non-authorizing review, never reserves a successor.
type QueueRequeueBinding struct {
	Run                 QueueLoopInput
	Checkpoint          string
	Consumed, Remaining uint16
}

func (s *Service) PrepareQueueRequeueAs(ctx context.Context, sub core.Subject, in RequeueQueueItemInput) (QueueLoopInput, QueueRequeueBinding, error) {
	if err := s.requireFleetPrincipal(sub); err != nil {
		return QueueLoopInput{}, QueueRequeueBinding{}, err
	}
	if in.QueueItem.Validate() != nil || in.IdempotencyKey == "" || len(in.IdempotencyKey) > 256 {
		return QueueLoopInput{}, QueueRequeueBinding{}, requeueDenied("requeue_invalid_selector")
	}
	old, err := s.GetQueueItemAs(ctx, sub, in.QueueItem.ID)
	if err != nil {
		return QueueLoopInput{}, QueueRequeueBinding{}, err
	}
	snapshot, err := s.FleetRepository.GetGraphRunSnapshot(ctx, old.Item.Snapshot.ID)
	if err != nil {
		return QueueLoopInput{}, QueueRequeueBinding{}, err
	}
	// Do not reinterpret historical Graphs as direct loops, including single-node Graphs.
	if snapshot.SchemaVersion != graph.DirectRunSnapshotSchemaVersion {
		return QueueLoopInput{}, QueueRequeueBinding{}, requeueDenied("requeue_graph_source_unsupported")
	}
	topology, err := s.Fleet.ResolveRunTopology(ctx, snapshot)
	if err != nil {
		return QueueLoopInput{}, QueueRequeueBinding{}, err
	}
	input, err := reconstructQueueRequeue(sub, in, old, snapshot, topology)
	if err != nil {
		return QueueLoopInput{}, QueueRequeueBinding{}, err
	}
	view, err := s.GetLoopViewAs(ctx, sub, input.Loop.ID, input.Loop.Revision)
	if err != nil {
		return QueueLoopInput{}, QueueRequeueBinding{}, err
	}
	if view.Revision.Digest != input.Loop.Digest {
		return QueueLoopInput{}, QueueRequeueBinding{}, ErrDenied
	}
	// Read-only eligibility check before reserving a successor. The QueueLoop
	// admission repeats this check under its own serialization boundary.
	ctx, err = bindDoerRun(ctx, input, view.Revision)
	if err != nil {
		return QueueLoopInput{}, QueueRequeueBinding{}, err
	}
	fact, err := s.deriveLoopReexecution(ctx, sub, input, queueLoopRequestID(sub, input), "", view.Revision)
	if err != nil {
		return QueueLoopInput{}, QueueRequeueBinding{}, err
	}
	return input, QueueRequeueBinding{input, fact.CheckpointDigest, fact.Consumed, fact.Remaining}, nil
}
