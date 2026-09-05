package badger

import (
	"bytes"
	"context"
	"errors"
	"sort"

	"github.com/berryhill/aegis/internal/disposition"
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
	projection, err := queue.NewProjection(queue.Projection{QueueItemID: accepted.QueueItem.ItemID, State: accepted.InitialTransition.To, AvailableAt: accepted.QueueItem.AvailableAt, LastTransitionID: accepted.InitialTransition.TransitionID, UpdatedAt: accepted.InitialTransition.OccurredAt})
	if err != nil {
		return false, err
	}
	projectionWire, err := queue.MarshalProjection(projection)
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
			accepted.InitialTransition.QueueItemID != accepted.QueueItem.ItemID || accepted.InitialTransition.From != "" || (accepted.InitialTransition.To != queue.StateQueued && accepted.InitialTransition.To != queue.StateAwaitingRuntime) ||
			(accepted.Submission.AuthorityKind == "registered-agent-workspace") != (accepted.InitialTransition.To == queue.StateAwaitingRuntime) {
			return fleet.ErrConflict
		}
		entries := []struct{ k, v []byte }{
			{key(familySnapshot, accepted.Snapshot.SnapshotID), snapshotWire},
			{key(familySubmission, accepted.Submission.SubmissionID), submissionWire},
			{key(familyQueueItem, accepted.QueueItem.ItemID), itemWire},
			{key(familyGraphRun, accepted.GraphRun.GraphRunID), runWire},
			{key(familyQueueTransition, accepted.QueueItem.ItemID, accepted.InitialTransition.TransitionID), transitionWire},
			{key(familyQueueProjection, accepted.QueueItem.ItemID), projectionWire},
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
		if e = scan(txn, familyLoopExecution, func(recordKey, wire []byte) error {
			parts, keyErr := decodeKeyParts(recordKey, familyLoopExecution)
			if keyErr != nil {
				return keyErr
			}
			if len(parts) == 2 && parts[1] == "binding" {
				return nil
			}
			if len(parts) != 1 {
				return fleet.ErrCorrupt
			}
			existing, decodeErr := execution.UnmarshalLoopExecution(wire)
			if decodeErr != nil {
				return corrupt(decodeErr)
			}
			if existing.GraphRunID == value.GraphRunID && existing.GraphNodeID == value.GraphNodeID {
				return fleet.ErrConflict
			}
			return nil
		}); e != nil {
			return e
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
		if claim.QueueItem != digestRef(item.ItemID, item.Digest) {
			return fleet.ErrConflict
		}
		if claim.Authority != item.Authority {
			bindingWire, found, bindErr := optional(txn, key(familyQueueRuntimeBinding, item.ItemID))
			if bindErr != nil || !found {
				return fleet.ErrConflict
			}
			binding, bindErr := queue.UnmarshalRuntimeBinding(bindingWire)
			if bindErr != nil || binding.Authority != claim.Authority || binding.QueueItem != claim.QueueItem {
				return fleet.ErrConflict
			}
		}
		projection, e := loadQueueProjection(txn, item.ItemID)
		if e != nil || validateClaimProjectionEligibility(txn, item, projection) != nil || projection.State != queue.StateQueued || projection.ActiveClaimID != "" || claim.ClaimedAt.Before(projection.AvailableAt) || projection.Attempts >= item.MaxAttempts {
			return fleet.ErrConflict
		}
		for _, dependency := range item.Dependencies {
			dependencyItem, loadErr := loadQueueItem(txn, dependency.ID)
			if loadErr != nil || dependency != digestRef(dependencyItem.ItemID, dependencyItem.Digest) {
				return fleet.ErrConflict
			}
			dependencyProjection, loadErr := loadQueueProjection(txn, dependency.ID)
			if loadErr != nil || validateSucceededDependency(txn, dependencyItem, dependencyProjection) != nil {
				return fleet.ErrConflict
			}
		}
		loopExecution, e := loadLoopExecution(txn, attempt.LoopExecutionID)
		if e != nil {
			return e
		}
		if attempt.AttemptID != claim.AttemptID || attempt.ClaimID != claim.ClaimID || attempt.AttemptNumber != projection.Attempts+1 || attempt.QueueItem != claim.QueueItem || attempt.GraphRunID != item.GraphRunID || loopExecution.GraphRunID != attempt.GraphRunID || transition.QueueItemID != item.ItemID || transition.ClaimID != claim.ClaimID || transition.From != queue.StateQueued || transition.To != queue.StateClaimed || transition.OccurredAt != claim.ClaimedAt {
			return fleet.ErrConflict
		}
		claimedProjection, e := queue.NewProjection(queue.Projection{QueueItemID: item.ItemID, State: queue.StateClaimed, Attempts: attempt.AttemptNumber, ActiveClaimID: claim.ClaimID, AvailableAt: projection.AvailableAt, LastTransitionID: transition.TransitionID, UpdatedAt: transition.OccurredAt})
		if e != nil {
			return e
		}
		projectionWire, e := queue.MarshalProjection(claimedProjection)
		if e != nil {
			return e
		}
		for _, entry := range []struct{ k, v []byte }{{key(familyClaim, claim.ClaimID), claimWire}, {key(familyAttempt, attempt.AttemptID), attemptWire}, {key(familyQueueTransition, item.ItemID, transition.TransitionID), transitionWire}} {
			if e = create(txn, entry.k, entry.v); e != nil {
				return e
			}
		}
		if e = txn.Set(key(familyClaimByItem, item.ItemID), []byte(claim.ClaimID)); e != nil {
			return e
		}
		if e = txn.Set(key(familyQueueProjection, item.ItemID), projectionWire); e != nil {
			return e
		}
		return appendAudit(txn, fact)
	})
}

