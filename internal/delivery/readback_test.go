package delivery

import (
	"context"
	"strings"
	"testing"
)

func TestReadbackHTTPAndStrictBinding(t *testing.T) {
	for _, failed := range []bool{false, true} {
		g, f, p := newGithubFixture(t)
		if failed {
			f.check = "failure"
		}
		i, _ := intentFor(p, CheckDelivery)
		o := g.Apply(context.Background(), i, p, func(context.Context) error { return nil })
		proof, e := ParseReadback(i, o)
		if e != nil || len(proof.Facts) == 0 {
			t.Fatalf("proof missing: %v", e)
		}
		if failed && o.Outcome != Failed {
			t.Fatal("failure lost")
		}
		for _, fact := range proof.Facts {
			if fact.Kind == "check_run" && fact.Conclusion != f.check {
				t.Fatal("upstream conclusion lost")
			}
		}
		wrong := i
		wrong.Policy += "different"
		if _, e = ParseReadback(wrong, o); e == nil {
			t.Fatal("wrong policy accepted")
		}
		for _, wire := range []string{o.ProofJSON + " ", strings.Replace(o.ProofJSON, `"Schema":`, `"unknown":0,"Schema":`, 1), strings.Replace(o.ProofJSON, `"Schema":`, `"Schema":"duplicate","Schema":`, 1)} {
			bad := o
			bad.ProofJSON = wire
			if _, e = ParseReadback(i, bad); e == nil {
				t.Fatal("noncanonical accepted")
			}
		}
		bad := o
		bad.Code = "tamper"
		if _, e = ParseReadback(i, bad); e == nil {
			t.Fatal("result drift accepted")
		}
	}
}
func TestReadbackSanitizesAndBounds(t *testing.T) {
	c := &readbackCollector{}
	ctx := context.WithValue(context.Background(), readbackKey{}, c)
	captureReadback(ctx, "/repos/o/r/pulls/1", 200, []byte(`{"html_url":"https://github.com/o/r/pull/1","number":1,"body":"PRIVATE_CANARY","title":"PRIVATE_CANARY","user":{"id":5,"login":"actor","token":"PRIVATE_CANARY"}}`))
	captureReadback(ctx, "/repos/o/r/pulls/1", 403, []byte(`{"message":"PRIVATE_CANARY"}`))
	o := sealReadback(Intent{}, Observation{Outcome: Denied}, c)
	if strings.Contains(o.ProofJSON, "PRIVATE_CANARY") {
		t.Fatal("private body retained")
	}
	if _, e := ParseReadback(Intent{}, o); e != nil {
		t.Fatal(e)
	}
	for n := 0; n < 401; n++ {
		c.add(ReadbackFact{})
	}
	if sealReadback(Intent{}, Observation{}, c).ProofJSON != "" {
		t.Fatal("overflow accepted")
	}
}
