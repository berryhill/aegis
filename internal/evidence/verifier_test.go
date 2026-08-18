package evidence

import (
	"context"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/store"
)

func TestBlobVerifierFreshReadProducesReplayBoundReceipt(t *testing.T) {
	records, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("runtime output")
	digest, err := records.PutBlob(content)
	if err != nil {
		t.Fatal(err)
	}
	artifact := RuntimeArtifact{ID: "artifact-1", AttemptID: "attempt-1", OwnerID: "owner-1", ActionID: "action-1", RunID: "run-1", AuthorityContextID: "authority-1", AuthorityContextDigest: "sha256:authority", Digest: digest, ContentRef: digest, MediaType: "text/plain", CreatedAt: time.Now().UTC()}
	verifier, err := NewBlobVerifier(records)
	if err != nil {
		t.Fatal(err)
	}
	policy := VerificationPolicy{VerifierID: ArtifactVerifierID, PolicyVersion: VerifierPolicyV1, Claim: "exact-output", MediaType: artifact.MediaType, ExpectedDigest: digest}
	receipt, err := verifier.Verify(context.Background(), artifact, policy)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Outcome != Passed || receipt.ArtifactID != artifact.ID || receipt.ActionID != artifact.ActionID || receipt.RunID != artifact.RunID || receipt.AuthorityContextDigest != artifact.AuthorityContextDigest {
		t.Fatalf("receipt lost replay-resistant bindings: %#v", receipt)
	}
	if receipt.EvidenceRef == "" || receipt.EvidenceRef == artifact.ContentRef {
		t.Fatalf("receipt was not persisted as distinct evidence: %#v", receipt)
	}
	reloaded, err := verifier.ReloadReceipt(context.Background(), receipt.EvidenceRef)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded != receipt {
		t.Fatalf("independently reloaded receipt changed: got=%#v want=%#v", reloaded, receipt)
	}
}

func TestBlobVerifierFailsClosedAgainstPrecommittedPolicyMatrix(t *testing.T) {
	records, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("runtime output")
	digest, err := records.PutBlob(content)
	if err != nil {
		t.Fatal(err)
	}
	artifact := RuntimeArtifact{ID: "artifact-policy", AttemptID: "attempt-policy", OwnerID: "owner-1", ActionID: "action-1", RunID: "run-1", AuthorityContextID: "authority-1", AuthorityContextDigest: "sha256:authority", Digest: digest, ContentRef: digest, MediaType: "text/plain", CreatedAt: time.Now().UTC()}
	base := VerificationPolicy{VerifierID: ArtifactVerifierID, PolicyVersion: VerifierPolicyV1, Claim: "exact-output", MediaType: artifact.MediaType, ExpectedDigest: digest}
	tests := []struct {
		name     string
		mutate   func(*VerificationPolicy)
		category string
	}{
		{"verifier", func(policy *VerificationPolicy) { policy.VerifierID = "other-verifier" }, "verifier_policy_mismatch"},
		{"policy version", func(policy *VerificationPolicy) { policy.PolicyVersion = "other-policy" }, "verifier_policy_mismatch"},
		{"media type", func(policy *VerificationPolicy) { policy.MediaType = "application/json" }, "media_type_mismatch"},
		{"expected bytes", func(policy *VerificationPolicy) { policy.ExpectedDigest = sha256Reference([]byte("different")) }, "expected_output_mismatch"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := base
			test.mutate(&policy)
			receipt, verifyErr := (&BlobVerifier{store: records, now: time.Now}).Verify(context.Background(), artifact, policy)
			if verifyErr != nil {
				t.Fatal(verifyErr)
			}
			if receipt.Outcome != Failed || receipt.FailureCategory != test.category || receipt.EvidenceRef == "" {
				t.Fatalf("policy mismatch was not durably failed: %#v", receipt)
			}
		})
	}
}

func TestDecodeVerificationReceiptRejectsNonCanonicalJSON(t *testing.T) {
	records, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("canonical output")
	digest, err := records.PutBlob(content)
	if err != nil {
		t.Fatal(err)
	}
	artifact := RuntimeArtifact{ID: "artifact-canonical", AttemptID: "attempt-canonical", OwnerID: "agent-1", ActionID: "action-1", RunID: "run-1", AuthorityContextID: "authority-1", AuthorityContextDigest: "sha256:authority", Digest: digest, ContentRef: digest, MediaType: "text/plain", CreatedAt: time.Now().UTC()}
	verifier, err := NewBlobVerifier(records)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := verifier.Verify(context.Background(), artifact, VerificationPolicy{VerifierID: ArtifactVerifierID, PolicyVersion: VerifierPolicyV1, Claim: "exact-output", MediaType: artifact.MediaType, ExpectedDigest: digest})
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := records.GetBlob(receipt.EvidenceRef)
	if err != nil {
		t.Fatal(err)
	}
	withoutClose := canonical[:len(canonical)-1]
	tests := map[string][]byte{
		"unknown field":           append(append([]byte{}, withoutClose...), []byte(`,"unknown":true}`)...),
		"duplicate field":         append(append([]byte{}, withoutClose...), []byte(`,"id":"duplicate"}`)...),
		"trailing value":          append(append([]byte{}, canonical...), []byte(` {}`)...),
		"noncanonical whitespace": append([]byte(" "), canonical...),
	}
	for name, wire := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeVerificationReceipt(sha256Reference(wire), wire); err == nil {
				t.Fatal("non-canonical receipt was accepted")
			}
		})
	}
}

func TestBlobVerifierRecordsFailureWithoutPromotingArtifact(t *testing.T) {
	records, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	digest, err := records.PutBlob([]byte("observed"))
	if err != nil {
		t.Fatal(err)
	}
	artifact := RuntimeArtifact{ID: "artifact-1", AttemptID: "attempt-1", OwnerID: "owner-1", ActionID: "action-1", RunID: "run-1", AuthorityContextID: "authority-1", AuthorityContextDigest: "sha256:authority", Digest: digest, ContentRef: digest, MediaType: "text/plain"}
	verifier, err := NewBlobVerifier(records)
	if err != nil {
		t.Fatal(err)
	}
	policy := VerificationPolicy{VerifierID: ArtifactVerifierID, PolicyVersion: VerifierPolicyV1, Claim: "exact-output", MediaType: artifact.MediaType, ExpectedDigest: sha256Reference([]byte("expected"))}
	receipt, err := verifier.Verify(context.Background(), artifact, policy)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Outcome != Failed || receipt.FailureCategory != "expected_output_mismatch" || receipt.EvidenceRef == "" {
		t.Fatalf("verification failure was not durably classified: %#v", receipt)
	}
	if artifact.Digest != digest {
		t.Fatal("verifier mutated runtime artifact into a success claim")
	}
}
