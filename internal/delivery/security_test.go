package delivery

import (
	"context"
	"strings"
	"testing"
)

func TestLocalExactTargetsBothPaths(t *testing.T) {
	for _, kind := range []string{"branch", "parent", "tree", "workspace", "protected", "intent"} {
		t.Run(kind, func(t *testing.T) {
			g, s := commitFixture(t)
			p := fixturePlan()
			p.Head = s.Commit
			p.BaseHead = s.Parent
			p.Commit = s
			p.Intake.Worktree = g.Directory
			i, _ := intentFor(p, Commit)
			switch kind {
			case "branch":
				p.Commit.Branch = "other/branch"
			case "parent":
				p.Commit.Parent = strings.Repeat("f", 40)
			case "tree":
				p.Commit.Tree = strings.Repeat("f", 40)
			case "workspace":
				p.Intake.Worktree += "/other"
			case "protected":
				p.Base = p.Branch
			case "intent":
				i.Actor = "other"
			}
			a := ClosedAdapter{Git: g}
			guard := func(context.Context) error { return nil }
			for _, o := range []Observation{a.Apply(context.Background(), i, p, guard), a.Reconcile(context.Background(), i, p, guard)} {
				if o.Outcome != Denied {
					t.Fatal(o)
				}
			}
		})
	}
}

type inFlightAdapter struct{ entered, release chan struct{} }

func (a *inFlightAdapter) Apply(context.Context, Intent, Plan, Guard) Observation {
	close(a.entered)
	<-a.release
	return Observation{Outcome: Confirmed}
}
func (a *inFlightAdapter) Reconcile(context.Context, Intent, Plan, Guard) Observation {
	return Observation{Outcome: Denied}
}
func TestDuplicateNegativeCannotTerminalizeInFlight(t *testing.T) {
	p := fixturePlan()
	m := &memoryOwner{}
	a := &inFlightAdapter{make(chan struct{}), make(chan struct{})}
	e := NewEngine(m, fixtureSource{p}, &fixtureAuthority{}, a)
	done := make(chan Record, 1)
	go func() { r, _ := e.Execute(context.Background(), p.Reference, Publish); done <- r }()
	<-a.entered
	r, err := e.Execute(context.Background(), p.Reference, Publish)
	if err != nil || r.Outcome != Unknown || r.Revision != 1 {
		t.Fatalf("duplicate %+v %v", r, err)
	}
	close(a.release)
	r = <-done
	if r.Outcome != Confirmed {
		t.Fatal(r)
	}
}
func TestPublicationStableAcrossLaterPRProjection(t *testing.T) {
	p := fixturePlan()
	a, _ := intentFor(p, Publish)
	p.PR = 7
	p.PRBody = "later"
	p.PRTitle = "later"
	p.Completion = "later"
	b, _ := intentFor(p, Publish)
	if a != b {
		t.Fatal("publication rotated")
	}
}
func TestEmptyOrDuplicateReviewPolicyDenied(t *testing.T) {
	for _, required := range [][]string{nil, {"reviewer", "reviewer"}, {"missing"}} {
		g, f, p := newGithubFixture(t)
		p.RequiredReviewers = required
		i, _ := intentFor(p, Merge)
		o := g.Apply(context.Background(), i, p, func(context.Context) error { return nil })
		if o.Outcome != Denied || f.writes != 0 {
			t.Fatal(o)
		}
	}
}

type invalidRelationTransport struct{ corruptTransport }

// Controlled HTTP corruption qualification, not remote acceptance.
func TestMissingMergeParentsUnresolved(t *testing.T) {
	g, f, p := newGithubFixture(t)
	p.MergeMethod = "merge"
	f.merged = true
	f.base = strings.Repeat("e", 40)
	i, _ := intentFor(p, Merge)
	o := g.Reconcile(context.Background(), i, p, func(context.Context) error { return nil })
	if o.Outcome != Unknown || o.Merge == "" {
		t.Fatal(o)
	}
}
