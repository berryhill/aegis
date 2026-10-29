package api

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	consoleweb "github.com/berryhill/aegis/web/console"
)

func TestQueueLoopNameUsesExactPinnedRevisionAndKeepsExecutionIdentity(t *testing.T) {
	ref := app.RevisionReference("opaque-loop", 1, "sha256:"+strings.Repeat("a", 64))
	old := app.LoopView{Revision: loop.LoopRevision{LoopID: ref.ID, Revision: ref.Revision, Digest: ref.Digest, Name: "Review the change"}}
	latest := app.LoopView{Revision: loop.LoopRevision{LoopID: ref.ID, Revision: 2, Digest: "sha256:" + strings.Repeat("b", 64), Name: "Different name"}}
	view := app.QueueExecutionView{SourceKind: graph.SourceDirectLoop, Source: ref, Item: queue.Item{ItemID: "queue-1"}}
	for _, state := range []queue.State{queue.StateQueued, queue.StateFailed, queue.StateSucceeded} {
		view.Projection.State = state
		record := consoleQueueRecordWithLoops(view, []app.LoopView{latest, old})
		if record.Label != "Review the change" || record.Key != "queue-1" || !strings.Contains(record.Summary, "queue-1") {
			t.Fatalf("state %s: inaccurate row: %+v", state, record)
		}
		var rendered bytes.Buffer
		if err := consoleweb.QueueRecordPanel(consoleweb.SurfaceModel{Domain: "queue"}, []consoleweb.RecordModel{record}).Render(context.Background(), &rendered); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(rendered.String(), ">Review the change</strong>") || !strings.Contains(rendered.String(), "record-queue-1") || !strings.Contains(rendered.String(), "/console/queue?record_key=queue-1#/queue/queue-1") {
			t.Fatalf("row does not present name and retain execution identity: %s", rendered.String())
		}
	}
	if label := consoleQueueRecordWithLoops(view, []app.LoopView{latest}).Label; !strings.Contains(label, "name unavailable") {
		t.Fatalf("substituted latest name: %q", label)
	}
	old.Revision.Name = ""
	if label := consoleQueueRecordWithLoops(view, []app.LoopView{old}).Label; !strings.Contains(label, "name unavailable") {
		t.Fatalf("legacy missing name was not explicit: %q", label)
	}
}

func TestQueueGraphLoopNameOnlyFromPinnedGraphAndLoop(t *testing.T) {
	loopRef := app.RevisionReference("opaque-loop", 1, "sha256:"+strings.Repeat("a", 64))
	graphRef := app.RevisionReference("graph-1", 1, "sha256:"+strings.Repeat("b", 64))
	snapshot := reference.DigestRef{ID: "snapshot-1", Digest: "sha256:" + strings.Repeat("c", 64)}
	view := app.QueueExecutionView{Item: queue.Item{ItemID: "queue-1", Snapshot: snapshot}}
	pinned := app.GraphView{Revision: graph.GraphRevision{GraphID: graphRef.ID, Revision: graphRef.Revision, Digest: graphRef.Digest, Nodes: []graph.Node{{ID: "node-1", Loop: loopRef}}}, Runs: []app.AcceptedGraphRunView{{Snapshot: graph.GraphRunSnapshot{SnapshotID: snapshot.ID, Digest: snapshot.Digest, Graph: graphRef}, QueueItem: view.Item}}}
	loops := []app.LoopView{{Revision: loop.LoopRevision{LoopID: loopRef.ID, Revision: loopRef.Revision, Digest: loopRef.Digest, Name: "Review the change"}}}
	if got := consoleQueueRecordWithLoops(view, loops, []app.GraphView{pinned}).Label; got != "Review the change" {
		t.Fatalf("pre-claim Graph row lost pinned name: %q", got)
	}
	pinned.Revision.Nodes = append(pinned.Revision.Nodes, graph.Node{ID: "node-2", Loop: loopRef})
	if got := consoleQueueRecordWithLoops(view, loops, []app.GraphView{pinned}).Label; got != "Graph execution · 2 Loops" {
		t.Fatalf("multi-Loop Graph chose one name: %q", got)
	}
	pinned.Revision.Digest = "sha256:" + strings.Repeat("d", 64)
	if got := consoleQueueRecordWithLoops(view, loops, []app.GraphView{pinned}).Label; !strings.Contains(got, "name unavailable") {
		t.Fatalf("mismatched Graph revision supplied a name: %q", got)
	}
}

func TestNamedLoopComposerFormsRetainExplicitName(t *testing.T) {
	values := validLoopComposerValues()
	values.Set("name", "Review the change")
	form, err := decodeLoopComposerForm(composerRequest(values))
	if err != nil || form.Revision.Name != "Review the change" {
		t.Fatalf("general composer name lost: %v", err)
	}
	values = validDoerForm()
	values.Set("name", "Review the selected file")
	form, err = decodeDoerComposerForm(doerFormRequest(values))
	if err != nil || form.Revision.Name != "Review the selected file" {
		t.Fatalf("Doer composer name lost: %v", err)
	}
}
