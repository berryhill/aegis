package badger

import (
	"bytes"
	"context"

	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	queue "github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	badgerdb "github.com/dgraph-io/badger/v4"
)

func (s *Store) AcceptSubmission(ctx context.Context, accepted fleet.AcceptedSubmission, fact fleet.AuditFact) (created bool, err error) {
	snapshotWire, err := graph.MarshalRunSnapshot(accepted.Snapshot)
	if err != nil {
		return false, err
	}
	submissionWire, err := queue.MarshalSubmission(accepted.Submission)
	if err != nil {
		return false, err
	}
	itemWire, err := queue.MarshalItem(accepted.QueueItem)
	if err != nil {
		return false, err
	}
	runWire, err := execution.MarshalGraphRun(accepted.GraphRun)
	if err != nil {
		return false, err
	}
	transitionWire, err := queue.MarshalTransition(accepted.InitialTransition)
	if err != nil {
		return false, err
	}
	binding, err := requestBinding("accepted", snapshotWire, submissionWire, itemWire, runWire, transitionWire, fact.Event)
	if err != nil {
		return false, err
	}
	err = s.update(ctx, func(txn *badgerdb.Txn) error {
		requestKey := key(familySubmissionRequest, accepted.Submission.IdempotencyKey)
		if prior, found, e := optional(txn, requestKey); e != nil {
			return e
		} else if found {
			if bytes.Equal(prior, binding) {
				return nil
			}
			return fleet.ErrConflict
		}
		if e := verifySnapshotReferences(txn, accepted.Snapshot); e != nil {
			return e
		}
		if accepted.Submission.Snapshot != digestRef(accepted.Snapshot.SnapshotID, accepted.Snapshot.Digest) ||
			accepted.QueueItem.Submission != digestRef(accepted.Submission.SubmissionID, accepted.Submission.Digest) ||
			accepted.QueueItem.Snapshot != accepted.Submission.Snapshot || accepted.QueueItem.Authority != accepted.Submission.Authority ||
			accepted.QueueItem.GraphRunID != accepted.GraphRun.GraphRunID || accepted.GraphRun.QueueItem != digestRef(accepted.QueueItem.ItemID, accepted.QueueItem.Digest) ||
			accepted.GraphRun.Snapshot != accepted.Submission.Snapshot || accepted.GraphRun.Authority != accepted.Submission.Authority ||
			accepted.InitialTransition.QueueItemID != accepted.QueueItem.ItemID || accepted.InitialTransition.From != "" || accepted.InitialTransition.To != queue.StateQueued {
			return fleet.ErrConflict
		}
		entries := []struct{ k, v []byte }{
			{key(familySnapshot, accepted.Snapshot.SnapshotID), snapshotWire},
			{key(familySubmission, accepted.Submission.SubmissionID), submissionWire},
			{key(familyQueueItem, accepted.QueueItem.ItemID), itemWire},
			{key(familyGraphRun, accepted.GraphRun.GraphRunID), runWire},
			{key(familyQueueTransition, accepted.QueueItem.ItemID, accepted.InitialTransition.TransitionID), transitionWire},
			{requestKey, binding},
		}
		for _, entry := range entries {
			if e := create(txn, entry.k, entry.v); e != nil {
				return e
			}
		}
		if e := appendAudit(txn, fact); e != nil {
			return e
		}
		created = true
		return nil
	})
	return created, err
}

func (s *Store) RejectSubmission(ctx context.Context, rejection queue.Rejection, fact fleet.AuditFact) (created bool, err error) {
	wire, err := queue.MarshalRejection(rejection)
	if err != nil {
		return false, err
	}
	binding, err := requestBinding("rejected", wire, fact.Event)
	if err != nil {
		return false, err
	}
	err = s.update(ctx, func(txn *badgerdb.Txn) error {
		requestKey := key(familySubmissionRequest, rejection.IdempotencyKey)
		if prior, found, e := optional(txn, requestKey); e != nil {
			return e
		} else if found {
			if bytes.Equal(prior, binding) {
				return nil
			}
			return fleet.ErrConflict
		}
		if e := create(txn, key(familyRejection, rejection.RejectionID), wire); e != nil {
			return e
		}
		if e := create(txn, requestKey, binding); e != nil {
			return e
		}
		if e := appendAudit(txn, fact); e != nil {
			return e
		}
		created = true
		return nil
	})
	return created, err
}

