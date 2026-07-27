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
	artifact := RuntimeArtifact{ID: "artifact-1", OwnerID: "owner-1", ActionID: "action-1", RunID: "run-1", AuthorityContextID: "authority-1", AuthorityContextDigest: "sha256:authority", Digest: digest, ContentRef: digest, MediaType: "text/plain", CreatedAt: time.Now().UTC()}
	verifier, err := NewBlobVerifier(records)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := verifier.Verify(context.Background(), artifact, "exact-output", digest)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Outcome != Passed || receipt.ArtifactID != artifact.ID || receipt.ActionID != artifact.ActionID || receipt.RunID != artifact.RunID || receipt.AuthorityContextDigest != artifact.AuthorityContextDigest {
		t.Fatalf("receipt lost replay-resistant bindings: %#v", receipt)
	}
	if receipt.EvidenceRef == "" || receipt.EvidenceRef == artifact.ContentRef {
		t.Fatalf("receipt was not persisted as distinct evidence: %#v", receipt)
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
	artifact := RuntimeArtifact{ID: "artifact-1", OwnerID: "owner-1", ActionID: "action-1", RunID: "run-1", AuthorityContextID: "authority-1", AuthorityContextDigest: "sha256:authority", Digest: digest, ContentRef: digest}
	verifier, err := NewBlobVerifier(records)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := verifier.Verify(context.Background(), artifact, "exact-output", sha256Reference([]byte("expected")))
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
