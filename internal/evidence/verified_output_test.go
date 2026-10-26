package evidence

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/store"
)

func verifiedOutputFixture(t *testing.T, text string) (*store.Store, RuntimeArtifact, []VerificationReceipt, VerifiedOutputBinding) {
	t.Helper()
	records, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	digest, err := records.PutBlob([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	a := RuntimeArtifact{ID: "artifact-producer", AttemptID: "attempt-producer", OwnerID: "agent", ActionID: "produce", RunID: "execution-producer", AuthorityContextID: "authority", AuthorityContextDigest: digest, Digest: digest, ContentRef: digest, MediaType: "application/octet-stream", CreatedAt: time.Now().UTC()}
	p := VerificationPolicy{VerifierID: ArtifactVerifierID, PolicyVersion: VerifierPolicyV1, Claim: "exact-output", MediaType: a.MediaType, ExpectedDigest: digest}
	v, err := NewBlobVerifier(records)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := v.Verify(context.Background(), a, p)
	if err != nil {
		t.Fatal(err)
	}
	binding := VerifiedOutputBinding{ArtifactID: a.ID, AttemptID: a.AttemptID, RunID: a.RunID, ActionID: a.ActionID, OwnerID: a.OwnerID, AuthorityContextID: a.AuthorityContextID, AuthorityContextDigest: a.AuthorityContextDigest, Policy: p}
	return records, a, []VerificationReceipt{receipt}, binding
}

func TestReadVerifiedOutputReturnsActualContentAndRejectsWrongLineage(t *testing.T) {
	for _, text := range []string{"hello", "github:berryhill/aegis#317"} {
		t.Run(text, func(t *testing.T) {
			records, a, receipts, b := verifiedOutputFixture(t, text)
			data, err := ReadVerifiedOutput(context.Background(), records, a, receipts, b)
			if err != nil || !bytes.Equal(data, []byte(text)) {
				t.Fatalf("data=%q err=%v", data, err)
			}
			for name, mutate := range map[string]func(*VerifiedOutputBinding){
				"artifact":         func(b *VerifiedOutputBinding) { b.ArtifactID = "other" },
				"attempt":          func(b *VerifiedOutputBinding) { b.AttemptID = "other" },
				"execution":        func(b *VerifiedOutputBinding) { b.RunID = "other" },
				"action":           func(b *VerifiedOutputBinding) { b.ActionID = "other" },
				"owner":            func(b *VerifiedOutputBinding) { b.OwnerID = "other" },
				"authority":        func(b *VerifiedOutputBinding) { b.AuthorityContextID = "other" },
				"authority digest": func(b *VerifiedOutputBinding) { b.AuthorityContextDigest = "other" },
			} {
				t.Run(name, func(t *testing.T) {
					wrong := b
					mutate(&wrong)
					if _, err := ReadVerifiedOutput(context.Background(), records, a, receipts, wrong); err == nil {
						t.Fatal("same bytes wrong lineage accepted")
					}
				})
			}
		})
	}
}

type alteredOutputReader struct {
	original interface{ GetBlob(string) ([]byte, error) }
	target   string
	data     []byte
	fail     bool
}

func (r alteredOutputReader) GetBlob(ref string) ([]byte, error) {
	if ref == r.target {
		if r.fail {
			return nil, errors.New("unavailable")
		}
		return r.data, nil
	}
	return r.original.GetBlob(ref)
}

func TestReadVerifiedOutputFailsClosedOnEvidenceAndContentFaults(t *testing.T) {
	records, a, receipts, b := verifiedOutputFixture(t, "hello")
	for name, mutate := range map[string]func(*RuntimeArtifact, *[]VerificationReceipt, *VerifiedOutputBinding){
		"no receipt":               func(_ *RuntimeArtifact, r *[]VerificationReceipt, _ *VerifiedOutputBinding) { *r = nil },
		"duplicate receipt":        func(_ *RuntimeArtifact, r *[]VerificationReceipt, _ *VerifiedOutputBinding) { *r = append(*r, (*r)[0]) },
		"projected receipt tamper": func(_ *RuntimeArtifact, r *[]VerificationReceipt, _ *VerifiedOutputBinding) { (*r)[0].RunID = "other" },
		"failed receipt": func(_ *RuntimeArtifact, r *[]VerificationReceipt, _ *VerifiedOutputBinding) {
			(*r)[0].Outcome = Failed
			(*r)[0].FailureCategory = "wrong-output"
		},
		"wrong policy": func(_ *RuntimeArtifact, _ *[]VerificationReceipt, b *VerifiedOutputBinding) { b.Policy.Claim = "other" },
		"path": func(a *RuntimeArtifact, _ *[]VerificationReceipt, _ *VerifiedOutputBinding) {
			a.ContentRef = "producer/result.txt"
		},
		"mutable reference": func(a *RuntimeArtifact, _ *[]VerificationReceipt, _ *VerifiedOutputBinding) { a.ContentRef = "latest" },
	} {
		t.Run(name, func(t *testing.T) {
			art, rs, binding := a, append([]VerificationReceipt(nil), receipts...), b
			mutate(&art, &rs, &binding)
			if _, err := ReadVerifiedOutput(context.Background(), records, art, rs, binding); err == nil {
				t.Fatal("invalid evidence accepted")
			}
		})
	}
	for _, target := range []string{a.ContentRef, receipts[0].EvidenceRef} {
		for _, fail := range []bool{false, true} {
			if _, err := ReadVerifiedOutput(context.Background(), alteredOutputReader{records, target, []byte("corrupt"), fail}, a, receipts, b); err == nil {
				t.Fatal("corrupt or missing blob accepted")
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadVerifiedOutput(ctx, records, a, receipts, b); err == nil {
		t.Fatal("cancelled read accepted")
	}
}
