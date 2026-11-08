package api

import (
	"net/url"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/loop"
)

func TestConsoleLogicalLoopsDiscoveryAndExactHistory(t *testing.T) {
	for _, availability := range []loop.Availability{loop.AvailabilityPublished, loop.AvailabilityArchived} {
		t.Run(string(availability), func(t *testing.T) {
			first := app.LoopView{Revision: loop.LoopRevision{LoopID: "stable/id:loop", Name: "Same human title", Revision: 1, Digest: "first"}}
			first.Availability.State = availability
			first.Lifecycle = loop.Lifecycle{State: loop.LifecycleActive, ActiveRevision: 1, ActiveDigest: "first"}
			second := first
			second.Revision.Revision, second.Revision.Digest = 2, "second"
			other := first
			other.Revision.LoopID, other.Revision.Digest = "different-loop", "other"
			model, err := consoleSurfaceModel(app.FleetSurface{Loops: []app.LoopView{second, other, first}, Readiness: map[string]app.SurfaceReadiness{"loops": {Authoritative: true, Count: 3, State: "ready"}}}, consoleLoops)
			if err != nil {
				t.Fatal(err)
			}
			if len(model.Records) != 2 || model.TotalCount != 2 || !strings.Contains(model.Status, "2 authoritative logical Loops") {
				t.Fatalf("revision count leaked: %+v", model)
			}
			head := model.Records[0]
			if head.Key != "stable/id:loop:2" || head.Lifecycle != "active" || head.Revision != "r2" || !strings.Contains(head.Loop.ActiveVersion, "r1 @ first") || len(head.Loop.RevisionHistory) != 2 {
				t.Fatalf("head/active/history: %+v %+v", head, head.Loop)
			}
			if err := selectConsoleRecord(&model, "stable/id:loop:1"); err != nil {
				t.Fatal(err)
			}
			if model.Inspector.Revision != "r1" || model.Inspector.Digest != "first" || len(model.Inspector.Loop.RevisionHistory) != 2 {
				t.Fatal("exact history lost")
			}
			if err := filterConsoleLoopAvailability(&model, string(availability), "stable/id:loop:1"); err != nil {
				t.Fatal(err)
			}
			if err := filterConsoleDefinitions(&model, "Same human title", "active"); err != nil {
				t.Fatal(err)
			}
			if err := paginateConsoleCollection(&model, "", "stable/id:loop:1", 1, url.Values{}); err != nil {
				t.Fatal(err)
			}
			if model.TotalRecords != 2 || len(model.Records) != 1 || model.Inspector.Digest != "first" {
				t.Fatal("pagination grouped wrong unit or lost historical detail")
			}
			if err := selectConsoleRecord(&model, "missing:1"); err == nil {
				t.Fatal("unknown exact revision admitted")
			}
		})
	}
}
