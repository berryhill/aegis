package app

import (
	"context"
	"errors"
	"os"

	"github.com/berryhill/aegis/internal/continuation"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	"github.com/berryhill/aegis/internal/store"
)

// LoopReexecution selects an authoritative known failure, never an unknown
// outcome or a retry of its terminal GraphRun. Counts are not caller inputs.
type LoopReexecution = continuation.Reexecution

var ErrDoerReexecutionBudgetExhausted = errors.New("doer_reexecution_budget_exhausted")

// LoopReexecutionFact is an append-only operation intent, not a new definition
// or runtime authority. The previous Queue can have exactly one successor.
type LoopReexecutionFact struct {
	RequestID        string                `json:"request_id"`
	RequestDigest    string                `json:"request_digest"`
	SubjectID        string                `json:"subject_id"`
	PrincipalID      string                `json:"principal_id"`
	Agent            reference.RevisionRef `json:"agent"`
	Loop             reference.RevisionRef `json:"loop"`
	Previous         LoopReexecution       `json:"previous"`
	CheckpointDigest string                `json:"checkpoint_digest"`
	Consumed         uint16                `json:"consumed"`
	Remaining        uint16                `json:"remaining"`
	Digest           string                `json:"digest"`
}

func reexecutionDigest(f LoopReexecutionFact) string { f.Digest = ""; return core.Digest(f) }

// Conservative counting includes an Implement visit even when proposal/Apply
// failed. Missing, unfinished or inconsistent history is not a zero-pass run.
func consumedDoerPasses(steps []orchestration.DoerStepView) (uint16, string, error) {
	if len(steps) == 0 || !steps[len(steps)-1].Terminal {
		return 0, "", ErrDenied
	}
	var count uint32
	for _, step := range steps {
		if step.StepID == "implement" && step.Visit > count {
			count = step.Visit
		}
	}
	if count == 0 || count > 3 {
		return 0, "", ErrDenied
	}
	return uint16(count), steps[len(steps)-1].Digest, nil
}

func (s *Service) deriveLoopReexecution(ctx context.Context, sub core.Subject, in QueueLoopInput, requestID, identity string, revision loop.LoopRevision) (LoopReexecutionFact, error) {
	var f LoopReexecutionFact
	if in.Reexecution == nil || in.Reexecution.PreviousQueue.Validate() != nil || in.Reexecution.PreviousDisposition.Validate() != nil || revision.SchemaVersion != loop.DoerRevisionSchemaVersion || revision.Doer == nil || requestID+"-queue" == in.Reexecution.PreviousQueue.ID {
		return f, ErrDenied
	}
	v, err := s.GetQueueItemAs(ctx, sub, in.Reexecution.PreviousQueue.ID)
	if err != nil {
		return f, err
	}
	d := v.Disposition
	if v.Item.Digest != in.Reexecution.PreviousQueue.Digest || v.Projection.State != queue.StateFailed || v.Projection.ActiveClaimID != "" || d == nil || d.State != execution.StateFailed || d.ReasonCode != "doer_loop_failed" || d.DispositionID != in.Reexecution.PreviousDisposition.ID || d.Digest != in.Reexecution.PreviousDisposition.Digest || d.QueueItem != in.Reexecution.PreviousQueue || d.GraphRunID != v.Item.GraphRunID || v.Submission.OwnerAgentID != in.Agent.ID || v.Submission.OwnerID != sub.PrincipalID || len(v.Attempts) != 1 || len(v.LoopExecutions) != 1 || v.RuntimeAuthorityBinding == nil {
		return f, ErrDenied
	}
	child, attempt := v.LoopExecutions[0], v.Attempts[0]
	if child.Loop != in.Loop || child.Participant != in.Agent || child.GraphRunID != d.GraphRunID || child.LoopExecutionID != d.LoopExecutionID || attempt.AttemptID != d.AttemptID || attempt.LoopExecutionID != child.LoopExecutionID || attempt.GraphRunID != d.GraphRunID {
		return f, ErrDenied
	}
	snapshot, err := s.FleetRepository.GetGraphRunSnapshot(ctx, v.Item.Snapshot.ID)
	if err != nil || snapshot.Digest != v.Item.Snapshot.Digest {
		return f, ErrDenied
	}
	topology, err := s.Fleet.ResolveRunTopology(ctx, snapshot)
	if err != nil || len(topology.Nodes) != 1 || topology.Nodes[0].Loop != in.Loop || topology.Nodes[0].Participant != in.Agent {
		return f, ErrDenied
	}
	// Reconstruct through the original source kind: direct runs have no
	// published Graph. Preserve historical Graph snapshot/input semantics.
	var inputSnapshot graph.GraphRunSnapshot
	if snapshot.SchemaVersion == graph.DirectRunSnapshotSchemaVersion {
		direct, inputErr := graph.NewDirectLoopSnapshot(snapshot.SnapshotID, topology.Nodes[0], in.Inputs)
		if inputErr != nil {
			return f, ErrDenied
		}
		inputSnapshot, err = graph.NewDirectRunSnapshot(direct)
	} else {
		g, loadErr := s.FleetRepository.GetGraphRevision(ctx, snapshot.Graph.ID, snapshot.Graph.Revision)
		if loadErr != nil {
			return f, ErrDenied
		}
		inputSnapshot, err = graph.NewRunSnapshot(snapshot.SnapshotID, g, in.Inputs)
	}
	// The same exact approved task and inputs, not a changed work payload.
	if err != nil || inputSnapshot.Digest != snapshot.Digest {
		return f, ErrDenied
	}
	steps, err := s.QueueWorker.DoerTrace(ctx, attempt.AttemptID, in.Loop.Digest)
	if err != nil {
		return f, err
	}
	consumed, head, err := consumedDoerPasses(steps)
	if err != nil {
		return f, err
	}
	cap := revision.Doer.MaxAttempts
	var parent LoopReexecutionFact
	err = s.Store.Load("queue-loop-reexecution", v.Submission.IdempotencyKey, &parent)
	if err == nil {
		if parent.Digest != reexecutionDigest(parent) || parent.RequestID != v.Submission.IdempotencyKey || parent.Agent != in.Agent || parent.Loop != in.Loop || parent.SubjectID != sub.ID || parent.PrincipalID != sub.PrincipalID || parent.Remaining == 0 || parent.Remaining >= cap {
			return f, ErrDenied
		}
		cap = parent.Remaining
	} else if !errors.Is(err, os.ErrNotExist) {
		return f, err
	}
	if consumed >= cap {
		return f, ErrDoerReexecutionBudgetExhausted
	}
	f = LoopReexecutionFact{RequestID: requestID, RequestDigest: identity, SubjectID: sub.ID, PrincipalID: sub.PrincipalID, Agent: in.Agent, Loop: in.Loop, Previous: *in.Reexecution, CheckpointDigest: head, Consumed: consumed, Remaining: cap - consumed}
	f.Digest = reexecutionDigest(f)
	return f, nil
}

