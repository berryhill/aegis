package api

import (
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/loop"
	"testing"
)

func TestConsoleLoopHumanTitleDoesNotReplaceStableIdentity(t *testing.T) {
	revision := loop.LoopRevision{LoopID: "stable-loop", Name: "Human authored title", Revision: 2, Digest: "exact-digest"}
	record := consoleLoopRecord(app.LoopView{Revision: revision})
	if record.Label != "stable-loop" || record.Key != "stable-loop:2" || record.Loop.HumanTitle != revision.Name {
		t.Fatalf("identity/title conflated: %+v", record)
	}
	if record.Loop.LatestVersion == "r2" {
		t.Fatal("historical revision claimed latest")
	}
	revision.Name = ""
	if consoleLoopRecord(app.LoopView{Revision: revision}).Loop.HumanTitle != "" {
		t.Fatal("invented human title")
	}
}
