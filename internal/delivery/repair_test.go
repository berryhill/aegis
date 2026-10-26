package delivery

import (
	"context"
	"testing"
)

type fixtureRepair struct {
	calls   int
	failure RepairFailure
}

func (r *fixtureRepair) RequestRepair(_ context.Context, f RepairFailure) error {
	r.calls++
	r.failure = f
	return nil
}
func TestRepairForwardOnlyConcretePersistedFailure(t *testing.T) {
	p := fixturePlan()
	i, _ := intentFor(p, Merge)
	r := Record{Intent: i, Revision: 2, Outcome: Failed, Observation: Observation{Outcome: Failed, Code: "ci_failed"}}
	owner := &fixtureRepair{}
	if ForwardRepair(context.Background(), owner, r) != nil || owner.calls != 1 || owner.failure.Intent != i.ID {
		t.Fatal("failure not forwarded")
	}
	for _, outcome := range []Outcome{Unknown, Denied, Confirmed} {
		r.Outcome = outcome
		if ForwardRepair(context.Background(), owner, r) == nil {
			t.Fatal("nonfailure repair accepted")
		}
	}
}
func TestGitHubCIFailureAndConflictAreStructured(t *testing.T) {
	g, f, p := newGithubFixture(t)
	f.check = "failure"
	i, _ := intentFor(p, Merge)
	o := g.Apply(context.Background(), i, p, func(context.Context) error { return nil })
	if o.Outcome != Failed || o.Code != "ci_failed" {
		t.Fatalf("%+v", o)
	}
}