func (s *Store) RetryQueueItem(ctx context.Context, mutation fleet.RetryMutation, fact fleet.AuditFact) error {
	retryWire, err := queue.MarshalRetry(mutation.Retry)
	if err != nil {
		return err
	}
	transitionWire, err := queue.MarshalTransition(mutation.Transition)
	if err != nil {
		return err
	}
	return s.update(ctx, func(txn *badgerdb.Txn) error {
		item, e := loadQueueItem(txn, mutation.Retry.QueueItem.ID)
		if e != nil || mutation.Retry.QueueItem != digestRef(item.ItemID, item.Digest) {
			return fleet.ErrConflict
		}
		projection, e := loadQueueProjection(txn, item.ItemID)
		if e != nil || validateProjectionBasis(txn, projection) != nil || projection.State != queue.StateClaimed || projection.ActiveClaimID != mutation.Retry.ClaimID || projection.Attempts != mutation.Retry.AttemptNumber || projection.Attempts >= item.MaxAttempts {
			return fleet.ErrConflict
		}
		claim, e := loadClaim(txn, mutation.Retry.ClaimID)
		if e != nil || claim.QueueItem != mutation.Retry.QueueItem {
			return fleet.ErrConflict
		}
		// No authoritative durable runtime-stop acknowledgement exists in this
		// model. Therefore only expiry-backed reclaim may release an active claim.
		if !mutation.Retry.Reclaimed || mutation.Retry.OccurredAt.Before(claim.ExpiresAt) {
			return fleet.ErrConflict
		}
		if mutation.Transition.QueueItemID != item.ItemID || mutation.Transition.From != queue.StateClaimed || mutation.Transition.To != queue.StateQueued || mutation.Transition.ClaimID != claim.ClaimID || mutation.Transition.Reason != mutation.Retry.Reason || mutation.Transition.OccurredAt != mutation.Retry.OccurredAt {
			return fleet.ErrConflict
		}
		next, e := queue.NewProjection(queue.Projection{QueueItemID: item.ItemID, State: queue.StateQueued, Attempts: projection.Attempts, AvailableAt: mutation.Retry.AvailableAt, LastTransitionID: mutation.Transition.TransitionID, UpdatedAt: mutation.Retry.OccurredAt})
		if e != nil {
			return e
		}
		projectionWire, e := queue.MarshalProjection(next)
		if e != nil {
			return e
		}
		if e = create(txn, key(familyQueueRetry, mutation.Retry.RetryID), retryWire); e != nil {
			return e
		}
		if e = create(txn, key(familyQueueTransition, item.ItemID, mutation.Transition.TransitionID), transitionWire); e != nil {
			return e
		}
		if e = txn.Delete(key(familyClaimByItem, item.ItemID)); e != nil {
			return e
		}
		if e = txn.Set(key(familyQueueProjection, item.ItemID), projectionWire); e != nil {
			return e
		}
		return appendAudit(txn, fact)
	})
}

