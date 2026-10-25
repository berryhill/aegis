package evidence

import (
	"context"
	"testing"

	"github.com/berryhill/aegis/internal/store"
)

func TestContributionIndependentReloadRejectsSubstitution(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	v, err := NewBlobVerifier(s)
	if err != nil {
		t.Fatal(err)
	}
	m := contributionManifestFixture()
	wire, _ := MarshalContributionManifest(m)
	ref, err := s.PutBlob(wire)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := v.ReloadContributionManifest(ctx, ref, m.Binding, m.Target); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"wrong-run", "wrong-contract", "wrong-attempt", "wrong-claim", "wrong-candidate", "wrong-loop", "wrong-control", "wrong-invocation", "wrong-profile", "wrong-policy", "narrower", "missing", "expired-context"} {
		t.Run(name, func(t *testing.T) {
			b, target, r, c := m.Binding, m.Target, ref, ctx
			switch name {
			case "wrong-run":
				b.Run = "foreign"
			case "wrong-contract":
				b.Contract.ID = "foreign"
			case "wrong-attempt":
				b.Attempt = "foreign"
			case "wrong-claim":
				b.Claim = "foreign"
			case "wrong-candidate":
				b.Candidate = sha256Reference([]byte("foreign"))
			case "wrong-loop":
				b.Loop.Revision++
			case "wrong-control":
				b.Control.Digest = sha256Reference([]byte("foreign"))
			case "wrong-invocation":
				b.Invocation.ID = "foreign"
			case "wrong-profile":
				b.Profile = sha256Reference([]byte("foreign"))
			case "wrong-policy":
				b.Policy = "wrong"
			case "narrower":
				target = "no-merge"
			case "missing":
				r = sha256Reference([]byte("absent"))
			case "expired-context":
				var cancel context.CancelFunc
				c, cancel = context.WithCancel(ctx)
				cancel()
			}
			if _, err := v.ReloadContributionManifest(c, r, b, target); err == nil {
				t.Fatal("foreign/stale evidence accepted")
			}
		})
	}
	r := ContributionReceipt{Schema: ContributionReceiptV1, Binding: m.Binding, Manifest: ref, Boundary: m.Target, Outcome: "merged-unresolved", Reason: "merged_validation_unresolved", Merge: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Evidence: []string{m.CandidateEvidence, m.AuthorityEvidence}, ObservedAt: m.ObservedAt}
	w, _ := MarshalContributionReceipt(r)
	rr, _ := s.PutBlob(w)
	if _, err := v.ReloadContributionReceipt(ctx, rr, m.Binding, ref, r.Evidence); err != nil {
		t.Fatal(err)
	}
	for _, required := range [][]string{nil, {}, r.Evidence[:1], {r.Evidence[0], r.Evidence[0]}, {r.Evidence[0], sha256Reference([]byte("substitute"))}, append(append([]string{}, r.Evidence...), sha256Reference([]byte("extra")))} {
		if _, err := v.ReloadContributionReceipt(ctx, rr, m.Binding, ref, required); err == nil {
			t.Fatal("missing/duplicate/additional-substitution evidence accepted")
		}
	}
	// Model-authored PASS can be content-addressed, but is not this schema and
	// can never be reinterpreted as a retained contribution verification report.
	fake, _ := s.PutBlob([]byte(`{"outcome":"PASS"}`))
	if _, err := v.ReloadContributionReceipt(ctx, fake, m.Binding, ref, r.Evidence); err == nil {
		t.Fatal("model PASS promoted")
	}
}
