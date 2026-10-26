package evidence

import (
	"context"
	"github.com/berryhill/aegis/internal/store"
	"strings"
	"testing"
)

func TestIndependentCrossBoundaryReceiptRejected(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	v, err := NewBlobVerifier(s)
	if err != nil {
		t.Fatal(err)
	}
	m := contributionManifestFixture()
	m.Target = "local-only"
	m.DeliveryEvidence = []string{}
	m.IssueEvidence = ""
	w, err := MarshalContributionManifest(m)
	if err != nil {
		t.Fatal(err)
	}
	mr, err := s.PutBlob(w)
	if err != nil {
		t.Fatal(err)
	}
	r := ContributionReceipt{Schema: ContributionReceiptV1, Binding: m.Binding, Manifest: mr, Boundary: "verified-protected-base-merge", Outcome: "succeeded", Reason: "contribution_verified", Merge: strings.Repeat("b", 40), Evidence: []string{m.CandidateEvidence, m.AuthorityEvidence}, ObservedAt: m.ObservedAt}
	rw, err := MarshalContributionReceipt(r)
	if err != nil {
		t.Fatal(err)
	}
	rr, err := s.PutBlob(rw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.ReloadContributionManifest(context.Background(), mr, m.Binding, "local-only"); err != nil {
		t.Fatal(err)
	}
	if got, err := v.ReloadContributionReceipt(context.Background(), rr, m.Binding, mr, r.Evidence); err == nil {
		t.Fatalf("local-only manifest reloaded as boundary=%s outcome=%s", got.Boundary, got.Outcome)
	}
}
