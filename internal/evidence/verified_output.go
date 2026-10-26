package evidence

import (
	"context"
	"errors"
)

// OutputBlobReader admits only exact content-addressed reads, never paths or
// caller-supplied replacement bytes. It grants no runtime or filesystem access.
type OutputBlobReader interface{ GetBlob(string) ([]byte, error) }

// VerifiedOutputBinding is pinned by the controller from exact historical
// node/attempt/Loop records, not inferred from the artifact being checked. This
// narrow raw-output boundary requires one precommitted artifact-verifier claim.
// Selected-file proof envelopes are not raw output and must not be silently
// treated as it; their separately qualified adapters retain their own policy.
type VerifiedOutputBinding struct {
	ArtifactID             string
	AttemptID              string
	RunID                  string
	ActionID               string
	OwnerID                string
	AuthorityContextID     string
	AuthorityContextDigest string
	Policy                 VerificationPolicy
}

// ReadVerifiedOutput independently reloads the artifact and its sole required
// receipt and compares every causal and policy field against controller-pinned
// expectations. Matching bytes do not authenticate an artifact's lineage.
// Success returns actual reloaded bytes, not the expected digest or expected
// text. This is a read/check, not a disposition or fresh authority grant.
func ReadVerifiedOutput(ctx context.Context, blobs OutputBlobReader, artifact RuntimeArtifact, receipts []VerificationReceipt, expected VerifiedOutputBinding) ([]byte, error) {
	denied := errors.New("exact verified output unavailable")
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if blobs == nil || artifact.Validate() != nil || expected.Policy.Validate() != nil ||
		expected.Policy.VerifierID != ArtifactVerifierID || expected.Policy.PolicyVersion != VerifierPolicyV1 ||
		artifact.ID != expected.ArtifactID || artifact.AttemptID != expected.AttemptID || artifact.RunID != expected.RunID || artifact.ActionID != expected.ActionID || artifact.OwnerID != expected.OwnerID ||
		artifact.AuthorityContextID != expected.AuthorityContextID || artifact.AuthorityContextDigest != expected.AuthorityContextDigest ||
		artifact.Digest != expected.Policy.ExpectedDigest || artifact.MediaType != expected.Policy.MediaType || len(receipts) != 1 {
		return nil, denied
	}
	receipt := receipts[0]
	policy := expected.Policy
	if receipt.Validate() != nil || receipt.Outcome != Passed || receipt.ArtifactID != artifact.ID || receipt.AttemptID != artifact.AttemptID || receipt.RunID != artifact.RunID || receipt.ActionID != artifact.ActionID || receipt.OwnerID != artifact.OwnerID ||
		receipt.AuthorityContextID != artifact.AuthorityContextID || receipt.AuthorityContextDigest != artifact.AuthorityContextDigest || receipt.VerifierID != policy.VerifierID || receipt.PolicyVersion != policy.PolicyVersion || receipt.Claim != policy.Claim || receipt.MediaType != policy.MediaType || receipt.ExpectedDigest != policy.ExpectedDigest || receipt.ObservedDigest != artifact.Digest {
		return nil, denied
	}
	receiptBytes, err := blobs.GetBlob(receipt.EvidenceRef)
	if err != nil {
		return nil, denied
	}
	reloaded, err := DecodeVerificationReceipt(receipt.EvidenceRef, receiptBytes)
	if err != nil || reloaded != receipt {
		return nil, denied
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := blobs.GetBlob(artifact.ContentRef)
	if err != nil || len(data) > SelectedFileMaxBytes || sha256Reference(data) != artifact.Digest {
		return nil, denied
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return append([]byte(nil), data...), nil
}
