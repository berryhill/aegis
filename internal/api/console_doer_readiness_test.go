package api

import (
	"context"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/loop"
	consoleweb "github.com/berryhill/aegis/web/console"
)

func TestDoerLoopConsoleDoesNotClaimExecutionReadiness(t *testing.T) {
	revision, _, err := loop.NewDoerRevision("doer-console", 1, "", loop.DoerContract{
		Task: "Write the selected file", Workspace: t.TempDir(),
		WritableFiles: []string{"OUTPUT.md"}, VerifyFile: "OUTPUT.md", MaxAttempts: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, want string
		state      loop.Lifecycle
	}{
		{"draft", "not execution-ready", loop.Lifecycle{LoopID: revision.LoopID, State: loop.LifecycleDraft}},
		{"active", "execution readiness unverified", loop.Lifecycle{LoopID: revision.LoopID, State: loop.LifecycleActive, ActiveRevision: revision.Revision, ActiveDigest: revision.Digest}},
		{"historical", "historical Doer revision is not the active execution target", loop.Lifecycle{LoopID: revision.LoopID, State: loop.LifecycleActive, ActiveRevision: 2, ActiveDigest: "sha256:" + strings.Repeat("b", 64)}},
		{"retired", "Retired; terminal lifecycle", loop.Lifecycle{LoopID: revision.LoopID, State: loop.LifecycleRetired}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			record := consoleLoopRecord(app.LoopView{Revision: revision, Lifecycle: tc.state})
			if record.Loop == nil || !record.Loop.DoerV4 || !strings.Contains(record.Readiness, tc.want) {
				t.Fatalf("Doer lifecycle %s presentation is not explicit: %+v", tc.name, record)
			}
			html, err := renderConsole(context.Background(), consoleweb.LoopWorkspace(consoleweb.SurfaceModel{Domain: "loops"}, &record, consoleweb.LoopTopology{}))
			if err != nil {
				t.Fatal(err)
			}
			content := string(html)
			if strings.Contains(content, "Find a Graph") || strings.Contains(content, "Run from a Graph") || !strings.Contains(content, "aegis loops queue FILE") || !strings.Contains(content, tc.want) {
				t.Fatalf("Doer detail offered the wrong execution action/status: %s", content)
			}
		})
	}
}

func TestReusableDoerConsoleIsDefinitionOnly(t *testing.T) {
	revision := loop.LoopRevision{SchemaVersion: loop.DoerReusableSchemaVersion, LoopID: "reusable-doer-console", Revision: 1,
		Digest: "sha256:" + strings.Repeat("a", 64), DoerReusable: &loop.DoerReusableContract{}}
	for _, state := range []loop.Lifecycle{
		{LoopID: revision.LoopID, State: loop.LifecycleDraft},
		{LoopID: revision.LoopID, State: loop.LifecycleActive, ActiveRevision: 1, ActiveDigest: revision.Digest},
	} {
		record := consoleLoopRecord(app.LoopView{Revision: revision, Lifecycle: state})
		if record.Loop == nil || !record.Loop.DoerV5 || record.Loop.CanActivate || !strings.Contains(record.Readiness, "no Queue worker") || !strings.Contains(record.Summary, "definition only · no Queue worker") {
			t.Fatalf("reusable Doer was presented as executable: %+v", record)
		}
		list, err := renderConsole(context.Background(), consoleweb.QueueRecordPanel(consoleweb.SurfaceModel{Domain: "loops"}, []consoleweb.RecordModel{record}))
		if err != nil || !strings.Contains(string(list), "definition only · no Queue worker") {
			t.Fatalf("reusable Doer list hid non-executable status: %s, err %v", list, err)
		}
		html, err := renderConsole(context.Background(), consoleweb.LoopWorkspace(consoleweb.SurfaceModel{Domain: "loops"}, &record, consoleweb.LoopTopology{}))
		if err != nil {
			t.Fatal(err)
		}
		content := string(html)
		if strings.Contains(content, "Find a Graph") || strings.Contains(content, "Run from a Graph") || strings.Contains(content, "aegis loops queue FILE") || !strings.Contains(content, "definition only") {
			t.Fatalf("reusable Doer detail offered an execution action: %s", content)
		}
	}
}
