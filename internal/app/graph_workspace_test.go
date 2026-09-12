package app

import (
	"reflect"
	"testing"

	"github.com/berryhill/aegis/internal/graph"
)

func TestGraphWorkspaceFactsExactLifecycleAndCurrentValidation(t *testing.T) {
	view := GraphView{Revision: graph.GraphRevision{GraphID: "workspace", Revision: 2, Digest: "exact"}}
	view.Lifecycle = graph.Lifecycle{State: graph.LifecycleActive, ActiveRevision: 2, ActiveDigest: "exact"}
	facts := view.WorkspaceFacts()
	if !facts.ActiveRevision {
		t.Fatal("exact active revision not recognized")
	}
	want := graph.ValidateRevision(view.Revision)
	if !reflect.DeepEqual(facts.Validation, want) || facts.StructurallyValid != (want.Outcome == graph.ValidationValid) {
		t.Fatal("current structural validation must be derived by graph domain")
	}
	if facts.StructurallyValid {
		t.Fatal("malformed revision must not be valid just because lifecycle is active")
	}
	for _, change := range []func(*GraphView){
		func(v *GraphView) { v.Lifecycle.ActiveRevision++ },
		func(v *GraphView) { v.Lifecycle.ActiveDigest = "other" },
		func(v *GraphView) { v.Lifecycle.State = "" },
	} {
		other := view
		change(&other)
		if other.WorkspaceFacts().ActiveRevision {
			t.Fatal("mismatched lifecycle must not project active")
		}
	}
}
