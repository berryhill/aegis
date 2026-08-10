// Package queue owns immutable submission and execution-queue lifecycle facts.
// Records bind exact persisted facts but never grant authority by themselves.
package queue

import (
	"time"

	"github.com/berryhill/aegis/internal/reference"
)

const (
	SubmissionSchemaVersion   = "aegis.queue.submission.v1"
	RejectionSchemaVersion    = "aegis.queue.rejection.v1"
	ItemSchemaVersion         = "aegis.queue.item.v1"
	ClaimSchemaVersion        = "aegis.queue.claim.v1"
	TransitionSchemaVersion   = "aegis.queue.transition.v1"
	RetrySchemaVersion        = "aegis.queue.retry.v1"
	CancellationSchemaVersion = "aegis.queue.cancellation.v1"
	ProjectionSchemaVersion   = "aegis.queue.projection.v1"
	MaxReasonBytes            = 1024
	MaxAttempts               = 100
	MaxDependencies           = 100
)

type State string

const (
	StateQueued    State = "queued"
	StateClaimed   State = "claimed"
	StateSucceeded State = "succeeded"
	StateFailed    State = "failed"
	StateDenied    State = "denied"
	StateCancelled State = "cancelled"
	StateExpired   State = "expired"
)

// Submission is the immutable admitted request. Authority is an exact context
// reference supplied by an authenticated application boundary, not by a model.
type Submission struct {
	SchemaVersion  string              `json:"schema_version"`
	SubmissionID   string              `json:"submission_id"`
	IdempotencyKey string              `json:"idempotency_key"`
	Snapshot       reference.DigestRef `json:"snapshot"`
	Authority      reference.DigestRef `json:"authority"`
	MandateID      string              `json:"mandate_id"`
	Runtime        string              `json:"runtime"`
	SubmittedAt    time.Time           `json:"submitted_at"`
	Digest         string              `json:"digest"`
}

// Rejection is durable negative admission truth. It cannot be retried or
// converted into a queue item in place.
type Rejection struct {
	SchemaVersion  string    `json:"schema_version"`
	RejectionID    string    `json:"rejection_id"`
	SubmissionID   string    `json:"submission_id"`
	IdempotencyKey string    `json:"idempotency_key"`
	ReasonCode     string    `json:"reason_code"`
	Reason         string    `json:"reason"`
	RejectedAt     time.Time `json:"rejected_at"`
	Digest         string    `json:"digest"`
}

// Item is the one durable queue entry created for an accepted submission.
type Item struct {
	SchemaVersion string                `json:"schema_version"`
	ItemID        string                `json:"item_id"`
	Submission    reference.DigestRef   `json:"submission"`
	Snapshot      reference.DigestRef   `json:"snapshot"`
	Authority     reference.DigestRef   `json:"authority"`
	GraphRunID    string                `json:"graph_run_id"`
	State         State                 `json:"state"`
	MaxAttempts   uint32                `json:"max_attempts"`
	EnqueuedAt    time.Time             `json:"enqueued_at"`
	AvailableAt   time.Time             `json:"available_at"`
	Dependencies  []reference.DigestRef `json:"dependencies,omitempty"`
	Digest        string                `json:"digest"`
}

// Claim is an immutable bounded lease. Repository claim creation is the
// single-winner operation; possession of this record is not runtime admission.
type Claim struct {
	SchemaVersion string              `json:"schema_version"`
	ClaimID       string              `json:"claim_id"`
	QueueItem     reference.DigestRef `json:"queue_item"`
	AttemptID     string              `json:"attempt_id"`
	WorkerID      string              `json:"worker_id"`
	Authority     reference.DigestRef `json:"authority"`
	ClaimedAt     time.Time           `json:"claimed_at"`
	ExpiresAt     time.Time           `json:"expires_at"`
	Digest        string              `json:"digest"`
}

// Transition is append-only queue history. Initial queue and claim facts are
// persisted atomically with the records whose state they describe.
type QueueTransition struct {
	SchemaVersion string    `json:"schema_version"`
	TransitionID  string    `json:"transition_id"`
	QueueItemID   string    `json:"queue_item_id"`
	From          State     `json:"from,omitempty"`
	To            State     `json:"to"`
	ClaimID       string    `json:"claim_id,omitempty"`
	Reason        string    `json:"reason"`
	OccurredAt    time.Time `json:"occurred_at"`
	Digest        string    `json:"digest"`
}

// Retry is the canonical decision to make a claimed item available again.
type Retry struct {
	SchemaVersion string              `json:"schema_version"`
	RetryID       string              `json:"retry_id"`
	QueueItem     reference.DigestRef `json:"queue_item"`
	ClaimID       string              `json:"claim_id"`
	AttemptNumber uint32              `json:"attempt_number"`
	AvailableAt   time.Time           `json:"available_at"`
	Reclaimed     bool                `json:"reclaimed"`
	Reason        string              `json:"reason"`
	OccurredAt    time.Time           `json:"occurred_at"`
	Digest        string              `json:"digest"`
}

// Cancellation is an immutable controller-admitted cancellation request.
type Cancellation struct {
	SchemaVersion  string              `json:"schema_version"`
	CancellationID string              `json:"cancellation_id"`
	QueueItem      reference.DigestRef `json:"queue_item"`
	ClaimID        string              `json:"claim_id,omitempty"`
	Reason         string              `json:"reason"`
	OccurredAt     time.Time           `json:"occurred_at"`
	Digest         string              `json:"digest"`
}

// Projection is rebuildable and cannot grant authority. Mutations always
// re-check canonical records in the same transaction.
type Projection struct {
	SchemaVersion    string    `json:"schema_version"`
	QueueItemID      string    `json:"queue_item_id"`
	State            State     `json:"state"`
	Attempts         uint32    `json:"attempts"`
	ActiveClaimID    string    `json:"active_claim_id,omitempty"`
	AvailableAt      time.Time `json:"available_at"`
	LastTransitionID string    `json:"last_transition_id"`
	UpdatedAt        time.Time `json:"updated_at"`
	Digest           string    `json:"digest"`
}
