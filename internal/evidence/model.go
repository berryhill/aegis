// Package evidence owns content-addressed runtime artifacts and claim-specific
// verifier receipts. Neither type grants authority or declares domain success.
package evidence

import "time"

type Outcome string

const (
	Passed Outcome = "passed"
	Failed Outcome = "failed"
)

// RuntimeArtifact is distinct from core.Artifact, which remains the canonical
// provisioning artifact type.
type RuntimeArtifact struct {
	ID                     string    `json:"id"`
	OwnerID                string    `json:"owner_id"`
	ActionID               string    `json:"action_id"`
	RunID                  string    `json:"run_id"`
	AuthorityContextID     string    `json:"authority_context_id"`
	AuthorityContextDigest string    `json:"authority_context_digest"`
	Digest                 string    `json:"digest"`
	ContentRef             string    `json:"content_ref"`
	MediaType              string    `json:"media_type"`
	CreatedAt              time.Time `json:"created_at"`
}

// VerificationReceipt binds a verifier claim to one artifact, action, run, and
// authority context so evidence cannot be replayed across runs.
type VerificationReceipt struct {
	ID                     string    `json:"id"`
	ArtifactID             string    `json:"artifact_id"`
	ActionID               string    `json:"action_id"`
	RunID                  string    `json:"run_id"`
	OwnerID                string    `json:"owner_id"`
	AuthorityContextID     string    `json:"authority_context_id"`
	AuthorityContextDigest string    `json:"authority_context_digest"`
	VerifierID             string    `json:"verifier_id"`
	PolicyVersion          string    `json:"policy_version"`
	Claim                  string    `json:"claim"`
	ExpectedDigest         string    `json:"expected_digest"`
	ObservedDigest         string    `json:"observed_digest,omitempty"`
	Outcome                Outcome   `json:"outcome"`
	FailureCategory        string    `json:"failure_category,omitempty"`
	EvidenceRef            string    `json:"evidence_ref"`
	ObservedAt             time.Time `json:"observed_at"`
}
