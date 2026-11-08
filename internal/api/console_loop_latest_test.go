package api

import (
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/loop"
)

func TestConsoleLoopLatestUsesAuthoritativeInventoryNotViewedRevision(t *testing.T) {
	views := []app.LoopView{
		{Revision: loop.LoopRevision{LoopID: "history-loop", Revision: 1, Digest: "first-digest"}},
		{Revision: loop.LoopRevision{LoopID: "history-loop", Revision: 2, Digest: "second-digest"}},
	}
	model, err := consoleSurfaceModel(app.FleetSurface{
		Loops:     views,
		Readiness: map[string]app.SurfaceReadiness{"loops": {State: "ready", Authoritative: true}},
	}, consoleLoops)
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Records) != 1 || len(model.ExactLoopRecords) != 2 {
		t.Fatalf("inventory lost exact revisions: %d", len(model.Records))
	}
	for _, record := range model.ExactLoopRecords {
		want := "r2 · viewing historical revision"
		if record.Revision == "r2" {
			want = "r2 · viewing latest"
		}
		if record.Loop.LatestVersion != want {
			t.Fatalf("view %s latest = %q; want %q", record.Revision, record.Loop.LatestVersion, want)
		}
		if record.AvailabilityHeadRevision != 2 || record.AvailabilityHeadDigest != "second-digest" {
			t.Fatal("latest enrichment changed the exact availability head")
		}
	}
}
