package badger

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"time"

	"github.com/berryhill/aegis/internal/disposition"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	badgerdb "github.com/dgraph-io/badger/v4"
)

func createCompletionEvidence(txn *badgerdb.Txn, k, v []byte, aggregate bool) error {
	if !aggregate {
		return create(txn, k, v)
	}
	old, e := get(txn, k)
	if e != nil {
		return e
	}
	if string(old) != string(v) {
		return fleet.ErrConflict
	}
	return nil
}

func nodePortsMatch(asserted []graph.Port, exact []loop.Port) bool {
	if len(asserted) != len(exact) {
		return false
	}
	for _, p := range asserted {
		found := false
		for _, q := range exact {
			if p.ID == q.ID && string(p.Type) == string(q.Type) && p.Required == q.Required {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

var _ fleet.GraphNodeRepository = (*Store)(nil)

func graphNodePlan(txn *badgerdb.Txn, claim queue.Claim, at time.Time) (graph.ProducerConsumerPlan, error) {
	fail := func() (graph.ProducerConsumerPlan, error) { return graph.ProducerConsumerPlan{}, fleet.ErrConflict }
	stored, e := loadClaim(txn, claim.ClaimID)
	if e != nil || stored != claim || at.Before(claim.ClaimedAt) || !at.Before(claim.ExpiresAt) {
		return fail()
	}
	item, e := loadQueueItem(txn, claim.QueueItem.ID)
	if e != nil || digestRef(item.ItemID, item.Digest) != claim.QueueItem || item.MaxAttempts != 1 {
		return fail()
	}
	p, e := loadQueueProjection(txn, item.ItemID)
	if e != nil || validateProjectionBasis(txn, p) != nil || p.State != queue.StateClaimed || p.ActiveClaimID != claim.ClaimID || p.Attempts != 1 {
		return fail()
	}
	wire, e := get(txn, key(familySnapshot, item.Snapshot.ID))
	if e != nil {
		return fail()
	}
	snapshot, e := graph.UnmarshalRunSnapshot(wire)
	if e != nil || snapshot.Digest != item.Snapshot.Digest {
		return fail()
	}
	wire, e = get(txn, key(familyGraphRevision, snapshot.Graph.ID, revisionPart(snapshot.Graph.Revision)))
	if e != nil {
		return fail()
	}
	definition, e := graph.UnmarshalRevision(wire)
	if e != nil {
		return fail()
	}
	plan, e := graph.ResolveProducerConsumerPlan(snapshot, definition)
	if e != nil {
		return fail()
	}
	return plan, nil
}

func loadNode(txn *badgerdb.Txn, run, nodeID string) (a fleet.GraphNodeAdmission, e error) {
	wire, e := get(txn, key(familyGraphNode, run, nodeID))
	if e != nil {
		return a, e
	}
	e = unmarshalGraphNodeRecord(wire, fleet.GraphNodeSchemaVersion, &a)
	if e != nil || validateGraphNodeAdmission(a) != nil || a.Execution.GraphRunID != run || a.Execution.GraphNodeID != nodeID {
		return a, fleet.ErrCorrupt
	}
	claim, err := loadClaim(txn, a.Claim.ClaimID)
	child, childErr := loadLoopExecution(txn, a.Execution.LoopExecutionID)
	attemptWire, attemptErr := get(txn, key(familyAttempt, a.Attempt.AttemptID))
	exactAttempt, marshalErr := execution.MarshalAttempt(a.Attempt)
	if err != nil || claim != a.Claim || childErr != nil || !reflect.DeepEqual(child, a.Execution) || attemptErr != nil || marshalErr != nil || string(attemptWire) != string(exactAttempt) {
		return a, fleet.ErrCorrupt
	}
	lw, le := get(txn, key(familyLoopRevision, a.Execution.Loop.ID, revisionPart(a.Execution.Loop.Revision)))
	lr, de := loop.UnmarshalRevision(lw)
	max := uint32(2)
	if lr.ManagedDoer != nil {
		max = uint32(lr.ManagedDoer.Doer.MaxAttempts)
	}
	if le != nil || de != nil || lr.Digest != a.Execution.Loop.Digest || a.MaxImplementationPasses != max {
		return a, fleet.ErrCorrupt
	}
	item, ie := loadQueueItem(txn, a.Claim.QueueItem.ID)
	sw, se := get(txn, key(familySnapshot, item.Snapshot.ID))
	snapshot, sd := graph.UnmarshalRunSnapshot(sw)
	gw, ge := get(txn, key(familyGraphRevision, snapshot.Graph.ID, revisionPart(snapshot.Graph.Revision)))
	definition, gd := graph.UnmarshalRevision(gw)
	plan, pe := graph.ResolveProducerConsumerPlan(snapshot, definition)
	if ie != nil || se != nil || sd != nil || ge != nil || gd != nil || pe != nil {
		return a, fleet.ErrCorrupt
	}
	node := plan.Producer
	if node.ID != a.Execution.GraphNodeID {
		node = plan.Consumer
		predecessor, e := loadNodeCheckpoint(txn, run, plan.Producer.ID)
		if e != nil || predecessor.Completion.Disposition.State != execution.StateSucceeded || predecessor.Completion.Artifact == nil || a.Input == nil || *a.Input != exactHandoff(plan, predecessor) {
			return a, fleet.ErrCorrupt
		}
	} else if a.Input != nil {
		return a, fleet.ErrCorrupt
	}
	if node.ID != a.Execution.GraphNodeID || node.Loop != a.Execution.Loop || node.Participant != a.Execution.Participant {
		return a, fleet.ErrCorrupt
	}
	return
}
func loadNodeCheckpoint(txn *badgerdb.Txn, run, node string) (c fleet.GraphNodeCheckpoint, e error) {
	wire, e := get(txn, key(familyGraphNodeCheckpoint, run, node))
	if e != nil {
		return c, e
	}
	e = unmarshalGraphNodeRecord(wire, graphNodeCheckpointSchema, &c)
	if e != nil || c.Admission.Execution.GraphRunID != run || c.Admission.Execution.GraphNodeID != node {
		return c, fleet.ErrCorrupt
	}
	a, err := loadNode(txn, run, node)
	if err != nil || !reflect.DeepEqual(a, c.Admission) || !validNodeResult(txn, a, c.Completion) {
		return c, fleet.ErrCorrupt
	}
	return
}
func (s *Store) GetGraphNode(ctx context.Context, run, node string) (a fleet.GraphNodeAdmission, err error) {
	err = s.view(ctx, func(txn *badgerdb.Txn) error { var e error; a, e = loadNode(txn, run, node); return e })
	return
}
func (s *Store) GetGraphNodeCheckpoint(ctx context.Context, run, node string) (c fleet.GraphNodeCheckpoint, err error) {
	err = s.view(ctx, func(txn *badgerdb.Txn) error { var e error; c, e = loadNodeCheckpoint(txn, run, node); return e })
	return
}

func exactHandoff(plan graph.ProducerConsumerPlan, c fleet.GraphNodeCheckpoint) fleet.GraphNodeHandoff {
	return fleet.GraphNodeHandoff{NodeID: plan.Producer.ID, ExecutionID: c.Admission.Execution.LoopExecutionID, AttemptID: c.Admission.Attempt.AttemptID, ArtifactID: c.Completion.Artifact.ID, ContentDigest: c.Completion.Artifact.Digest, DispositionID: c.Completion.Disposition.DispositionID, EdgeID: plan.Edge.ID}
}

func (s *Store) AdmitGraphNode(ctx context.Context, a fleet.GraphNodeAdmission, fact fleet.AuditFact) error {
	ew, e := execution.MarshalLoopExecution(a.Execution)
	if e != nil {
		return e
	}
	aw, e := execution.MarshalAttempt(a.Attempt)
	if e != nil {
		return e
	}
	if a.SchemaVersion != fleet.GraphNodeSchemaVersion || (a.MaxImplementationPasses < 1 || a.MaxImplementationPasses > 2) || a.Attempt.ClaimID != a.Claim.ClaimID || a.Attempt.QueueItem != a.Claim.QueueItem || a.Attempt.LoopExecutionID != a.Execution.LoopExecutionID || a.Attempt.GraphRunID != a.Execution.GraphRunID || a.Attempt.AttemptNumber != 1 || !a.Attempt.CreatedAt.Equal(a.AdmittedAt) || fact.Event.Type != "fleet.graph.node.admitted" || fact.Event.SubjectID != a.Execution.GraphNodeID {
		return fleet.ErrConflict
	}
	return s.update(ctx, func(txn *badgerdb.Txn) error {
		plan, e := graphNodePlan(txn, a.Claim, a.AdmittedAt)
		if e != nil {
			return e
		}
		item, e := loadQueueItem(txn, a.Claim.QueueItem.ID)
		if e != nil || item.GraphRunID != a.Execution.GraphRunID {
			return fleet.ErrConflict
		}
		node := plan.Producer
		if a.Execution.GraphNodeID == plan.Consumer.ID {
			node = plan.Consumer
			predecessor, e := loadNodeCheckpoint(txn, a.Execution.GraphRunID, plan.Producer.ID)
			if e != nil || predecessor.Completion.Disposition.State != execution.StateSucceeded || predecessor.Completion.Artifact == nil || a.Input == nil || *a.Input != exactHandoff(plan, predecessor) || a.AdmittedAt.Before(predecessor.Completion.Disposition.OccurredAt) {
				return fleet.ErrConflict
			}
		} else if a.Input != nil || a.Attempt.AttemptID != a.Claim.AttemptID {
			return fleet.ErrConflict
		}
		if node.ID != a.Execution.GraphNodeID || node.Loop != a.Execution.Loop || node.Participant != a.Execution.Participant {
			return fleet.ErrConflict
		}
		// First implementation slice accepts only raw v2 evidence. Managed selected-file
		// schemas require their own versioned binding, never raw envelope-as-content.
		lw, e := get(txn, key(familyLoopRevision, node.Loop.ID, revisionPart(node.Loop.Revision)))
		if e != nil {
			return e
		}
		lr, e := loop.UnmarshalRevision(lw)
		if e != nil || (lr.SchemaVersion != loop.RevisionSchemaVersion && lr.SchemaVersion != loop.ManagedDoerSchemaVersion) || lr.Digest != node.Loop.Digest || !nodePortsMatch(node.Inputs, lr.Inputs) || !nodePortsMatch(node.Outputs, lr.Outputs) {
			return fleet.ErrConflict
		}
		max := uint16(2)
		if lr.SchemaVersion == loop.ManagedDoerSchemaVersion {
			if lr.ManagedDoer == nil {
				return fleet.ErrConflict
			}
			max = lr.ManagedDoer.Doer.MaxAttempts
		}
		if a.MaxImplementationPasses != uint32(max) {
			return fleet.ErrConflict
		}
		if _, found, e := optional(txn, key(familyGraphNode, a.Execution.GraphRunID, node.ID)); e != nil || found {
			return fleet.ErrConflict
		}
		// Reuse the producer identities created by the one outer claim; a consumer
		// creates new child facts without consuming/resetting the Queue budget.
		for _, entry := range []struct{ k, v []byte }{{key(familyLoopExecution, a.Execution.LoopExecutionID), ew}, {key(familyAttempt, a.Attempt.AttemptID), aw}} {
			old, found, e := optional(txn, entry.k)
			if e != nil {
				return e
			}
			if found {
				if string(old) != string(entry.v) || node.ID != plan.Producer.ID {
					return fleet.ErrConflict
				}
			} else if e = create(txn, entry.k, entry.v); e != nil {
				return e
			}
		}
		wire, e := marshalGraphNodeRecord(fleet.GraphNodeSchemaVersion, a)
		if e != nil {
			return e
		}
		if e = create(txn, key(familyGraphNode, a.Execution.GraphRunID, node.ID), wire); e != nil {
			return e
		}
		return appendAudit(txn, fact)
	})
}

// reloadNodeEvidence checks actual content-addressed bytes and receipt wires.
// Provenance is an in-process seal and is checked only on initial checkpoint.
func reloadNodeEvidence(c fleet.Completion, reader fleet.EvidenceReader) bool {
	if c.Artifact == nil || reader == nil || c.Artifact.Validate() != nil || len(c.Receipts) == 0 {
		return false
	}
	b, e := reader.GetBlob(c.Artifact.ContentRef)
	sum := sha256.Sum256(b)
	if e != nil || "sha256:"+hex.EncodeToString(sum[:]) != c.Artifact.Digest {
		return false
	}
	for _, r := range c.Receipts {
		b, e = reader.GetBlob(r.EvidenceRef)
		got, de := evidence.DecodeVerificationReceipt(r.EvidenceRef, b)
		if e != nil || de != nil || got != r || r.Validate() != nil || r.Outcome != evidence.Passed || r.ObservedDigest != c.Artifact.Digest || r.ArtifactID != c.Artifact.ID || r.AttemptID != c.Artifact.AttemptID || r.RunID != c.Artifact.RunID || r.OwnerID != c.Artifact.OwnerID || r.ActionID != c.Artifact.ActionID || r.AuthorityContextID != c.Artifact.AuthorityContextID || r.AuthorityContextDigest != c.Artifact.AuthorityContextDigest {
			return false
		}
	}
	return true
}

func validNodeResult(txn *badgerdb.Txn, a fleet.GraphNodeAdmission, c fleet.Completion) bool {
	d := c.Disposition
	if c.Claim != a.Claim || d.GraphRunID != a.Execution.GraphRunID || d.LoopExecutionID != a.Execution.LoopExecutionID || d.AttemptID != a.Attempt.AttemptID || d.QueueItem != a.Claim.QueueItem || d.Authority != a.Claim.Authority || d.OccurredAt.Before(a.AdmittedAt) || (!d.OccurredAt.Before(a.Claim.ExpiresAt) && d.State != execution.StateExpired) {
		return false
	}
	if _, e := disposition.Marshal(d); e != nil {
		return false
	}
	tr := c.Transition
	if _, e := queue.MarshalTransition(tr); e != nil || tr.QueueItemID != a.Claim.QueueItem.ID || tr.ClaimID != a.Claim.ClaimID || tr.From != queue.StateClaimed || !tr.OccurredAt.Equal(d.OccurredAt) || tr.Reason != d.ReasonCode {
		return false
	}
	if d.State == execution.StateSucceeded && tr.To != queue.StateSucceeded {
		return false
	}
	if d.State != execution.StateSucceeded {
		return (d.State == execution.StateFailed || d.State == execution.StateDenied || d.State == execution.StateExpired || d.State == execution.StateCancelled) && c.Artifact == nil && len(c.Receipts) == 0 && len(d.ArtifactIDs) == 0 && len(d.ReceiptIDs) == 0 && c.Transition.To == executionQueueStateForNode(d.State)
	}
	if c.Artifact == nil {
		return false
	}
	if c.Artifact.RunID != a.Execution.LoopExecutionID || c.Artifact.AttemptID != a.Attempt.AttemptID || c.Artifact.OwnerID != a.Execution.Participant.ID || c.Artifact.AuthorityContextID != a.Claim.Authority.ID || c.Artifact.AuthorityContextDigest != a.Claim.Authority.Digest || len(d.ArtifactIDs) != 1 || d.ArtifactIDs[0] != c.Artifact.ID || len(d.ReceiptIDs) != len(c.Receipts) {
		return false
	}
	for i, r := range c.Receipts {
		if r.ID != d.ReceiptIDs[i] {
			return false
		}
	}
	return exactNodeEvidence(txn, a.Attempt, c)
}

func (s *Store) CheckpointGraphNode(ctx context.Context, c fleet.GraphNodeCheckpoint, fact fleet.AuditFact, reader fleet.EvidenceReader) error {
	if (c.Completion.Disposition.State == execution.StateSucceeded && (!reloadNodeEvidence(c.Completion, reader) || !evidence.ValidateCompletionProvenance(c.Completion.Provenance, *c.Completion.Artifact, c.Completion.Receipts))) || fact.Event.Type != "fleet.graph.node.checkpointed" || fact.Event.SubjectID != c.Admission.Execution.GraphNodeID {
		return fleet.ErrConflict
	}
	return s.update(ctx, func(txn *badgerdb.Txn) error {
		at := c.Completion.Disposition.OccurredAt
		if c.Completion.Disposition.State == execution.StateExpired {
			at = c.Admission.AdmittedAt
		}
		if _, e := graphNodePlan(txn, c.Admission.Claim, at); e != nil {
			return e
		}
		a, e := loadNode(txn, c.Admission.Execution.GraphRunID, c.Admission.Execution.GraphNodeID)
		if e != nil || !reflect.DeepEqual(a, c.Admission) || !validNodeResult(txn, a, c.Completion) {
			return fleet.ErrConflict
		}
		wire, e := marshalGraphNodeRecord(graphNodeCheckpointSchema, c)
		if e != nil {
			return e
		}
		if e = create(txn, key(familyGraphNodeCheckpoint, a.Execution.GraphRunID, a.Execution.GraphNodeID), wire); e != nil {
			return e
		}
		// Producer checkpoint is durable evidence only: no root disposition index,
		// projection mutation, lease deletion or additional Queue attempt.
		if c.Completion.Disposition.State != execution.StateSucceeded {
			return appendAudit(txn, fact)
		}
		aw, _ := json.Marshal(c.Completion.Artifact)
		if e = create(txn, key(familyRuntimeArtifact, c.Completion.Artifact.ID), aw); e != nil {
			return e
		}
		for _, r := range c.Completion.Receipts {
			rw, _ := json.Marshal(r)
			if e = create(txn, key(familyVerificationReceipt, r.ID), rw); e != nil {
				return e
			}
		}
		return appendAudit(txn, fact)
	})
}

func executionQueueStateForNode(state execution.State) queue.State {
	switch state {
	case execution.StateFailed:
		return queue.StateFailed
	case execution.StateDenied:
		return queue.StateDenied
	case execution.StateExpired:
		return queue.StateExpired
	case execution.StateCancelled:
		return queue.StateCancelled
	}
	return ""
}

func exactGraphAggregate(txn *badgerdb.Txn, c fleet.Completion, reader fleet.EvidenceReader) bool {
	plan, e := graphNodePlan(txn, c.Claim, c.Disposition.OccurredAt)
	if e != nil || c.Disposition.State != execution.StateSucceeded {
		return false
	}
	producer, e := loadNodeCheckpoint(txn, c.Disposition.GraphRunID, plan.Producer.ID)
	if e != nil || !reloadNodeEvidence(producer.Completion, reader) || !validNodeResult(txn, producer.Admission, producer.Completion) {
		return false
	}
	consumer, e := loadNodeCheckpoint(txn, c.Disposition.GraphRunID, plan.Consumer.ID)
	if e != nil || !reloadNodeEvidence(consumer.Completion, reader) || !validNodeResult(txn, consumer.Admission, consumer.Completion) || consumer.Admission.Input == nil || *consumer.Admission.Input != exactHandoff(plan, producer) {
		return false
	}
	// Aggregate uses a fresh disposition identity but exactly the checkpointed
	// consumer evidence/attempt. Those evidence records are already immutable.
	return c.Disposition.AttemptID == consumer.Admission.Attempt.AttemptID && c.Disposition.LoopExecutionID == consumer.Admission.Execution.LoopExecutionID && reflect.DeepEqual(c.Artifact, consumer.Completion.Artifact) && reflect.DeepEqual(c.Receipts, consumer.Completion.Receipts) && !c.Disposition.OccurredAt.Before(consumer.Completion.Disposition.OccurredAt)
}
func (s *Store) CompleteGraph(ctx context.Context, c fleet.Completion, fact fleet.AuditFact, reader fleet.EvidenceReader) error {
	return s.completeQueueItem(ctx, c, fact, reader, nil, true)
}