func (s *Store) CreateLoopExecution(ctx context.Context, value execution.LoopExecution, fact fleet.AuditFact) (created bool, err error) {
	wire, err := execution.MarshalLoopExecution(value)
	if err != nil {
		return false, err
	}
	binding, err := requestBinding(wire, fact.Event)
	if err != nil {
		return false, err
	}
	err = s.update(ctx, func(txn *badgerdb.Txn) error {
		recordKey := key(familyLoopExecution, value.LoopExecutionID)
		if prior, found, e := optional(txn, recordKey); e != nil {
			return e
		} else if found {
			if bytes.Equal(prior, wire) {
				return nil
			}
			return fleet.ErrConflict
		}
		run, e := loadGraphRun(txn, value.GraphRunID)
		if e != nil {
			return e
		}
		snapshot, e := loadSnapshot(txn, run.Snapshot.ID)
		if e != nil {
			return e
		}
		if !containsRevision(snapshot.Loops, value.Loop) || !containsRevision(snapshot.Participants, value.Participant) {
			return fleet.ErrConflict
		}
		if e := create(txn, recordKey, wire); e != nil {
			return e
		}
		if e := create(txn, key(familyLoopExecution, value.LoopExecutionID, "binding"), binding); e != nil {
			return e
		}
		if e := appendAudit(txn, fact); e != nil {
			return e
		}
		created = true
		return nil
	})
	return created, err
}

func (s *Store) ClaimQueueItem(ctx context.Context, claim queue.Claim, attempt execution.Attempt, transition queue.QueueTransition, fact fleet.AuditFact) error {
	claimWire, err := queue.MarshalClaim(claim)
	if err != nil {
		return err
	}
	attemptWire, err := execution.MarshalAttempt(attempt)
	if err != nil {
		return err
	}
	transitionWire, err := queue.MarshalTransition(transition)
	if err != nil {
		return err
	}
	return s.update(ctx, func(txn *badgerdb.Txn) error {
		item, e := loadQueueItem(txn, claim.QueueItem.ID)
		if e != nil {
			return e
		}
		if claim.QueueItem != digestRef(item.ItemID, item.Digest) || claim.Authority != item.Authority {
			return fleet.ErrConflict
		}
		if _, found, e := optional(txn, key(familyClaimByItem, item.ItemID)); e != nil {
			return e
		} else if found {
			return fleet.ErrConflict
		}
		loopExecution, e := loadLoopExecution(txn, attempt.LoopExecutionID)
		if e != nil {
			return e
		}
		if attempt.AttemptID != claim.AttemptID || attempt.ClaimID != claim.ClaimID || attempt.QueueItem != claim.QueueItem || attempt.GraphRunID != item.GraphRunID || loopExecution.GraphRunID != attempt.GraphRunID || transition.QueueItemID != item.ItemID || transition.ClaimID != claim.ClaimID || transition.From != queue.StateQueued || transition.To != queue.StateClaimed {
			return fleet.ErrConflict
		}
		for _, entry := range []struct{ k, v []byte }{{key(familyClaim, claim.ClaimID), claimWire}, {key(familyClaimByItem, item.ItemID), []byte(claim.ClaimID)}, {key(familyAttempt, attempt.AttemptID), attemptWire}, {key(familyQueueTransition, item.ItemID, transition.TransitionID), transitionWire}} {
			if e = create(txn, entry.k, entry.v); e != nil {
				return e
			}
		}
		return appendAudit(txn, fact)
	})
}

