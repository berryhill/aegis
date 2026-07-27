package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/berryhill/aegis/internal/store"
)

const (
	ArtifactVerifierID = "aegis-artifact-verifier"
	VerifierPolicyV1   = "aegis.dev/artifact-verification/v1"
)

// BlobVerifier verifies an artifact through a fresh store read. It never
// receives runtime output bytes in memory.
type BlobVerifier struct {
	store *store.Store
	now   func() time.Time
}

func NewBlobVerifier(records *store.Store) (*BlobVerifier, error) {
	if records == nil {
		return nil, errors.New("artifact verifier store is required")
	}
	return &BlobVerifier{store: records, now: time.Now}, nil
}

func (v *BlobVerifier) Verify(ctx context.Context, artifact RuntimeArtifact, claim, expectedDigest string) (VerificationReceipt, error) {
	if err := ctx.Err(); err != nil {
		return VerificationReceipt{}, err
	}
	if artifact.ID == "" || artifact.ActionID == "" || artifact.RunID == "" || artifact.OwnerID == "" ||
		artifact.AuthorityContextID == "" || artifact.AuthorityContextDigest == "" || claim == "" || expectedDigest == "" {
		return VerificationReceipt{}, errors.New("artifact and verification binding are required")
	}
	receipt := VerificationReceipt{
		ID: store.ID("evidence"), ArtifactID: artifact.ID, ActionID: artifact.ActionID, RunID: artifact.RunID,
		OwnerID: artifact.OwnerID, AuthorityContextID: artifact.AuthorityContextID,
		AuthorityContextDigest: artifact.AuthorityContextDigest, VerifierID: ArtifactVerifierID,
		PolicyVersion: VerifierPolicyV1, Claim: claim, ExpectedDigest: expectedDigest,
		Outcome: Failed, ObservedAt: v.now().UTC(),
	}
	content, err := v.store.GetBlob(artifact.ContentRef)
	if err != nil {
		receipt.FailureCategory = "artifact_unreadable_or_corrupt"
	} else {
		receipt.ObservedDigest = sha256Reference(content)
		switch {
		case receipt.ObservedDigest != artifact.Digest || artifact.ContentRef != artifact.Digest:
			receipt.FailureCategory = "artifact_digest_mismatch"
		case receipt.ObservedDigest != expectedDigest:
			receipt.FailureCategory = "expected_output_mismatch"
		default:
			receipt.Outcome = Passed
		}
	}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		return VerificationReceipt{}, err
	}
	receipt.EvidenceRef, err = v.store.PutBlob(encoded)
	if err != nil {
		return VerificationReceipt{}, err
	}
	if !strings.HasPrefix(receipt.EvidenceRef, "sha256:") {
		return VerificationReceipt{}, errors.New("verifier store returned a non-content-addressed receipt")
	}
	return receipt, nil
}

func sha256Reference(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}
