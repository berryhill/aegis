package poc

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/berryhill/aegis/internal/plumbing"
	"github.com/berryhill/aegis/internal/store"
)

// BlobArtifactVerifier verifies bytes through a fresh create-only-store read.
// It never receives the Hermes in-memory output used to create the artifact.
type BlobArtifactVerifier struct {
	store *store.Store
	now   func() time.Time
}

func NewBlobArtifactVerifier(records *store.Store) (*BlobArtifactVerifier, error) {
	if records == nil {
		return nil, errors.New("artifact verifier store is required")
	}
	return &BlobArtifactVerifier{store: records, now: time.Now}, nil
}

type verificationReceipt struct {
	Version         string                       `json:"version"`
	ArtifactID      string                       `json:"artifact_id"`
	ClaimedDigest   string                       `json:"claimed_digest"`
	ObservedDigest  string                       `json:"observed_digest,omitempty"`
	ExpectedDigest  string                       `json:"expected_digest"`
	Outcome         plumbing.VerificationOutcome `json:"outcome"`
	FailureCategory string                       `json:"failure_category,omitempty"`
}

func (v *BlobArtifactVerifier) Verify(ctx context.Context, artifact plumbing.Artifact, expectedDigest string) (plumbing.VerificationEvidence, error) {
	if err := ctx.Err(); err != nil {
		return plumbing.VerificationEvidence{}, err
	}
	at := v.now().UTC()
	receipt := verificationReceipt{
		Version: "aegis.dev/artifact-verification/v1alpha1", ArtifactID: artifact.ID,
		ClaimedDigest: artifact.Digest, ExpectedDigest: expectedDigest, Outcome: plumbing.VerificationFailed,
	}
	content, err := v.store.GetBlob(artifact.ContentRef)
	if err != nil {
		receipt.FailureCategory = "artifact_unreadable_or_corrupt"
	} else {
		receipt.ObservedDigest = digest(content)
		if receipt.ObservedDigest == artifact.Digest && receipt.ObservedDigest == expectedDigest && artifact.ContentRef == "sha256:"+artifact.Digest {
			receipt.Outcome = plumbing.VerificationPassed
		} else if receipt.ObservedDigest != artifact.Digest || artifact.ContentRef != "sha256:"+artifact.Digest {
			receipt.FailureCategory = "artifact_digest_mismatch"
		} else {
			receipt.FailureCategory = "expected_output_mismatch"
		}
	}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		return plumbing.VerificationEvidence{}, err
	}
	reference, err := v.store.PutBlob(encoded)
	if err != nil {
		return plumbing.VerificationEvidence{}, err
	}
	return plumbing.VerificationEvidence{
		ID: store.ID("evidence"), SubjectKind: "artifact", SubjectID: artifact.ID,
		Verifier: plumbing.VerifierArtifact, Outcome: receipt.Outcome,
		Digest: strings.TrimPrefix(reference, "sha256:"), EvidenceRef: reference,
		ObservedAt: at,
		Provenance: plumbing.Provenance{OwnerID: artifact.OwnerID, Producer: plumbing.ProducerVerifier, SourceRef: reference, RecordedAt: at},
	}, nil
}