func digestRef(id, digest string) reference.DigestRef {
	return reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: id, Digest: digest}
}
func containsRevision(values []reference.RevisionRef, want reference.RevisionRef) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func loadSnapshot(txn *badgerdb.Txn, id string) (graph.GraphRunSnapshot, error) {
	wire, err := get(txn, key(familySnapshot, id))
	if err != nil {
		return graph.GraphRunSnapshot{}, err
	}
	v, err := graph.UnmarshalRunSnapshot(wire)
	if err != nil || v.SnapshotID != id {
		return graph.GraphRunSnapshot{}, corrupt(err)
	}
	return v, nil
}
func loadQueueItem(txn *badgerdb.Txn, id string) (queue.Item, error) {
	wire, err := get(txn, key(familyQueueItem, id))
	if err != nil {
		return queue.Item{}, err
	}
	v, err := queue.UnmarshalItem(wire)
	if err != nil || v.ItemID != id {
		return queue.Item{}, corrupt(err)
	}
	return v, nil
}
func loadGraphRun(txn *badgerdb.Txn, id string) (execution.GraphRun, error) {
	wire, err := get(txn, key(familyGraphRun, id))
	if err != nil {
		return execution.GraphRun{}, err
	}
	v, err := execution.UnmarshalGraphRun(wire)
	if err != nil || v.GraphRunID != id {
		return execution.GraphRun{}, corrupt(err)
	}
	return v, nil
}
func loadLoopExecution(txn *badgerdb.Txn, id string) (execution.LoopExecution, error) {
	wire, err := get(txn, key(familyLoopExecution, id))
	if err != nil {
		return execution.LoopExecution{}, err
	}
	v, err := execution.UnmarshalLoopExecution(wire)
	if err != nil || v.LoopExecutionID != id {
		return execution.LoopExecution{}, corrupt(err)
	}
	return v, nil
}

func readRecord[T any](s *Store, ctx context.Context, family byte, id string, decode func([]byte) (T, error), identity func(T) string) (out T, err error) {
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		wire, e := get(txn, key(family, id))
		if e != nil {
			return e
		}
		out, e = decode(wire)
		if e != nil || identity(out) != id {
			return corrupt(e)
		}
		return nil
	})
	return
}
func (s *Store) GetSubmission(ctx context.Context, id string) (queue.Submission, error) {
	return readRecord(s, ctx, familySubmission, id, queue.UnmarshalSubmission, func(v queue.Submission) string { return v.SubmissionID })
}
func (s *Store) GetRejection(ctx context.Context, id string) (queue.Rejection, error) {
	return readRecord(s, ctx, familyRejection, id, queue.UnmarshalRejection, func(v queue.Rejection) string { return v.RejectionID })
}
func (s *Store) GetQueueItem(ctx context.Context, id string) (queue.Item, error) {
	return readRecord(s, ctx, familyQueueItem, id, queue.UnmarshalItem, func(v queue.Item) string { return v.ItemID })
}
func (s *Store) GetGraphRun(ctx context.Context, id string) (execution.GraphRun, error) {
	return readRecord(s, ctx, familyGraphRun, id, execution.UnmarshalGraphRun, func(v execution.GraphRun) string { return v.GraphRunID })
}
func (s *Store) GetLoopExecution(ctx context.Context, id string) (execution.LoopExecution, error) {
	return readRecord(s, ctx, familyLoopExecution, id, execution.UnmarshalLoopExecution, func(v execution.LoopExecution) string { return v.LoopExecutionID })
}
func (s *Store) GetClaim(ctx context.Context, id string) (queue.Claim, error) {
	return readRecord(s, ctx, familyClaim, id, queue.UnmarshalClaim, func(v queue.Claim) string { return v.ClaimID })
}
func (s *Store) GetAttempt(ctx context.Context, id string) (execution.Attempt, error) {
	return readRecord(s, ctx, familyAttempt, id, execution.UnmarshalAttempt, func(v execution.Attempt) string { return v.AttemptID })
}
