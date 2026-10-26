package delivery

import (
	"context"
	"strings"
	"testing"
)

func TestIndependentV6LostMergePostFailurePreservesReadback(t *testing.T) {
	g, f, p := newGithubFixture(t)
	f.lost = true
	f.postFail = true
	m := &memoryOwner{}
	engine := NewEngine(m, fixtureSource{p}, &fixtureAuthority{}, g)
	ctx := context.Background()
	first, err := engine.Execute(ctx, p.Reference, Merge)
	f.Lock()
	writes, merged := f.writes, f.merged
	f.Unlock()
	if err != nil || first.Outcome != Unknown || writes != 1 || !merged {
		t.Fatalf("positive effect did not start: %+v %v writes=%d merged=%v", first, err, writes, merged)
	}
	i, err := intentFor(p, Merge)
	if err != nil {
		t.Fatal(err)
	}
	observed := g.Reconcile(ctx, i, p, func(context.Context) error { return nil })
	if observed.Outcome != Unknown || observed.Merge != strings.Repeat("e", 40) {
		t.Fatalf("partial readback not exercised: %+v", observed)
	}
	replay, err := engine.Execute(ctx, p.Reference, Merge)
	m.Lock()
	stored := m.records[i.ID]
	m.Unlock()
	f.Lock()
	writes = f.writes
	f.Unlock()
	t.Logf("readback=%+v returned=%+v stored=%+v writes=%d", observed, replay.Observation, stored.Observation, writes)
	if err != nil || replay.Merge != observed.Merge || stored.Merge != observed.Merge || writes != 1 {
		t.Fatalf("irreversible merge readback discarded: err=%v returned.Merge=%q stored.Merge=%q expected=%q writes=%d", err, replay.Merge, stored.Merge, observed.Merge, writes)
	}
}
