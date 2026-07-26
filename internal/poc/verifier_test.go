package poc

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/plumbing"
)

func TestBlobArtifactVerifierReadsStoredBytesAndPersistsReceipt(t *testing.T) {
	records := openTestStore(t)
	content := []byte("runtime output")
	reference, err := records.PutBlob(content)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewBlobArtifactVerifier(records)
	if err != nil {
		t.Fatal(err)
	}
	observedAt := time.Now().UTC()
	verifier.now = func() time.Time { return observedAt }
	artifact := plumbing.Artifact{ID: "artifact-1", OwnerID: "owner-1", Digest: digest(content), ContentRef: reference}

	evidence, err := verifier.Verify(context.Background(), artifact)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Outcome != plumbing.VerificationPassed || evidence.SubjectID != artifact.ID || evidence.Verifier != plumbing.VerifierArtifact || evidence.Provenance.Producer != plumbing.ProducerVerifier || evidence.ObservedAt != observedAt {
		t.Fatalf("evidence = %#v", evidence)
	}
	receiptBytes, err := records.GetBlob(evidence.EvidenceRef)
	if err != nil {
		t.Fatal(err)
	}
	var receipt verificationReceipt
	if err = json.Unmarshal(receiptBytes, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.ArtifactID != artifact.ID || receipt.ClaimedDigest != artifact.Digest || receipt.ObservedDigest != artifact.Digest || receipt.Outcome != plumbing.VerificationPassed || receipt.FailureCategory != "" {
		t.Fatalf("receipt = %#v", receipt)
	}
}

func TestBlobArtifactVerifierFailsClosedForUnresolvableOrMismatchedArtifact(t *testing.T) {
	records := openTestStore(t)
	storedRef, err := records.PutBlob([]byte("stored bytes"))
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewBlobArtifactVerifier(records)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		artifact plumbing.Artifact
		category string
	}{
		{
			name:     "missing content",
			artifact: plumbing.Artifact{ID: "artifact-missing", OwnerID: "owner-1", Digest: digest([]byte("missing")), ContentRef: "sha256:" + digest([]byte("missing"))},
			category: "artifact_unreadable_or_corrupt",
		},
		{
			name:     "claimed digest mismatch",
			artifact: plumbing.Artifact{ID: "artifact-mismatch", OwnerID: "owner-1", Digest: digest([]byte("different bytes")), ContentRef: storedRef},
			category: "artifact_digest_mismatch",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence, verifyErr := verifier.Verify(context.Background(), test.artifact)
			if verifyErr != nil {
				t.Fatal(verifyErr)
			}
			if evidence.Outcome != plumbing.VerificationFailed || evidence.Provenance.Producer != plumbing.ProducerVerifier {
				t.Fatalf("evidence = %#v", evidence)
			}
			receiptBytes, readErr := records.GetBlob(evidence.EvidenceRef)
			if readErr != nil {
				t.Fatal(readErr)
			}
			var receipt verificationReceipt
			if unmarshalErr := json.Unmarshal(receiptBytes, &receipt); unmarshalErr != nil {
				t.Fatal(unmarshalErr)
			}
			if receipt.Outcome != plumbing.VerificationFailed || receipt.FailureCategory != test.category {
				t.Fatalf("receipt = %#v", receipt)
			}
		})
	}
}

func TestBlobArtifactVerifierHonorsCancellationBeforeStorageRead(t *testing.T) {
	records := openTestStore(t)
	verifier, err := NewBlobArtifactVerifier(records)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = verifier.Verify(ctx, plumbing.Artifact{ID: "artifact-1"})
	if err != context.Canceled {
		t.Fatalf("error = %v, want context canceled", err)
	}
}