func (s *Store) CancelQueueItem(ctx context.Context, mutation fleet.CancellationMutation, fact fleet.AuditFact) error {
	cancellationWire, err := queue.MarshalCancellation(mutation.Cancellation)
	if err != nil {
		return err
	}
	transitionWire, err := queue.MarshalTransition(mutation.Transition)
	if err != nil {
		return err
	}
	dispositionWire, err := disposition.Marshal(mutation.Disposition)
	if err != nil {
		return err
	}
	return s.update(ctx, func(txn *badgerdb.Txn) error {
		item, e := loadQueueItem(txn, mutation.Cancellation.QueueItem.ID)
		if e != nil || mutation.Cancellation.QueueItem != digestRef(item.ItemID, item.Digest) {
			return fleet.ErrConflict
		}
		projection, e := loadQueueProjection(txn, item.ItemID)
		allowedState := projection.State == queue.StateQueued || projection.State == queue.StateClaimed || projection.State == queue.StateAwaitingRuntime
		if e != nil || validateProjectionBasis(txn, projection) != nil || !allowedState {
			return fleet.ErrConflict
		}
		terminal := mutation.Transition.To == queue.StateCancelled || (mutation.Transition.To == queue.StateExpired && projection.State == queue.StateClaimed) || mutation.Transition.To == queue.StateRevoked || (mutation.Transition.To == queue.StateFailed && projection.State == queue.StateClaimed && projection.Attempts >= item.MaxAttempts)
		if mutation.Transition.QueueItemID != item.ItemID || mutation.Transition.From != projection.State || !terminal || mutation.Transition.OccurredAt != mutation.Cancellation.OccurredAt {
			return fleet.ErrConflict
		}
		expectedState := execution.StateCancelled
		if mutation.Transition.To == queue.StateExpired {
			expectedState = execution.StateExpired
		} else if mutation.Transition.To == queue.StateFailed {
			expectedState = execution.StateFailed
		} else if mutation.Transition.To == queue.StateRevoked {
			expectedState = execution.StateRevoked
		}
		if mutation.Cancellation.Reason != mutation.Transition.Reason || string(mutation.Disposition.ReasonCode) != mutation.Cancellation.Reason || mutation.Disposition.GraphRunID != item.GraphRunID || mutation.Disposition.QueueItem != mutation.Cancellation.QueueItem || mutation.Disposition.Authority != item.Authority || mutation.Disposition.State != expectedState || string(mutation.Disposition.ReasonCode) != mutation.Transition.Reason || mutation.Disposition.OccurredAt != mutation.Cancellation.OccurredAt {
			return fleet.ErrConflict
		}
		if projection.State == queue.StateClaimed {
			if mutation.Cancellation.ClaimID != projection.ActiveClaimID || mutation.Transition.ClaimID != projection.ActiveClaimID {
				return fleet.ErrConflict
			}
			claim, loadErr := loadClaim(txn, projection.ActiveClaimID)
			attemptWire, loadErr2 := get(txn, key(familyAttempt, claim.AttemptID))
			attempt, decodeErr := execution.UnmarshalAttempt(attemptWire)
			if loadErr != nil || loadErr2 != nil || decodeErr != nil || mutation.Disposition.AttemptID != attempt.AttemptID || mutation.Disposition.LoopExecutionID != attempt.LoopExecutionID || attempt.GraphRunID != item.GraphRunID {
				return fleet.ErrConflict
			}
		} else if mutation.Cancellation.ClaimID != "" || mutation.Transition.ClaimID != "" || mutation.Disposition.AttemptID != "" || mutation.Disposition.LoopExecutionID != "" {
			return fleet.ErrConflict
		}
		if _, found, loadErr := optional(txn, key(familyDispositionByRun, item.GraphRunID)); loadErr != nil || found {
			return fleet.ErrConflict
		}
		next, e := queue.NewProjection(queue.Projection{QueueItemID: item.ItemID, State: mutation.Transition.To, Attempts: projection.Attempts, AvailableAt: projection.AvailableAt, LastTransitionID: mutation.Transition.TransitionID, UpdatedAt: mutation.Cancellation.OccurredAt})
		if e != nil {
			return e
		}
		projectionWire, e := queue.MarshalProjection(next)
		if e != nil {
			return e
		}
		if e = create(txn, key(familyQueueCancellation, mutation.Cancellation.CancellationID), cancellationWire); e != nil {
			return e
		}
		if e = create(txn, key(familyQueueTransition, item.ItemID, mutation.Transition.TransitionID), transitionWire); e != nil {
			return e
		}
		if e = create(txn, key(familyDisposition, mutation.Disposition.DispositionID), dispositionWire); e != nil {
			return e
		}
		if e = create(txn, key(familyDispositionByRun, item.GraphRunID), []byte(mutation.Disposition.DispositionID)); e != nil {
			return e
		}
		if e = txn.Delete(key(familyClaimByItem, item.ItemID)); e != nil {
			return e
		}
		if e = txn.Set(key(familyQueueProjection, item.ItemID), projectionWire); e != nil {
			return e
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
func loadQueueProjection(txn *badgerdb.Txn, id string) (queue.Projection, error) {
	wire, err := get(txn, key(familyQueueProjection, id))
	if err != nil {
		return queue.Projection{}, err
	}
	v, err := queue.UnmarshalProjection(wire)
	if err != nil || v.QueueItemID != id {
		return queue.Projection{}, corrupt(err)
	}
	return v, nil
}
func validateProjectionBasis(txn *badgerdb.Txn, projection queue.Projection) error {
	wire, err := get(txn, key(familyQueueTransition, projection.QueueItemID, projection.LastTransitionID))
	if err != nil {
		return err
	}
	transition, err := queue.UnmarshalTransition(wire)
	if err != nil || transition.QueueItemID != projection.QueueItemID || transition.TransitionID != projection.LastTransitionID || transition.To != projection.State || transition.OccurredAt != projection.UpdatedAt {
		return fleet.ErrConflict
	}
	if projection.State == queue.StateClaimed && transition.ClaimID != projection.ActiveClaimID {
		return fleet.ErrConflict
	}
	return nil
}

// validateClaimProjectionEligibility binds every projection field that can
// grant a queued claim to immutable submission or retry facts in this same
// transaction. The projection remains a rebuildable read model only.
func validateClaimProjectionEligibility(txn *badgerdb.Txn, item queue.Item, projection queue.Projection) error {
	if validateProjectionBasis(txn, projection) != nil || projection.State != queue.StateQueued {
		return fleet.ErrConflict
	}
	transitionWire, err := get(txn, key(familyQueueTransition, item.ItemID, projection.LastTransitionID))
	if err != nil {
		return fleet.ErrConflict
	}
	transition, err := queue.UnmarshalTransition(transitionWire)
	if err != nil {
		return fleet.ErrConflict
	}
	if transition.From == "" {
		if projection.Attempts != 0 || projection.AvailableAt != item.AvailableAt {
			return fleet.ErrConflict
		}
		return nil
	}

	var matched *queue.Retry
	err = scan(txn, familyQueueRetry, func(_, wire []byte) error {
		retry, decodeErr := queue.UnmarshalRetry(wire)
		if decodeErr != nil {
			return corrupt(decodeErr)
		}
		if retry.QueueItem == digestRef(item.ItemID, item.Digest) && retry.ClaimID == transition.ClaimID && retry.OccurredAt == transition.OccurredAt && retry.Reason == transition.Reason {
			if matched != nil {
				return fleet.ErrConflict
			}
			matched = &retry
		}
		return nil
	})
	if err != nil || matched == nil || projection.Attempts != matched.AttemptNumber || projection.AvailableAt != matched.AvailableAt {
		return fleet.ErrConflict
	}
	claim, err := loadClaim(txn, matched.ClaimID)
	if err != nil || claim.QueueItem != matched.QueueItem {
		return fleet.ErrConflict
	}
	attemptWire, err := get(txn, key(familyAttempt, claim.AttemptID))
	if err != nil {
		return fleet.ErrConflict
	}
	attempt, err := execution.UnmarshalAttempt(attemptWire)
	if err != nil || attempt.ClaimID != claim.ClaimID || attempt.QueueItem != claim.QueueItem || attempt.AttemptNumber != matched.AttemptNumber {
		return fleet.ErrConflict
	}
	return nil
}

// validateSucceededDependency requires the terminal projection to agree with
// the canonical disposition, not merely with another derived transition.
func validateSucceededDependency(txn *badgerdb.Txn, item queue.Item, projection queue.Projection) error {
	if validateProjectionBasis(txn, projection) != nil || projection.State != queue.StateSucceeded {
		return fleet.ErrConflict
	}
	dispositionID, err := get(txn, key(familyDispositionByRun, item.GraphRunID))
	if err != nil {
		return fleet.ErrConflict
	}
	wire, err := get(txn, key(familyDisposition, string(dispositionID)))
	if err != nil {
		return fleet.ErrConflict
	}
	record, err := disposition.Unmarshal(wire)
	if err != nil || record.GraphRunID != item.GraphRunID || record.QueueItem != digestRef(item.ItemID, item.Digest) || record.State != execution.StateSucceeded || record.OccurredAt != projection.UpdatedAt {
		return fleet.ErrConflict
	}
	attemptWire, err := get(txn, key(familyAttempt, record.AttemptID))
	if err != nil {
		return fleet.ErrConflict
	}
	attempt, err := execution.UnmarshalAttempt(attemptWire)
	if err != nil || attempt.GraphRunID != item.GraphRunID || attempt.QueueItem != record.QueueItem || attempt.AttemptNumber != projection.Attempts {
		return fleet.ErrConflict
	}
	return nil
}
func loadClaim(txn *badgerdb.Txn, id string) (queue.Claim, error) {
	wire, err := get(txn, key(familyClaim, id))
	if err != nil {
		return queue.Claim{}, err
	}
	v, err := queue.UnmarshalClaim(wire)
	if err != nil || v.ClaimID != id {
		return queue.Claim{}, corrupt(err)
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

func (s *Store) ListSubmissions(ctx context.Context) (out []queue.Submission, err error) {
	out = []queue.Submission{}
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		return scan(txn, familySubmission, func(recordKey, wire []byte) error {
			item, decodeErr := queue.UnmarshalSubmission(wire)
			parts, keyErr := decodeKeyParts(recordKey, familySubmission)
			if decodeErr != nil || keyErr != nil || len(parts) != 1 || parts[0] != item.SubmissionID {
				return corrupt(errors.Join(decodeErr, keyErr))
			}
			out = append(out, item)
			return nil
		})
	})
	sort.Slice(out, func(i, j int) bool { return out[i].SubmittedAt.Before(out[j].SubmittedAt) })
	return
}

func (s *Store) ListRejections(ctx context.Context) (out []queue.Rejection, err error) {
	out = []queue.Rejection{}
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		return scan(txn, familyRejection, func(recordKey, wire []byte) error {
			item, decodeErr := queue.UnmarshalRejection(wire)
			parts, keyErr := decodeKeyParts(recordKey, familyRejection)
			if decodeErr != nil || keyErr != nil || len(parts) != 1 || parts[0] != item.RejectionID {
				return corrupt(errors.Join(decodeErr, keyErr))
			}
			out = append(out, item)
			return nil
		})
	})
	sort.Slice(out, func(i, j int) bool { return out[i].RejectedAt.Before(out[j].RejectedAt) })
	return
}
func (s *Store) GetQueueItem(ctx context.Context, id string) (queue.Item, error) {
	return readRecord(s, ctx, familyQueueItem, id, queue.UnmarshalItem, func(v queue.Item) string { return v.ItemID })
}
func (s *Store) GetQueueRuntimeBinding(ctx context.Context, itemID string) (queue.RuntimeBinding, error) {
	return readRecord(s, ctx, familyQueueRuntimeBinding, itemID, queue.UnmarshalRuntimeBinding, func(v queue.RuntimeBinding) string { return v.QueueItem.ID })
}

func (s *Store) BindQueueRuntime(ctx context.Context, binding queue.RuntimeBinding, transition queue.QueueTransition, fact fleet.AuditFact) (created bool, err error) {
	bindingWire, err := queue.MarshalRuntimeBinding(binding)
	if err != nil {
		return false, err
	}
	transitionWire, err := queue.MarshalTransition(transition)
	if err != nil {
		return false, err
	}
	err = s.update(ctx, func(txn *badgerdb.Txn) error {
		recordKey := key(familyQueueRuntimeBinding, binding.QueueItem.ID)
		if prior, found, loadErr := optional(txn, recordKey); loadErr != nil {
			return loadErr
		} else if found {
			if bytes.Equal(prior, bindingWire) {
				return nil
			}
			return fleet.ErrConflict
		}
		item, loadErr := loadQueueItem(txn, binding.QueueItem.ID)
		if loadErr != nil || binding.QueueItem != digestRef(item.ItemID, item.Digest) || binding.Submission != item.Submission {
			return fleet.ErrConflict
		}
		submissionWire, loadErr := get(txn, key(familySubmission, item.Submission.ID))
		if loadErr != nil {
			return loadErr
		}
		submission, loadErr := queue.UnmarshalSubmission(submissionWire)
		if loadErr != nil || submission.Digest != item.Submission.Digest || submission.AuthorityKind != "registered-agent-workspace" || submission.OwnerAgentID != binding.OwnerAgent.ID {
			return fleet.ErrConflict
		}
		projection, loadErr := loadQueueProjection(txn, item.ItemID)
		if loadErr != nil || projection.State != queue.StateAwaitingRuntime || transition.QueueItemID != item.ItemID || transition.From != queue.StateAwaitingRuntime || transition.To != queue.StateQueued || transition.OccurredAt != binding.BoundAt {
			return fleet.ErrConflict
		}
		next, loadErr := queue.NewProjection(queue.Projection{QueueItemID: item.ItemID, State: queue.StateQueued, Attempts: projection.Attempts, AvailableAt: binding.BoundAt, LastTransitionID: transition.TransitionID, UpdatedAt: binding.BoundAt})
		if loadErr != nil {
			return loadErr
		}
		projectionWire, loadErr := queue.MarshalProjection(next)
		if loadErr != nil {
			return loadErr
		}
		if loadErr = create(txn, recordKey, bindingWire); loadErr != nil {
			return loadErr
		}
		if loadErr = create(txn, key(familyQueueTransition, item.ItemID, transition.TransitionID), transitionWire); loadErr != nil {
			return loadErr
		}
		if loadErr = txn.Set(key(familyQueueProjection, item.ItemID), projectionWire); loadErr != nil {
			return loadErr
		}
		if loadErr = appendAudit(txn, fact); loadErr != nil {
			return loadErr
		}
		created = true
		return nil
	})
	return created, err
}
func (s *Store) ListQueueItems(ctx context.Context) (out []queue.Item, err error) {
	out = []queue.Item{}
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		return scan(txn, familyQueueItem, func(recordKey []byte, wire []byte) error {
			value, decodeErr := queue.UnmarshalItem(wire)
			if decodeErr != nil {
				return corrupt(decodeErr)
			}
			parts, keyErr := decodeKeyParts(recordKey, familyQueueItem)
			if keyErr != nil || len(parts) != 1 || parts[0] != value.ItemID {
				return fleet.ErrCorrupt
			}
			out = append(out, value)
			return nil
		})
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].EnqueuedAt.Equal(out[j].EnqueuedAt) {
			return out[i].ItemID < out[j].ItemID
		}
		return out[i].EnqueuedAt.Before(out[j].EnqueuedAt)
	})
	return
}
func (s *Store) GetGraphRun(ctx context.Context, id string) (execution.GraphRun, error) {
	return readRecord(s, ctx, familyGraphRun, id, execution.UnmarshalGraphRun, func(v execution.GraphRun) string { return v.GraphRunID })
}
func (s *Store) ListGraphRuns(ctx context.Context) (out []execution.GraphRun, err error) {
	out = []execution.GraphRun{}
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		return scan(txn, familyGraphRun, func(recordKey []byte, wire []byte) error {
			value, decodeErr := execution.UnmarshalGraphRun(wire)
			if decodeErr != nil {
				return corrupt(decodeErr)
			}
			parts, keyErr := decodeKeyParts(recordKey, familyGraphRun)
			if keyErr != nil || len(parts) != 1 || parts[0] != value.GraphRunID {
				return fleet.ErrCorrupt
			}
			out = append(out, value)
			return nil
		})
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].GraphRunID < out[j].GraphRunID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return
}
func (s *Store) GetLoopExecution(ctx context.Context, id string) (execution.LoopExecution, error) {
	return readRecord(s, ctx, familyLoopExecution, id, execution.UnmarshalLoopExecution, func(v execution.LoopExecution) string { return v.LoopExecutionID })
}
func (s *Store) ListLoopExecutions(ctx context.Context) (out []execution.LoopExecution, err error) {
	out = []execution.LoopExecution{}
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		return scan(txn, familyLoopExecution, func(recordKey []byte, wire []byte) error {
			parts, keyErr := decodeKeyParts(recordKey, familyLoopExecution)
			if keyErr != nil {
				return keyErr
			}
			if len(parts) == 2 && parts[1] == "binding" {
				return nil
			}
			if len(parts) != 1 {
				return fleet.ErrCorrupt
			}
			value, decodeErr := execution.UnmarshalLoopExecution(wire)
			if decodeErr != nil {
				return corrupt(decodeErr)
			}
			if parts[0] != value.LoopExecutionID {
				return fleet.ErrCorrupt
			}
			out = append(out, value)
			return nil
		})
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].LoopExecutionID < out[j].LoopExecutionID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return
}
func (s *Store) GetClaim(ctx context.Context, id string) (queue.Claim, error) {
	return readRecord(s, ctx, familyClaim, id, queue.UnmarshalClaim, func(v queue.Claim) string { return v.ClaimID })
}
func (s *Store) ListClaims(ctx context.Context) (out []queue.Claim, err error) {
	out = []queue.Claim{}
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		return scan(txn, familyClaim, func(recordKey, wire []byte) error {
			value, decodeErr := queue.UnmarshalClaim(wire)
			parts, keyErr := decodeKeyParts(recordKey, familyClaim)
			if decodeErr != nil || keyErr != nil || len(parts) != 1 || parts[0] != value.ClaimID {
				return corrupt(errors.Join(decodeErr, keyErr))
			}
			out = append(out, value)
			return nil
		})
	})
	sort.Slice(out, func(i, j int) bool { return out[i].ClaimedAt.Before(out[j].ClaimedAt) })
	return
}
func (s *Store) ListQueueTransitions(ctx context.Context, itemID string) (out []queue.QueueTransition, err error) {
	out = []queue.QueueTransition{}
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		return scan(txn, familyQueueTransition, func(recordKey, wire []byte) error {
			value, decodeErr := queue.UnmarshalTransition(wire)
			parts, keyErr := decodeKeyParts(recordKey, familyQueueTransition)
			if decodeErr != nil || keyErr != nil || len(parts) != 2 || parts[0] != value.QueueItemID || parts[1] != value.TransitionID {
				return corrupt(errors.Join(decodeErr, keyErr))
			}
			if value.QueueItemID == itemID {
				out = append(out, value)
			}
			return nil
		})
	})
	sort.Slice(out, func(i, j int) bool { return out[i].OccurredAt.Before(out[j].OccurredAt) })
	return
}
func (s *Store) ListQueueRetries(ctx context.Context, itemID string) (out []queue.Retry, err error) {
	out = []queue.Retry{}
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		return scan(txn, familyQueueRetry, func(recordKey, wire []byte) error {
			value, decodeErr := queue.UnmarshalRetry(wire)
			parts, keyErr := decodeKeyParts(recordKey, familyQueueRetry)
			if decodeErr != nil || keyErr != nil || len(parts) != 1 || parts[0] != value.RetryID {
				return corrupt(errors.Join(decodeErr, keyErr))
			}
			if value.QueueItem.ID == itemID {
				out = append(out, value)
			}
			return nil
		})
	})
	sort.Slice(out, func(i, j int) bool { return out[i].OccurredAt.Before(out[j].OccurredAt) })
	return
}
func (s *Store) ListQueueCancellations(ctx context.Context, itemID string) (out []queue.Cancellation, err error) {
	out = []queue.Cancellation{}
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		return scan(txn, familyQueueCancellation, func(recordKey, wire []byte) error {
			value, decodeErr := queue.UnmarshalCancellation(wire)
			parts, keyErr := decodeKeyParts(recordKey, familyQueueCancellation)
			if decodeErr != nil || keyErr != nil || len(parts) != 1 || parts[0] != value.CancellationID {
				return corrupt(errors.Join(decodeErr, keyErr))
			}
			if value.QueueItem.ID == itemID {
				out = append(out, value)
			}
			return nil
		})
	})
	sort.Slice(out, func(i, j int) bool { return out[i].OccurredAt.Before(out[j].OccurredAt) })
	return
}
func (s *Store) GetQueueProjection(ctx context.Context, id string) (out queue.Projection, err error) {
	err = s.view(ctx, func(txn *badgerdb.Txn) error { out, err = loadQueueProjection(txn, id); return err })
	return
}
func (s *Store) GetAttempt(ctx context.Context, id string) (execution.Attempt, error) {
	return readRecord(s, ctx, familyAttempt, id, execution.UnmarshalAttempt, func(v execution.Attempt) string { return v.AttemptID })
}
func (s *Store) ListAttempts(ctx context.Context) (out []execution.Attempt, err error) {
	out = []execution.Attempt{}
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		return scan(txn, familyAttempt, func(recordKey []byte, wire []byte) error {
			value, decodeErr := execution.UnmarshalAttempt(wire)
			if decodeErr != nil {
				return corrupt(decodeErr)
			}
			parts, keyErr := decodeKeyParts(recordKey, familyAttempt)
			if keyErr != nil || len(parts) != 1 || parts[0] != value.AttemptID {
				return fleet.ErrCorrupt
			}
			out = append(out, value)
			return nil
		})
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].AttemptID < out[j].AttemptID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return
}
