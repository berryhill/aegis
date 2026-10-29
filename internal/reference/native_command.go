package reference

import "errors"

// NativeCommandBinding is inert command lineage, never authority or completion proof.
type NativeCommandBinding struct {
	IntentID      string    `json:"intent_id"`
	RunID         string    `json:"run_id"`
	QueueItemID   string    `json:"queue_item_id"`
	ClaimID       string    `json:"claim_id"`
	AttemptID     string    `json:"attempt_id"`
	Role          string    `json:"role"`
	Epoch         uint64    `json:"epoch"`
	Authority     DigestRef `json:"authority"`
	Candidate     string    `json:"candidate"`
	PolicyDigest  string    `json:"policy_digest"`
	CommandDigest string    `json:"command_digest"`
}

// ErrNativeQuiescenceUnavailable is shared without importing an execution owner.
var ErrNativeQuiescenceUnavailable = errors.New("contribution native quiescence unavailable")
