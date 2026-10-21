package queue

import (
	"github.com/berryhill/aegis/internal/reference"
	"testing"
	"time"
)

func TestQueuePresentationSelection(t *testing.T) {
	s, v, err := ResolveSelection(Preferences{}, "", "")
	if err != nil || s != SelectionSucceeded || v != VisibilityUnarchived {
		t.Fatalf("defaults: %q %q %v", s, v, err)
	}
	saved := Preferences{Selection: SelectionSucceeded, Visibility: VisibilityUnarchived}
	s, v, err = ResolveSelection(saved, SelectionAll, VisibilityArchived)
	if err != nil || s != SelectionAll || v != VisibilityArchived || saved.Selection != SelectionSucceeded {
		t.Fatalf("override: %q %q %v", s, v, err)
	}
	for _, state := range []State{StatePreparationPending, StateAwaitingRuntime, StateQueued, StateClaimed, StateSucceeded, StateFailed, StateDenied, StateCancelled, StateExpired, StateRevoked} {
		if err := LifecycleSelection(state).Validate(); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err = ResolveSelection(saved, "invented", ""); err == nil {
		t.Fatal("unknown lifecycle accepted")
	}
	if _, _, err = ResolveSelection(saved, "", "invented"); err == nil {
		t.Fatal("unknown visibility accepted")
	}
}

func TestQueuePresentationCanonicalRoundTrip(t *testing.T) {
	a, err := NewArchive(Archive{PrincipalID: "principal", SubjectID: "operator", RequestID: "archive", QueueItem: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: "item", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, Archived: true, Revision: 1, UpdatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	wire, err := MarshalArchive(a)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := UnmarshalArchive(wire); err != nil || got != a {
		t.Fatalf("round trip: %+v %v", got, err)
	}
	a.Archived = false
	if _, err = MarshalArchive(a); err == nil {
		t.Fatal("tampered archive accepted")
	}
	p, err := NewPreferences(Preferences{PrincipalID: "principal", SubjectID: "operator", RequestID: "preferences", Selection: SelectionSucceeded, Visibility: VisibilityUnarchived, Revision: 1, UpdatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	wire, err = MarshalPreferences(p)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := UnmarshalPreferences(wire); err != nil || got != p {
		t.Fatalf("round trip: %+v %v", got, err)
	}
	p.Revision = 0
	if _, err = NewPreferences(p); err == nil {
		t.Fatal("zero revision accepted")
	}
}
