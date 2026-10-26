package delivery

import (
	"context"
	"testing"
)

func TestClosedAdapterRejectsDifferentLocalTarget(t *testing.T) {
	g, s := commitFixture(t)
	p := fixturePlan()
	p.Head = s.Commit
	p.BaseHead = s.Parent
	p.Commit = s
	p.Intake.Worktree = g.Directory
	i, _ := intentFor(p, Commit)
	p.Commit.Branch = "other/branch"
	o := (ClosedAdapter{Git: g}).Apply(context.Background(), i, p, func(context.Context) error { return nil })
	if o.Outcome != Denied {
		t.Fatal(o)
	}
}
