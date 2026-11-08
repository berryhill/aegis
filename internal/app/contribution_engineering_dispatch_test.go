package app

import (
	"context"
	"fmt"
	"testing"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
)

func TestEngineeringDispatchRetainsFailureAcrossRecovery(t *testing.T) {
	f := newEngineeringAppFixture(t, []string{`{"unknown":true}`, engineeringInspect, engineeringScope})
	ctx := context.Background()
	key := "immutable-dispatch"
	if _, err := f.s.admitContributionAs(ctx, f.subject, "session", f.ref, f.reader.source.URL.URL, key, "local-only", f.reader); err == nil {
		t.Fatal("bad preparation admitted")
	}
	first, err := f.s.ReadContributionEngineeringAs(ctx, f.subject, key)
	if err != nil {
		t.Fatal(err)
	}
	var failed execution.LaunchContract
	if err = f.s.Store.Load("contribution-engineering-dispatches", first.IntentID, &failed); err != nil {
		t.Fatal(err)
	}
	if failed.ParentDispatch.State != execution.StateFailed || first.DispatchSequence != 3 {
		t.Fatal("actual failed lifecycle missing")
	}
	oldDigest := core.Digest(failed)
	if _, err = f.s.admitContributionAs(ctx, f.subject, "fresh-session", f.ref, f.reader.source.URL.URL, key, "local-only", f.reader); err != nil {
		t.Fatal(err)
	}
	after, err := f.s.ReadContributionEngineeringAs(ctx, f.subject, key)
	if err != nil {
		t.Fatal(err)
	}
	var retained execution.LaunchContract
	if err = f.s.Store.Load("contribution-engineering-dispatches", first.IntentID, &retained); err != nil || core.Digest(retained) != oldDigest {
		t.Fatal("first terminal record replaced", err)
	}
	if after.DispatchSequence != 6 || after.ReservedTurns != 3 || after.ReturnedTurns != 3 || after.RunID != first.RunID {
		t.Fatal("lifecycle/counters changed")
	}
	var final engineeringDispatchPhase
	if err = f.s.Store.Load("contribution-engineering-dispatch-phases", fmt.Sprintf("%s-%06d", first.IntentID, after.DispatchSequence), &final); err != nil || final.Operation.ParentDispatch.State != execution.StateSucceeded || final.Operation.ParentDispatch.ID == failed.ParentDispatch.ID {
		t.Fatal("recovery success not separate", err)
	}
	// Fixture-only tamper must invalidate authenticated complete-history readback,
	// even when a newer successful phase is intact.
	var old engineeringDispatchPhase
	target := fmt.Sprintf("%s-%06d", first.IntentID, first.DispatchSequence)
	if err = f.s.Store.Load("contribution-engineering-dispatch-phases", target, &old); err != nil {
		t.Fatal(err)
	}
	old.Operation.ParentDispatch.State = execution.StateSucceeded
	if err = f.s.Store.Save("contribution-engineering-dispatch-phases", target, old); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ReadContributionEngineeringAs(ctx, f.subject, key); err == nil {
		t.Fatal("old phase tamper hidden by successful retry")
	}
}
