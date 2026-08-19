package badger

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/berryhill/aegis/internal/disposition"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	queue "github.com/berryhill/aegis/internal/queue"
	badgerdb "github.com/dgraph-io/badger/v4"
)

// CompleteQueueItem atomically publishes immutable evidence metadata, the
// terminal disposition, the queue transition, and authoritative audit fact.
func (s *Store) CompleteQueueItem(ctx context.Context, completion fleet.Completion, fact fleet.AuditFact, evidenceReader fleet.EvidenceReader) error {
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
		if evidenceReader == nil {
			return fleet.ErrConflict
		}
		content, readErr := evidenceReader.GetBlob(completion.Artifact.ContentRef)
		sum := sha256.Sum256(content)
		if readErr != nil || "sha256:"+hex.EncodeToString(sum[:]) != completion.Artifact.Digest || !evidence.ValidateCompletionProvenance(completion.Provenance, *completion.Artifact, completion.Receipts) {
			return fleet.ErrConflict
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
		if evidenceReader == nil {
			return fleet.ErrConflict
		}
		content, readErr := evidenceReader.GetBlob(receipt.EvidenceRef)
		reloaded, reloadErr := evidence.DecodeVerificationReceipt(receipt.EvidenceRef, content)
		if readErr != nil || reloadErr != nil || reloaded != receipt {
			return fleet.ErrConflict
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
		if completion.Disposition.Authority != storedClaim.Authority || completion.Transition.QueueItemID != storedClaim.QueueItem.ID || completion.Transition.ClaimID != storedClaim.ClaimID || completion.Transition.From != queue.StateClaimed || queueState(completion.Disposition.State) != completion.Transition.To || completion.Transition.Reason != completion.Disposition.ReasonCode || !completion.Transition.OccurredAt.Equal(completion.Disposition.OccurredAt) {
			return fleet.ErrConflict
		}
		if fact.Event.Type != "fleet.disposition.recorded" || fact.Event.Reason != completion.Disposition.ReasonCode || fact.Event.Outcome != string(completion.Disposition.State) || fact.Event.Metadata["disposition_id"] != completion.Disposition.DispositionID || fact.Event.Metadata["transition_id"] != completion.Transition.TransitionID || fact.Event.Metadata["terminal_at"] != completion.Disposition.OccurredAt.UTC().Format(time.RFC3339Nano) {
			return fleet.ErrConflict
		}
		loopExecutionWire, e := get(txn, key(familyLoopExecution, attempt.LoopExecutionID))
		if e != nil {
			return e
		}
		loopExecution, e := execution.UnmarshalLoopExecution(loopExecutionWire)
		if e != nil || loopExecution.Participant.ID == "" || fact.Event.AgentID != loopExecution.Participant.ID || fact.Event.StanzaID == "" {
			return fleet.ErrConflict
		}
		itemWire, e := get(txn, key(familyQueueItem, storedClaim.QueueItem.ID))
		if e != nil {
			return e
		}
		item, e := queue.UnmarshalItem(itemWire)
		if e != nil {
			return fleet.ErrConflict
		}
		submissionWire, e := get(txn, key(familySubmission, item.Submission.ID))
		if e != nil {
			return e
		}
		submission, e := queue.UnmarshalSubmission(submissionWire)
		if e != nil || fact.Event.MandateID != submission.MandateID {
			return fleet.ErrConflict
		}
		if _, found, e := optional(txn, key(familyDispositionByRun, completion.Disposition.GraphRunID)); e != nil {
			return e
		} else if found {
			return fleet.ErrConflict
		}
		if completion.Artifact != nil {
			if len(completion.Disposition.ArtifactIDs) != 1 || completion.Disposition.ArtifactIDs[0] != completion.Artifact.ID || completion.Artifact.AttemptID != attempt.AttemptID || completion.Artifact.AuthorityContextID != storedClaim.Authority.ID || completion.Artifact.AuthorityContextDigest != storedClaim.Authority.Digest || completion.Artifact.RunID != attempt.LoopExecutionID {
				return fleet.ErrConflict
			}
		} else if len(completion.Disposition.ArtifactIDs) != 0 {
			return fleet.ErrConflict
		}
		if len(completion.Disposition.ReceiptIDs) != len(completion.Receipts) || (len(completion.Receipts) > 0 && completion.Artifact == nil) {
			return fleet.ErrConflict
		}
		allPassed := len(completion.Receipts) > 0
		for index, receipt := range completion.Receipts {
			if receipt.ID != completion.Disposition.ReceiptIDs[index] || receipt.AttemptID != attempt.AttemptID || receipt.AttemptID != completion.Artifact.AttemptID || receipt.ArtifactID != completion.Artifact.ID || receipt.ActionID != completion.Artifact.ActionID || receipt.RunID != completion.Artifact.RunID || receipt.OwnerID != completion.Artifact.OwnerID || receipt.AuthorityContextID != completion.Artifact.AuthorityContextID || receipt.AuthorityContextDigest != completion.Artifact.AuthorityContextDigest {
				return fleet.ErrConflict
			}
			allPassed = allPassed && receipt.Outcome == evidence.Passed && receipt.ObservedDigest == completion.Artifact.Digest
		}
		if completion.Disposition.State == execution.StateSucceeded {
			if completion.Artifact == nil || !allPassed || !exactRequiredEvidence(txn, attempt, completion) {
				return fleet.ErrConflict
			}
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

func exactRequiredEvidence(txn *badgerdb.Txn, attempt execution.Attempt, completion fleet.Completion) bool {
	executionWire, err := get(txn, key(familyLoopExecution, attempt.LoopExecutionID))
	if err != nil {
		return false
	}
	loopExecution, err := execution.UnmarshalLoopExecution(executionWire)
	if err != nil || loopExecution.LoopExecutionID != attempt.LoopExecutionID || completion.Artifact.OwnerID != loopExecution.Participant.ID {
		return false
	}
	revisionWire, err := get(txn, key(familyLoopRevision, loopExecution.Loop.ID, revisionPart(loopExecution.Loop.Revision)))
	if err != nil {
		return false
	}
	revision, err := loop.UnmarshalRevision(revisionWire)
	if err != nil || revision.Digest != loopExecution.Loop.Digest || len(revision.RequiredEvidence) != len(completion.Receipts) {
		return false
	}
	claims := map[string]loop.EvidenceClaim{}
	for _, step := range revision.Steps {
		if step.ID == completion.Artifact.ActionID {
			for _, claim := range step.EvidenceClaims {
				claims[claim.Claim] = claim
			}
		}
	}
	receipts := map[string]evidence.VerificationReceipt{}
	for _, receipt := range completion.Receipts {
		if _, duplicate := receipts[receipt.Claim]; duplicate {
			return false
		}
		receipts[receipt.Claim] = receipt
	}
	for _, requirement := range revision.RequiredEvidence {
		claim, declared := claims[requirement.Claim]
		receipt, present := receipts[requirement.Claim]
		if !declared || !present || requirement.ProducerStepID != completion.Artifact.ActionID || receipt.Outcome != evidence.Passed || receipt.VerifierID != claim.VerifierID || receipt.PolicyVersion != claim.PolicyVersion || receipt.MediaType != claim.MediaType || receipt.ExpectedDigest != claim.ExpectedDigest {
			return false
		}
	}
	return true
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
	case execution.StateRevoked:
		return queue.StateRevoked
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

func (s *Store) GetDispositionByGraphRun(ctx context.Context, graphRunID string) (out disposition.Record, err error) {
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		id, e := get(txn, key(familyDispositionByRun, graphRunID))
		if e != nil {
			return e
		}
		wire, e := get(txn, key(familyDisposition, string(id)))
		if e != nil {
			return e
		}
		out, e = disposition.Unmarshal(wire)
		if e != nil || out.GraphRunID != graphRunID || out.DispositionID != string(id) {
			return corrupt(e)
		}
		return nil
	})
	return
}
