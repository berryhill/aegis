package badger

import (
	"context"
	"encoding/json"

	"github.com/berryhill/aegis/internal/disposition"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	queue "github.com/berryhill/aegis/internal/queue"
	badgerdb "github.com/dgraph-io/badger/v4"
)

// CompleteQueueItem atomically publishes immutable evidence metadata, the
// terminal disposition, the queue transition, and authoritative audit fact.
func (s *Store) CompleteQueueItem(ctx context.Context, completion fleet.Completion, fact fleet.AuditFact) error {
	dispositionWire, err := disposition.Marshal(completion.Disposition)
	if err != nil {
		return err
	}
	transitionWire, err := queue.MarshalTransition(completion.Transition)
	if err != nil {
		return err
	}
	var artifactWire []byte
	if completion.Artifact != nil {
		if err = completion.Artifact.Validate(); err != nil {
			return err
		}
		artifactWire, err = json.Marshal(completion.Artifact)
		if err != nil {
			return err
		}
	}
	receiptWires := make([][]byte, len(completion.Receipts))
	for index, receipt := range completion.Receipts {
		if err = receipt.Validate(); err != nil {
			return err
		}
		receiptWires[index], err = json.Marshal(receipt)
		if err != nil {
			return err
		}
	}
	return s.update(ctx, func(txn *badgerdb.Txn) error {
		claimWire, e := get(txn, key(familyClaim, completion.Claim.ClaimID))
		if e != nil {
			return e
		}
		storedClaim, e := queue.UnmarshalClaim(claimWire)
		if e != nil || storedClaim != completion.Claim {
			return fleet.ErrConflict
		}
		projection, e := loadQueueProjection(txn, storedClaim.QueueItem.ID)
		if e != nil || validateProjectionBasis(txn, projection) != nil || projection.State != queue.StateClaimed || projection.ActiveClaimID != storedClaim.ClaimID || !completion.Transition.OccurredAt.Before(storedClaim.ExpiresAt) {
			return fleet.ErrConflict
		}
		attemptWire, e := get(txn, key(familyAttempt, completion.Disposition.AttemptID))
		if e != nil {
			return e
		}
		attempt, e := execution.UnmarshalAttempt(attemptWire)
		if e != nil || attempt.ClaimID != storedClaim.ClaimID || attempt.GraphRunID != completion.Disposition.GraphRunID || attempt.LoopExecutionID != completion.Disposition.LoopExecutionID || attempt.QueueItem != completion.Disposition.QueueItem {
			return fleet.ErrConflict
		}
		if completion.Disposition.Authority != storedClaim.Authority || completion.Transition.QueueItemID != storedClaim.QueueItem.ID || completion.Transition.ClaimID != storedClaim.ClaimID || completion.Transition.From != queue.StateClaimed || queueState(completion.Disposition.State) != completion.Transition.To {
			return fleet.ErrConflict
		}
		if _, found, e := optional(txn, key(familyDispositionByRun, completion.Disposition.GraphRunID)); e != nil {
			return e
		} else if found {
			return fleet.ErrConflict
		}
		if completion.Disposition.State == execution.StateSucceeded {
			if completion.Artifact == nil || len(completion.Disposition.ArtifactIDs) != 1 || completion.Disposition.ArtifactIDs[0] != completion.Artifact.ID || len(completion.Disposition.ReceiptIDs) != len(completion.Receipts) {
				return fleet.ErrConflict
			}
			if completion.Artifact.AuthorityContextID != storedClaim.Authority.ID || completion.Artifact.AuthorityContextDigest != storedClaim.Authority.Digest || completion.Artifact.RunID != attempt.LoopExecutionID {
				return fleet.ErrConflict
			}
			for index, receipt := range completion.Receipts {
				if receipt.Outcome != evidence.Passed || receipt.ID != completion.Disposition.ReceiptIDs[index] || receipt.ArtifactID != completion.Artifact.ID || receipt.ActionID != completion.Artifact.ActionID || receipt.RunID != completion.Artifact.RunID || receipt.OwnerID != completion.Artifact.OwnerID || receipt.AuthorityContextID != completion.Artifact.AuthorityContextID || receipt.AuthorityContextDigest != completion.Artifact.AuthorityContextDigest {
					return fleet.ErrConflict
				}
			}
		} else if completion.Artifact != nil || len(completion.Receipts) != 0 || len(completion.Disposition.ArtifactIDs) != 0 || len(completion.Disposition.ReceiptIDs) != 0 {
			return fleet.ErrConflict
		}
		terminalProjection, e := queue.NewProjection(queue.Projection{QueueItemID: storedClaim.QueueItem.ID, State: completion.Transition.To, Attempts: projection.Attempts, AvailableAt: projection.AvailableAt, LastTransitionID: completion.Transition.TransitionID, UpdatedAt: completion.Transition.OccurredAt})
		if e != nil {
			return e
		}
		projectionWire, e := queue.MarshalProjection(terminalProjection)
		if e != nil {
			return e
		}
		if completion.Artifact != nil {
			if e = create(txn, key(familyRuntimeArtifact, completion.Artifact.ID), artifactWire); e != nil {
				return e
			}
		}
		for index, receipt := range completion.Receipts {
			if e = create(txn, key(familyVerificationReceipt, receipt.ID), receiptWires[index]); e != nil {
				return e
			}
		}
		for _, entry := range []struct{ k, v []byte }{
			{key(familyDisposition, completion.Disposition.DispositionID), dispositionWire},
			{key(familyDispositionByRun, completion.Disposition.GraphRunID), []byte(completion.Disposition.DispositionID)},
			{key(familyQueueTransition, completion.Claim.QueueItem.ID, completion.Transition.TransitionID), transitionWire},
		} {
			if e = create(txn, entry.k, entry.v); e != nil {
				return e
			}
		}
		if e = txn.Delete(key(familyClaimByItem, storedClaim.QueueItem.ID)); e != nil {
			return e
		}
		if e = txn.Set(key(familyQueueProjection, storedClaim.QueueItem.ID), projectionWire); e != nil {
			return e
		}
		return appendAudit(txn, fact)
	})
}

func queueState(state execution.State) queue.State {
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
	default:
		return ""
	}
}

func (s *Store) GetRuntimeArtifact(ctx context.Context, id string) (out evidence.RuntimeArtifact, err error) {
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		wire, e := get(txn, key(familyRuntimeArtifact, id))
		if e != nil {
			return e
		}
		if e = json.Unmarshal(wire, &out); e != nil || out.ID != id || out.Validate() != nil {
			return corrupt(e)
		}
		return nil
	})
	return
}
func (s *Store) GetVerificationReceipt(ctx context.Context, id string) (out evidence.VerificationReceipt, err error) {
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		wire, e := get(txn, key(familyVerificationReceipt, id))
		if e != nil {
			return e
		}
		if e = json.Unmarshal(wire, &out); e != nil || out.ID != id || out.Validate() != nil {
			return corrupt(e)
		}
		return nil
	})
	return
}
func (s *Store) GetDisposition(ctx context.Context, id string) (out disposition.Record, err error) {
	return readRecord(s, ctx, familyDisposition, id, disposition.Unmarshal, func(v disposition.Record) string { return v.DispositionID })
}
