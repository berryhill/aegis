package queue

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/reference"
)

func TestQueueCanonicalRecordsRoundTripAndRejectTampering(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	submission, err := NewSubmission(Submission{
		SubmissionID:   "submission-1",
		IdempotencyKey: "submit-key-1",
		Snapshot:       digestRef("snapshot-1", 'a'),
		Authority:      digestRef("authority-1", 'b'),
		MandateID:      "mandate-1",
		Runtime:        "hermes-agent",
		SubmittedAt:    now,
	})
	if err != nil {
		t.Fatal(err)
	}
	wire, err := MarshalSubmission(submission)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := UnmarshalSubmission(wire)
	if err != nil || decoded != submission {
		t.Fatalf("submission round trip: got=%+v err=%v", decoded, err)
	}

	tampered := bytes.Replace(wire, []byte("mandate-1"), []byte("mandate-2"), 1)
	if _, err = UnmarshalSubmission(tampered); err == nil {
		t.Fatal("digest-validating decoder accepted semantic tampering")
	}
	duplicate := bytes.Replace(wire, []byte(`"submission_id":"submission-1"`), []byte(`"submission_id":"submission-1","submission_id":"submission-2"`), 1)
	if _, err = UnmarshalSubmission(duplicate); err == nil {
		t.Fatal("strict decoder accepted a duplicate object key")
	}
	unknown := bytes.Replace(wire, []byte(`"digest":`), []byte(`"unknown":true,"digest":`), 1)
	if _, err = UnmarshalSubmission(unknown); err == nil {
		t.Fatal("strict decoder accepted an unknown field")
	}
}

func TestQueueConstructorsEnforceInitialStatesAndBounds(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	item, err := NewItem(Item{
		ItemID:      "item-1",
		Submission:  digestRef("submission-1", 'a'),
		Snapshot:    digestRef("snapshot-1", 'b'),
		Authority:   digestRef("authority-1", 'c'),
		GraphRunID:  "run-1",
		State:       StateClaimed,
		MaxAttempts: 3,
		EnqueuedAt:  now,
		AvailableAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.State != StateQueued {
		t.Fatalf("item constructor did not force queued initial state: %q", item.State)
	}

	if _, err = NewItem(Item{ItemID: "item-2", Submission: digestRef("submission-2", 'a'), Snapshot: digestRef("snapshot-2", 'b'), Authority: digestRef("authority-2", 'c'), GraphRunID: "run-2", MaxAttempts: MaxAttempts + 1, EnqueuedAt: now, AvailableAt: now}); err == nil {
		t.Fatal("queue item accepted an unbounded retry count")
	}
	if _, err = NewClaim(Claim{ClaimID: "claim-1", QueueItem: digestRef("item-1", 'a'), AttemptID: "attempt-1", WorkerID: "worker-1", Authority: digestRef("authority-1", 'b'), ClaimedAt: now, ExpiresAt: now}); err == nil {
		t.Fatal("claim accepted a non-positive lease")
	}
	if _, err = NewRejection(Rejection{RejectionID: "reject-1", SubmissionID: "submission-1", IdempotencyKey: "submit-key-1", ReasonCode: "denied", Reason: strings.Repeat("x", MaxReasonBytes+1), RejectedAt: now}); err == nil {
		t.Fatal("rejection accepted an oversized reason")
	}
	if _, err = NewTransition(QueueTransition{TransitionID: "transition-1", QueueItemID: "item-1", From: StateClaimed, To: StateQueued, ClaimID: "claim-1", Reason: "illegal rewind", OccurredAt: now}); err == nil {
		t.Fatal("transition accepted a non-initial queue transition")
	}
}

func digestRef(id string, fill byte) reference.DigestRef {
	return reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: id, Digest: "sha256:" + strings.Repeat(string(fill), 64)}
}
