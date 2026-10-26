package delivery

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestV15ReadbackSemanticTampering(t *testing.T) {
	g, _, p := newGithubFixture(t)
	i, _ := intentFor(p, CheckDelivery)
	o := g.Apply(context.Background(), i, p, func(context.Context) error { return nil })
	proof, e := ParseReadback(i, o)
	if e != nil || o.Outcome != Confirmed {
		t.Fatalf("baseline %v %s", e, o.Code)
	}
	for _, kind := range []string{"head", "protection", "check", "review", "source", "missing", "requirements"} {
		t.Run(kind, func(t *testing.T) {
			var v ReadbackProof
			b, _ := json.Marshal(proof)
			_ = json.Unmarshal(b, &v)
			if kind == "requirements" {
				v.Requirements.Checks = []string{"other"}
			} else {
				for n := range v.Facts {
					f := &v.Facts[n]
					switch {
					case kind == "head" && f.Kind == "pull":
						f.Head.SHA = strings.Repeat("f", 40)
					case kind == "protection" && f.Kind == "protection":
						f.Protection.Checks.Strict = false
					case kind == "check" && f.Kind == "check_run":
						f.Conclusion = "skipped"
					case kind == "review" && f.Kind == "review":
						f.State = "CHANGES_REQUESTED"
					case kind == "source" && f.Kind == "check_run":
						f.App = nil
					case kind == "missing" && f.Kind == "actor":
						f.Kind = "repository"
					}
				}
			}
			b, _ = json.Marshal(v)
			bad := o
			bad.ProofJSON = string(b)
			bad.ProofDigest = digest(v)
			if _, e := ParseReadback(i, bad); e == nil {
				t.Fatal("tampered semantic proof accepted")
			}
		})
	}
}
func TestV15RetainedProofKeepsCurrentBase(t *testing.T) {
	g, f, p := newGithubFixture(t)
	f.merged = true
	f.postFail = true
	i, _ := intentFor(p, Merge)
	guard := func(context.Context) error { return nil }
	a := g.Reconcile(context.Background(), i, p, guard)
	old := Record{Intent: i, Outcome: Unknown, Observation: a, Revision: 2}
	if a.BaseHead == "" {
		t.Fatal("missing first base")
	}
	f.base = strings.Repeat("f", 40)
	f.postFail = false
	b := g.Reconcile(context.Background(), i, p, guard)
	if b.Outcome != Confirmed {
		t.Fatal(b.Code)
	}
	joined, e := retainObservation(old, b)
	if e != nil {
		t.Fatal(e)
	}
	proof, e := ParseReadback(i, joined)
	if e != nil || proof.ObservedResult.BaseHead != f.base || proof.Result.BaseHead != a.BaseHead || proof.RetainedFrom == nil {
		t.Fatalf("join %v", e)
	}
	if ValidateRetainedReadback(old, joined) != nil {
		t.Fatal("valid predecessor denied")
	}
	wrong := old
	wrong.Revision++
	if ValidateRetainedReadback(wrong, joined) == nil {
		t.Fatal("forged predecessor accepted")
	}
}
func TestV15StructuredSuccessCanary(t *testing.T) {
	for _, body := range []string{`{"id":42,"login":"PRIVATE_CANARY"}`, `{"id":42,"login":"github_pat_canary"}`} {
		c := &readbackCollector{}
		ctx := context.WithValue(context.Background(), readbackKey{}, c)
		captureReadback(ctx, "/user", 200, []byte(body))
		o := sealReadback(Intent{}, Observation{Outcome: Confirmed, Merge: strings.Repeat("e", 40)}, c)
		if strings.Contains(o.ProofJSON, "CANARY") || o.ProofJSON != "" || o.Outcome != Unknown || o.Merge == "" {
			t.Fatal("unsafe success capture")
		}
	}
}