func (s *Service) sealLoopReexecution(ctx context.Context, sub core.Subject, in QueueLoopInput, id, identity string, revision loop.LoopRevision) (*LoopReexecutionFact, error) {
	if in.Reexecution == nil {
		return nil, nil
	}
	f, err := s.deriveLoopReexecution(ctx, sub, in, id, identity, revision)
	if err != nil {
		return nil, err
	}
	// Single successor even under distinct fresh keys. A failed/unknown append
	// outcome retains this slot; retry reconciles the exact same immutable fact.
	for _, slot := range []struct{ kind, key string }{{"queue-loop-reexecution-successor", f.Previous.PreviousQueue.ID}, {"queue-loop-reexecution", id}} {
		err = s.Store.Create(slot.kind, slot.key, f)
		if err != nil && !errors.Is(err, store.ErrAlreadyExists) {
			return nil, err
		}
		var got LoopReexecutionFact
		if err = s.Store.Load(slot.kind, slot.key, &got); err != nil {
			return nil, err
		}
		if got != f {
			return nil, fleet.ErrConflict
		}
	}
	if err = s.audit(ctx, core.AuditEvent{Type: "doer.run.reexecution", SubjectID: sub.ID, PrincipalID: sub.PrincipalID, AgentID: in.Agent.ID, Outcome: "authorized", Reason: "known_terminal_failure_remaining_budget", Metadata: map[string]string{"request_id": id, "lineage_digest": f.Digest, "previous_queue_id": f.Previous.PreviousQueue.ID, "previous_disposition_digest": f.Previous.PreviousDisposition.Digest}}); err != nil {
		return nil, err
	}
	return &f, nil
}

// Budget is resolved from the signed exact Run scope AND the canonical intent,
// never from wire counts, current Loop edits, or controller-wide grants.
func (s *Service) resolveDoerPassCap(ctx context.Context, c loop.DoerContract, a registry.AgentRevision) (uint16, error) {
	id, candidate, worker := orchestration.DoerExecutionScope(ctx)
	if !worker {
		return c.MaxAttempts, nil
	}
	if id == "" {
		return 0, ErrDenied
	}
	var f LoopReexecutionFact
	err := s.Store.Load("queue-loop-reexecution", id, &f)
	if errors.Is(err, os.ErrNotExist) {
		return c.MaxAttempts, nil
	}
	if err != nil || f.Digest != reexecutionDigest(f) || f.RequestID != id || f.Agent != agentRevisionRef(a) || f.Loop != candidate || f.Remaining == 0 || f.Remaining >= c.MaxAttempts || !orchestration.DoerExecutionIntentMatches(ctx, f.RequestDigest) {
		return 0, ErrDenied
	}
	if err = s.resolveDoerHostApproval(ctx, c, a); err != nil {
		return 0, err
	}
	record, err := s.Store.ReadDoerHostApproval(s.Config.Credentials.Authority.DeploymentID, a.Ownership.OwnerID, agentRevisionRef(a), c, s.Now())
	if err != nil || record.RunRequestID != id || record.ReexecutionDigest != f.Digest || record.ImplementationPassCap != f.Remaining {
		return 0, ErrDenied
	}
	return f.Remaining, nil
}
