package badger

import (
	"context"
	"reflect"
	"testing"

	"github.com/berryhill/aegis/internal/contribution"
	badgerdb "github.com/dgraph-io/badger/v4"
)

func TestContributionContinuityReplayCacheIsolation(t *testing.T) {
	s, c := continuityStore(t)
	ctx := context.Background()
	prior, err := s.AppendContributionContinuity(ctx, continuityEvent(t, c, contribution.Continuity{}, "initialize"))
	if err != nil {
		t.Fatal(err)
	}
	event := continuityEvent(t, c, prior, "reserve")
	event.OperationID = "turn-1"
	event, err = contribution.SealContinuityEvent(event)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AppendContributionContinuity(ctx, event); err != nil {
		t.Fatal(err)
	}
	state, events, err := s.GetContributionContinuity(ctx, c.QueueItemID)
	if err != nil {
		t.Fatal(err)
	}
	// Returned mutable data must not poison a later authority reconstruction.
	state.Reservations["turn-1"] = "candidate"
	state.Decisions["forged"] = "approved"
	events[0].OwnerID = "forged"
	warm, history, err := s.GetContributionContinuity(ctx, c.QueueItemID)
	if err != nil {
		t.Fatal(err)
	}
	if warm.Reservations["turn-1"] != "runtime-turn" || len(warm.Decisions) != 0 || history[0].OwnerID != c.OwnerID {
		t.Fatal("returned data aliases replay cache")
	}
	// Recovery and a fresh Store still use full canonical decoding/reduction.
	err = s.view(ctx, func(tx *badgerdb.Txn) error {
		cold, coldHistory, e := loadContributionContinuity(tx, c.QueueItemID)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(cold, warm) || !reflect.DeepEqual(coldHistory, history) {
			t.Fatal("cached replay differs from complete replay")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestContributionContinuityReplayCopiesNestedPointers(t *testing.T) {
	original := contribution.Continuity{Continuation: &contribution.Continuation{ClaimID: "claim"}, NativeBoundaries: map[string]contribution.NativeBoundary{"builder": {Bindings: []contribution.NativeBinding{{IntentID: "intent"}}}}}
	events := []contribution.ContinuityEvent{{Continuation: original.Continuation, NativeBoundary: &contribution.NativeBoundary{Bindings: []contribution.NativeBinding{{IntentID: "intent"}}}, NativeEffect: &contribution.NativeEffect{State: "completed"}, RoleStop: &contribution.RoleStop{PID: 1}, RoleAuthority: &contribution.RoleAuthority{PID: 1}}}
	copy := copyReplayState(original)
	history := copyReplayEvents(events)
	if !reflect.DeepEqual(original, copy) || !reflect.DeepEqual(events, history) {
		t.Fatal("copy drift")
	}
	copy.Continuation.ClaimID = "forged"
	copy.NativeBoundaries["builder"].Bindings[0].IntentID = "forged"
	history[0].Continuation.ClaimID = "forged"
	history[0].NativeBoundary.Bindings[0].IntentID = "forged"
	history[0].NativeEffect.State = "forged"
	history[0].RoleStop.PID = 2
	history[0].RoleAuthority.PID = 2
	if original.Continuation.ClaimID != "claim" || original.NativeBoundaries["builder"].Bindings[0].IntentID != "intent" || events[0].Continuation.ClaimID != "claim" || events[0].NativeBoundary.Bindings[0].IntentID != "intent" || events[0].NativeEffect.State != "completed" || events[0].RoleStop.PID != 1 || events[0].RoleAuthority.PID != 1 {
		t.Fatal("nested replay data aliases caller")
	}
}
