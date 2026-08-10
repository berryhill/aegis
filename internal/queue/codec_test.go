package queue

import (
	"bytes"
	"reflect"
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
	if _, err = NewTransition(QueueTransition{TransitionID: "transition-1", QueueItemID: "item-1", From: StateQueued, To: StateSucceeded, Reason: "illegal terminal shortcut", OccurredAt: now}); err == nil {
		t.Fatal("transition accepted an illegal queued-to-terminal shortcut")
	}
	dependencies := make([]reference.DigestRef, MaxDependencies+1)
	for i := range dependencies {
		dependencies[i] = digestRef("dependency-"+strings.Repeat("x", i+1), 'd')
	}
	if _, err = NewItem(Item{ItemID: "item-3", Submission: digestRef("submission-3", 'a'), Snapshot: digestRef("snapshot-3", 'b'), Authority: digestRef("authority-3", 'c'), GraphRunID: "run-3", MaxAttempts: 3, EnqueuedAt: now, AvailableAt: now, Dependencies: dependencies}); err == nil {
		t.Fatal("queue item accepted an unbounded dependency set")
	}
	duplicate := digestRef("dependency-1", 'd')
	if _, err = NewItem(Item{ItemID: "item-4", Submission: digestRef("submission-4", 'a'), Snapshot: digestRef("snapshot-4", 'b'), Authority: digestRef("authority-4", 'c'), GraphRunID: "run-4", MaxAttempts: 3, EnqueuedAt: now, AvailableAt: now, Dependencies: []reference.DigestRef{duplicate, duplicate}}); err == nil {
		t.Fatal("queue item accepted duplicate dependencies")
	}
}

func TestLifecycleRecordsRoundTripAndEnforceLeaseState(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	item := digestRef("item-1", 'a')
	retry := mustRetry(t, Retry{RetryID: "retry-1", QueueItem: item, ClaimID: "claim-1", AttemptNumber: 1, AvailableAt: now.Add(time.Minute), Reclaimed: true, Reason: "lease expired", OccurredAt: now})
	cancellation := mustCancellation(t, Cancellation{CancellationID: "cancel-1", QueueItem: item, ClaimID: "claim-1", Reason: "operator cancelled", OccurredAt: now})
	projection := mustProjection(t, Projection{QueueItemID: item.ID, State: StateClaimed, Attempts: 1, ActiveClaimID: "claim-1", AvailableAt: now, LastTransitionID: "transition-1", UpdatedAt: now})

	tests := []struct {
		name      string
		value     any
		marshal   func() ([]byte, error)
		unmarshal func([]byte) (any, error)
	}{
		{"retry", retry, func() ([]byte, error) { return MarshalRetry(retry) }, func(wire []byte) (any, error) { return UnmarshalRetry(wire) }},
		{"cancellation", cancellation, func() ([]byte, error) { return MarshalCancellation(cancellation) }, func(wire []byte) (any, error) { return UnmarshalCancellation(wire) }},
		{"projection", projection, func() ([]byte, error) { return MarshalProjection(projection) }, func(wire []byte) (any, error) { return UnmarshalProjection(wire) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wire, err := test.marshal()
			if err != nil {
				t.Fatal(err)
			}
			got, err := test.unmarshal(wire)
			if err != nil || !reflect.DeepEqual(got, test.value) {
				t.Fatalf("round trip: got=%+v want=%+v err=%v", got, test.value, err)
			}
			if _, err = test.unmarshal(bytes.Replace(wire, []byte(`"digest":`), []byte(`"unknown":true,"digest":`), 1)); err == nil {
				t.Fatal("strict decoder accepted unknown field")
			}
		})
	}

	if _, err := NewRetry(Retry{RetryID: "retry-invalid", QueueItem: item, ClaimID: "claim-1", AttemptNumber: 1, AvailableAt: now.Add(-time.Second), Reason: "negative backoff", OccurredAt: now}); err == nil {
		t.Fatal("retry accepted availability before its canonical decision time")
	}
	if _, err := NewProjection(Projection{QueueItemID: item.ID, State: StateQueued, ActiveClaimID: "claim-1", AvailableAt: now, LastTransitionID: "transition-1", UpdatedAt: now}); err == nil {
		t.Fatal("queued projection retained an active claim")
	}
}

func mustRetry(t *testing.T, value Retry) Retry {
	t.Helper()
	got, err := NewRetry(value)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func mustCancellation(t *testing.T, value Cancellation) Cancellation {
	t.Helper()
	got, err := NewCancellation(value)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func mustProjection(t *testing.T, value Projection) Projection {
	t.Helper()
	got, err := NewProjection(value)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func digestRef(id string, fill byte) reference.DigestRef {
	return reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: id, Digest: "sha256:" + strings.Repeat(string(fill), 64)}
}
