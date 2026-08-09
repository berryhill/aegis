// Package evidence owns content-addressed runtime artifacts and claim-specific
// verifier receipts. Neither type grants authority or declares domain success.
package evidence

import (
	"errors"
	"strings"
	"time"
)

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

func (value RuntimeArtifact) Validate() error {
	if value.ID == "" || value.OwnerID == "" || value.ActionID == "" || value.RunID == "" || value.AuthorityContextID == "" || value.AuthorityContextDigest == "" || !strings.HasPrefix(value.Digest, "sha256:") || value.ContentRef != value.Digest || value.MediaType == "" || value.CreatedAt.IsZero() {
		return errors.New("invalid runtime artifact")
	}
	return nil
}

func (value VerificationReceipt) Validate() error {
	if value.ID == "" || value.ArtifactID == "" || value.ActionID == "" || value.RunID == "" || value.OwnerID == "" || value.AuthorityContextID == "" || value.AuthorityContextDigest == "" || value.VerifierID == "" || value.PolicyVersion == "" || value.Claim == "" || value.ExpectedDigest == "" || value.EvidenceRef == "" || value.ObservedAt.IsZero() || (value.Outcome != Passed && value.Outcome != Failed) {
		return errors.New("invalid verification receipt")
	}
	if value.Outcome == Passed && (value.FailureCategory != "" || value.ObservedDigest != value.ExpectedDigest) {
		return errors.New("invalid passing verification receipt")
	}
	if value.Outcome == Failed && value.FailureCategory == "" {
		return errors.New("failed verification receipt requires category")
	}
	return nil
}
