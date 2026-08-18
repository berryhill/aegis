package evidence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
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

func (v *BlobVerifier) Verify(ctx context.Context, artifact RuntimeArtifact, policy VerificationPolicy) (VerificationReceipt, error) {
	if err := ctx.Err(); err != nil {
		return VerificationReceipt{}, err
	}
	if artifact.ID == "" || artifact.ActionID == "" || artifact.RunID == "" || artifact.OwnerID == "" ||
		artifact.AuthorityContextID == "" || artifact.AuthorityContextDigest == "" || policy.Validate() != nil {
		return VerificationReceipt{}, errors.New("artifact and verification binding are required")
	}
	receipt := VerificationReceipt{
		ID: store.ID("evidence"), AttemptID: artifact.AttemptID, ArtifactID: artifact.ID, ActionID: artifact.ActionID, RunID: artifact.RunID,
		OwnerID: artifact.OwnerID, AuthorityContextID: artifact.AuthorityContextID,
		AuthorityContextDigest: artifact.AuthorityContextDigest, VerifierID: ArtifactVerifierID,
		PolicyVersion: VerifierPolicyV1, Claim: policy.Claim, MediaType: policy.MediaType, ExpectedDigest: policy.ExpectedDigest,
		Outcome: Failed, ObservedAt: v.now().UTC(),
	}
	content, readErr := v.store.GetBlob(artifact.ContentRef)
	switch {
	case policy.VerifierID != ArtifactVerifierID || policy.PolicyVersion != VerifierPolicyV1:
		receipt.FailureCategory = "verifier_policy_mismatch"
	case artifact.MediaType != policy.MediaType:
		receipt.FailureCategory = "media_type_mismatch"
	case readErr != nil:
		receipt.FailureCategory = "artifact_unreadable_or_corrupt"
	default:
		receipt.ObservedDigest = sha256Reference(content)
		switch {
		case receipt.ObservedDigest != artifact.Digest || artifact.ContentRef != artifact.Digest:
			receipt.FailureCategory = "artifact_digest_mismatch"
		case receipt.ObservedDigest != policy.ExpectedDigest:
			receipt.FailureCategory = "expected_output_mismatch"
		default:
			receipt.Outcome = Passed
		}
	}
	// EvidenceRef cannot be embedded in the bytes it addresses. Persist the
	// canonical receipt with an empty reference and bind the returned record to
	// that content address. ReloadReceipt performs the inverse operation.
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

// ReloadReceipt independently reloads and validates content-addressed receipt
// evidence without trusting the fleet receipt projection.
func (v *BlobVerifier) ReloadReceipt(ctx context.Context, ref string) (VerificationReceipt, error) {
	if err := ctx.Err(); err != nil {
		return VerificationReceipt{}, err
	}
	content, err := v.store.GetBlob(ref)
	if err != nil {
		return VerificationReceipt{}, errors.New("verification receipt evidence is unreadable or corrupt")
	}
	return DecodeVerificationReceipt(ref, content)
}

// DecodeVerificationReceipt validates the content address and canonical
// self-reference convention before returning independently loaded evidence.
func DecodeVerificationReceipt(ref string, content []byte) (VerificationReceipt, error) {
	if sha256Reference(content) != ref {
		return VerificationReceipt{}, errors.New("verification receipt evidence is unreadable or corrupt")
	}
	var receipt VerificationReceipt
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&receipt); err != nil || receipt.EvidenceRef != "" {
		return VerificationReceipt{}, errors.New("verification receipt evidence is malformed")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return VerificationReceipt{}, errors.New("verification receipt evidence has trailing data")
	}
	canonical, err := json.Marshal(receipt)
	if err != nil || !bytes.Equal(canonical, content) {
		return VerificationReceipt{}, errors.New("verification receipt evidence is non-canonical")
	}
	receipt.EvidenceRef = ref
	if err := receipt.Validate(); err != nil {
		return VerificationReceipt{}, err
	}
	return receipt, nil
}

// CompletionProvenance is an opaque verifier-minted capability. Its fields are
// intentionally inaccessible outside this package so completion callers cannot
// mint evidence from projections they control.
type CompletionProvenance struct{ digest string }

// AuthorizeCompletion independently reloads the artifact and every receipt at
// the final verification boundary and seals their exact projections.
func (v *BlobVerifier) AuthorizeCompletion(ctx context.Context, artifact RuntimeArtifact, receipts []VerificationReceipt) (CompletionProvenance, error) {
	if err := ctx.Err(); err != nil {
		return CompletionProvenance{}, err
	}
	content, err := v.store.GetBlob(artifact.ContentRef)
	if err != nil || sha256Reference(content) != artifact.Digest || artifact.ContentRef != artifact.Digest {
		return CompletionProvenance{}, errors.New("artifact is unreadable or corrupt at completion")
	}
	for _, receipt := range receipts {
		reloaded, reloadErr := v.ReloadReceipt(ctx, receipt.EvidenceRef)
		if reloadErr != nil || reloaded != receipt {
			return CompletionProvenance{}, errors.New("receipt is unreadable or substituted at completion")
		}
	}
	return completionProvenance(artifact, receipts), nil
}

func ValidateCompletionProvenance(provenance CompletionProvenance, artifact RuntimeArtifact, receipts []VerificationReceipt) bool {
	return provenance.digest != "" && provenance == completionProvenance(artifact, receipts)
}

func completionProvenance(artifact RuntimeArtifact, receipts []VerificationReceipt) CompletionProvenance {
	wire, _ := json.Marshal(struct {
		Artifact RuntimeArtifact       `json:"artifact"`
		Receipts []VerificationReceipt `json:"receipts"`
	}{artifact, receipts})
	return CompletionProvenance{digest: sha256Reference(wire)}
}

func sha256Reference(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}
