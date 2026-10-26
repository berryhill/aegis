package evidence

import (
	"context"
	"github.com/berryhill/aegis/internal/store"
	"testing"
)

func TestContributionReceiptManifestJoin(t *testing.T) {
	for _, target := range []string{"local-only", "no-publish", "no-merge", "verified-protected-base-merge"} {
		for _, mutation := range []string{"valid", "wrong-target", "cross-binding", "missing", "corrupt", "omitted-obligation", "additional-obligation"} {
			t.Run(target+"/"+mutation, func(t *testing.T) {
				s, err := store.Open(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				v, err := NewBlobVerifier(s)
				if err != nil {
					t.Fatal(err)
				}
				m := contributionManifestFixture()
				m.Target = target
				if target != "verified-protected-base-merge" {
					m.IssueEvidence = ""
				}
				if target == "local-only" || target == "no-publish" {
					m.DeliveryEvidence = []string{}
				}
				expected := m.Binding
				boundary := target
				if mutation == "cross-binding" {
					m.Binding.Run = "foreign"
				}
				if mutation == "wrong-target" {
					if target == "local-only" {
						boundary = "no-publish"
					} else {
						boundary = "local-only"
					}
				}
				wire, err := MarshalContributionManifest(m)
				if err != nil {
					t.Fatal(err)
				}
				ref := sha256Reference(wire)
				if mutation == "corrupt" {
					wire = []byte(`{"invalid":true}`)
					ref = sha256Reference(wire)
				}
				if mutation != "missing" {
					if _, err = s.PutBlob(wire); err != nil {
						t.Fatal(err)
					}
				}
				required := append([]string{m.CandidateEvidence, m.AuthorityEvidence}, m.DeliveryEvidence...)
				if m.IssueEvidence != "" {
					required = append(required, m.IssueEvidence)
				}
				if mutation == "omitted-obligation" {
					required = required[:len(required)-1]
				}
				if mutation == "additional-obligation" {
					required = append(required, sha256Reference([]byte("not-in-immutable-manifest")))
				}
				r := ContributionReceipt{Schema: ContributionReceiptV1, Binding: expected, Manifest: ref, Boundary: boundary, Outcome: "narrower-target-completed", Reason: "narrower_target_verified", Evidence: required, ObservedAt: m.ObservedAt}
				if boundary == "verified-protected-base-merge" {
					r.Outcome = "succeeded"
					r.Reason = "contribution_verified"
					r.Merge = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
				}
				rw, err := MarshalContributionReceipt(r)
				if err != nil {
					t.Fatal(err)
				}
				rr, err := s.PutBlob(rw)
				if err != nil {
					t.Fatal(err)
				}
				_, err = v.ReloadContributionReceipt(context.Background(), rr, expected, ref, required)
				if (err == nil) != (mutation == "valid") {
					t.Fatalf("reload err=%v", err)
				}
			})
		}
	}
}
