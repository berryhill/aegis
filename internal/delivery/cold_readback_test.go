package delivery

import (
	"context"
	"testing"
)

func coldFixtureWrites(f *githubFixture) int { f.Lock(); defer f.Unlock(); return f.writes }

// Synthetic loopback transport/credential authority and memory CAS owner; not
// live-provider or canonical Badger evidence (covered in persistence tests).
func TestColdReadbackLostMergeAndCancelledRead(t *testing.T) {
	g, f, p := newGithubFixture(t)
	f.Lock()
	f.lost = true
	f.Unlock()
	i, err := intentFor(p, Merge)
	if err != nil {
		t.Fatal(err)
	}
	owner := &memoryOwner{}
	old, _, err := owner.Reserve(context.Background(), i)
	if err != nil {
		t.Fatal(err)
	}
	got := g.Apply(context.Background(), i, p, func(context.Context) error { return nil })
	if got.Outcome != Unknown || coldFixtureWrites(f) != 1 {
		t.Fatal(got, coldFixtureWrites(f))
	}
	owner.cancelled = true
	r, err := ReconcileHistorical(context.Background(), owner, g, old, p)
	if err != nil || r.Outcome != Confirmed || r.Merge == "" || coldFixtureWrites(f) != 1 {
		t.Fatal(r, err, coldFixtureWrites(f))
	}
	again, err := ReconcileHistorical(context.Background(), owner, g, r, p)
	if err != nil || again != r || coldFixtureWrites(f) != 1 {
		t.Fatal(again, err)
	}
}

func TestColdReadbackExactPayloadAndAbsencePending(t *testing.T) {
	g, f, p := newGithubFixture(t)
	i, _ := intentFor(p, Merge)
	owner := &memoryOwner{}
	old, _, _ := owner.Reserve(context.Background(), i)
	altered := p
	altered.MergeMethod = "rebase"
	if _, err := ReconcileHistorical(context.Background(), owner, g, old, altered); err == nil {
		t.Fatal("changed payload accepted")
	}
	r, err := ReconcileHistorical(context.Background(), owner, g, old, p)
	if r.Outcome != Unknown || coldFixtureWrites(f) != 0 {
		t.Fatal(r, err, coldFixtureWrites(f))
	}
	if owner.records[i.ID].Outcome != Unknown {
		t.Fatal("absence terminalized reservation")
	}
}

// Lost response followed by failed postchecks preserves the irreversible merge;
// a later read can finish validation without Source or another Merge API call.
func TestColdReadbackPostfailureThenLaterGreen(t *testing.T) {
	g, f, p := newGithubFixture(t)
	f.Lock()
	f.lost = true
	f.postFail = true
	f.Unlock()
	i, e := intentFor(p, Merge)
	if e != nil {
		t.Fatal(e)
	}
	owner := &memoryOwner{}
	old, _, e := owner.Reserve(context.Background(), i)
	if e != nil {
		t.Fatal(e)
	}
	obs := g.Apply(context.Background(), i, p, func(context.Context) error { return nil })
	if obs.Outcome != Unknown {
		t.Fatal(obs)
	}
	owner.cancelled = true
	first, e := ReconcileHistorical(context.Background(), owner, g, old, p)
	if e != nil || first.Outcome != Unknown || first.Merge == "" || first.Intent != i {
		t.Fatal(first, e)
	}
	f.Lock()
	f.postFail = false
	f.Unlock()
	final, e := ReconcileHistorical(context.Background(), owner, g, first, p)
	if e != nil || final.Outcome != Confirmed || final.Merge != first.Merge || final.Intent != i || coldFixtureWrites(f) != 1 {
		t.Fatal(final, e, coldFixtureWrites(f))
	}
}
