package api

import (
	"context"
	"encoding/json"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/graph"
	authoritybadger "github.com/berryhill/aegis/internal/persistence/authority/badger"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/registry"
	"github.com/berryhill/aegis/internal/runtime/hermes"
	statestore "github.com/berryhill/aegis/internal/store"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	consoleweb "github.com/berryhill/aegis/web/console"
)

func TestConsoleQueueParticipantOwnerIsNotExecutor(t *testing.T) {
	view := app.QueueExecutionView{Item: queue.Item{ItemID: "queue-346"}, Submission: queue.Submission{OwnerAgentID: "owner-b"}, LoopExecutions: []execution.LoopExecution{{GraphNodeID: "work", Participant: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "executor-c", Revision: 2, Digest: "sha256:" + strings.Repeat("c", 64)}}}}
	record := consoleQueueRecord(view)
	model := consoleweb.SurfaceModel{Domain: "queue", Records: []consoleweb.RecordModel{record}}
	for name, render := range map[string]func() ([]byte, error){
		"list": func() ([]byte, error) {
			return renderConsole(context.Background(), consoleweb.QueueRecordPanel(model, model.Records))
		},
		"detail": func() ([]byte, error) {
			return renderConsole(context.Background(), consoleweb.QueueWorkspace(model, &record, consoleweb.QueuePinnedTopology{}))
		},
	} {
		body, err := render()
		if err != nil {
			t.Fatal(err)
		}
		if record.Queue.Participant == "owner-b" || !strings.Contains(string(body), "executor-c r2") {
			t.Errorf("%s false participant: projected=%q; rendered executor-c r2=%t (submission owner owner-b)", name, record.Queue.Participant, strings.Contains(string(body), "executor-c r2"))
		}
	}
}

func participant346Ref(id string, revision uint64) reference.RevisionRef {
	return reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: id, Revision: revision, Digest: "sha256:" + strings.Repeat("c", 64)}
}

func TestConsoleQueueParticipantsExactFacts(t *testing.T) {
	c, d := participant346Ref("executor-c", 2), participant346Ref("executor-d", 7)
	for _, tc := range []struct {
		name string
		view app.QueueExecutionView
		want string
	}{
		{"preclaim_multi_node", app.QueueExecutionView{NodeParticipants: map[string]reference.RevisionRef{"z": d, "a": c}, NodeRuntimes: map[string]registry.RuntimeBinding{"a": {}, "z": {}}}, "a · executor-c r2 @ " + c.Digest + "; z · executor-d r7 @ " + d.Digest},
		{"executed_multi_node", app.QueueExecutionView{LoopExecutions: []execution.LoopExecution{{GraphNodeID: "z", Participant: d}, {GraphNodeID: "a", Participant: c}, {GraphNodeID: "a", Participant: c}}}, "a · executor-c r2 @ " + c.Digest + "; z · executor-d r7 @ " + d.Digest},
		{"missing", app.QueueExecutionView{Submission: queue.Submission{OwnerAgentID: "owner-b"}}, "Unavailable"},
		{"runtime_is_not_identity", app.QueueExecutionView{NodeRuntimes: map[string]registry.RuntimeBinding{"a": {Runtime: "hermes-agent", Target: "profile/latest"}}}, "a · Unavailable"},
		{"partial", app.QueueExecutionView{NodeParticipants: map[string]reference.RevisionRef{"a": c}, NodeRuntimes: map[string]registry.RuntimeBinding{"a": {}, "z": {}}}, "a · executor-c r2 @ " + c.Digest + "; z · Unavailable"},
		{"invalid_exact_ref", app.QueueExecutionView{NodeParticipants: map[string]reference.RevisionRef{"a": {ID: "owner-b"}}}, "a · Unavailable"},
		{"conflicting_revision", app.QueueExecutionView{NodeParticipants: map[string]reference.RevisionRef{"a": c}, LoopExecutions: []execution.LoopExecution{{GraphNodeID: "a", Participant: participant346Ref("executor-c", 3)}}}, "a · Unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record := consoleQueueRecord(tc.view)
			if record.Queue.Participant != tc.want {
				t.Fatalf("got %q want %q", record.Queue.Participant, tc.want)
			}
			model := consoleweb.SurfaceModel{Domain: "queue", Records: []consoleweb.RecordModel{record}}
			for name, component := range map[string]func() ([]byte, error){"list": func() ([]byte, error) {
				return renderConsole(context.Background(), consoleweb.QueueRecordPanel(model, model.Records))
			}, "detail": func() ([]byte, error) {
				return renderConsole(context.Background(), consoleweb.QueueWorkspace(model, &record, consoleweb.QueuePinnedTopology{}))
			}} {
				body, err := component()
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(body), tc.want) {
					t.Fatalf("%s omitted exact participants %q", name, tc.want)
				}
			}
		})
	}
}

func TestConsoleQueueParticipantsEscapingAndTargets(t *testing.T) {
	canary := `<script id="participant-canary">alert(346)</script>`
	view := app.QueueExecutionView{Item: queue.Item{ItemID: "queue-346", Digest: "queue-digest", Snapshot: reference.DigestRef{ID: "snapshot-346"}}, Submission: queue.Submission{OwnerAgentID: canary}, Projection: queue.Projection{State: queue.StateQueued}, Runtime: registry.RuntimeBinding{Runtime: "hermes-agent"}, LoopExecutions: []execution.LoopExecution{{GraphNodeID: canary, Participant: participant346Ref("executor-c", 2), Loop: participant346Ref("loop-c", 4)}}}
	record := consoleQueueRecord(view)
	if record.Key != "queue-346" || record.Label != "queue-346" || record.Revision != "snapshot-346" || record.Runtime != "hermes-agent" || record.Lifecycle != "claimable" {
		t.Fatalf("identity/runtime/state changed: %+v", record)
	}
	if record.Queue.Links[0].URL != "/console/agents?record_key=executor-c&revision=2#/agents/executor-c" {
		t.Fatal("participant target changed", record.Queue.Links)
	}
	model := consoleweb.SurfaceModel{Domain: "queue", CSRF: "csrf-canary", Records: []consoleweb.RecordModel{record}}
	for name, component := range map[string]func() ([]byte, error){"list": func() ([]byte, error) {
		return renderConsole(context.Background(), consoleweb.QueueRecordPanel(model, model.Records))
	}, "detail": func() ([]byte, error) {
		return renderConsole(context.Background(), consoleweb.QueueWorkspace(model, &record, consoleweb.QueuePinnedTopology{}))
	}} {
		body, err := component()
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		if strings.Contains(text, canary) || !strings.Contains(text, "&lt;script") || !strings.Contains(text, "executor-c r2") || !strings.Contains(text, "Submission owner agent") || !strings.Contains(text, "hermes-agent") {
			t.Fatalf("%s escaping/provenance/runtime absent", name)
		}
		if name == "list" && (!strings.Contains(text, `id="record-queue-346"`) || !strings.Contains(text, `href="/console/queue?record_key=queue-346#/queue/queue-346"`)) {
			t.Fatalf("row target changed: %s", text)
		}
		if name == "detail" && (!strings.Contains(text, `action="/console/queue/queue-346/operate"`) || !strings.Contains(text, `name="csrf" value="csrf-canary"`)) {
			t.Fatal("action/CSRF target changed")
		}
	}
}

// Separate child lifetimes ensure the real Badger stores close before reopen.
// No listener, provider invocation, granted authority or runtime success claim.
func TestConsoleQueueParticipantsPersistenceRestart(t *testing.T) {
	root := t.TempDir()
	for _, phase := range []string{"seed", "read"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestConsoleQueueParticipantsPersistenceChild$", "-test.v")
		cmd.Env = append(os.Environ(), "AEGIS_PARTICIPANT_346_ROOT="+root, "AEGIS_PARTICIPANT_346_PHASE="+phase)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", phase, err, out)
		}
	}
}

func TestConsoleQueueParticipantsPersistenceChild(t *testing.T) {
	root := os.Getenv("AEGIS_PARTICIPANT_346_ROOT")
	if root == "" {
		t.Skip("child-only persistence fixture")
	}
	var svc *app.Service
	if os.Getenv("AEGIS_PARTICIPANT_346_PHASE") == "seed" {
		svc = apiServiceAt(t, filepath.Clean(root))
		raw, err := json.Marshal(svc.Config)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(root, "fixture-config.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	} else {
		raw, err := os.ReadFile(filepath.Join(root, "fixture-config.json"))
		if err != nil {
			t.Fatal(err)
		}
		var cfg config.Config
		if err = json.Unmarshal(raw, &cfg); err != nil {
			t.Fatal(err)
		}
		state, err := statestore.Open(cfg.StateDir)
		if err != nil {
			t.Fatal(err)
		}
		authority, err := authoritybadger.Open(context.Background(), filepath.Join(cfg.StateDir, "persistence", "authority-v1"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = authority.Close() })
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		svc = app.New(cfg, state, authority, authority, hermes.New(cfg.HermesExecutable, logger), logger)
	}
	store := configureAPIFleet(t, svc)
	ctx := context.Background()
	sub, err := svc.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("AEGIS_PARTICIPANT_346_PHASE") == "seed" {
		agent := registerDoerWorkspaceAgent(t, svc, sub, "executor-c", sub.PrincipalID).Revision
		history := storeAgentExecutionHistory(t, svc, store, agent)
		second := registerDoerWorkspaceAgent(t, svc, sub, "executor-d", sub.PrincipalID).Revision
		seedParticipant346Multi(t, svc, history, second)
		latest := agent
		latest.Revision = 2
		latest.Runtime.Target = "profile/latest-must-not-be-used"
		latest, err = registry.SealRevision(latest)
		if err != nil {
			t.Fatal(err)
		}
		fact := fleet.AuditFact{Event: core.AuditEvent{Type: "agent.revision.published", SubjectID: sub.ID, PrincipalID: sub.PrincipalID, Outcome: "succeeded", Reason: "synthetic history drift fixture"}}
		if err = store.PublishAgentRevision(ctx, latest, fact); err != nil {
			t.Fatal(err)
		}
	}
	view, err := svc.GetQueueItemAs(ctx, sub, "agent-history-item")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.LoopExecutions) != 0 || len(view.NodeParticipants) != 1 || len(view.NodeRuntimes) != 1 {
		t.Fatalf("preclaim facts missing: %+v", view.NodeParticipants)
	}
	ref := view.NodeParticipants["work"]
	if ref.ID != "executor-c" || ref.Revision != 1 || ref.Validate() != nil {
		t.Fatal("exact snapshot participant missing", ref)
	}
	latest, err := store.LatestAgentRevision(ctx, "executor-c")
	if err != nil || latest.Revision != 2 {
		t.Fatal("latest drift fixture missing", err)
	}
	if view.Runtime.Target == latest.Runtime.Target || view.NodeRuntimes["work"].Target == latest.Runtime.Target {
		t.Fatal("runtime provenance used latest revision")
	}
	record := consoleQueueRecord(view)
	if !strings.Contains(record.Queue.Participant, "executor-c r1 @ "+ref.Digest) || record.Queue.TerminalOutcome != "" {
		t.Fatal("persisted preclaim participant lost or terminal success invented", record.Queue.Participant)
	}

	multi, err := svc.GetQueueItemAs(ctx, sub, "multi-346-item")
	if err != nil {
		t.Fatal(err)
	}
	if len(multi.NodeParticipants) != 2 || len(multi.NodeRuntimes) != 2 || len(multi.LoopExecutions) != 0 || multi.Submission.OwnerAgentID != "owner-b" {
		t.Fatal("multi-node persisted facts missing")
	}
	multiRecord := consoleQueueRecord(multi)
	want := "second · executor-d r1 @ " + multi.NodeParticipants["second"].Digest + "; work · executor-c r1 @ " + ref.Digest
	if multiRecord.Queue.Participant != want {
		t.Fatalf("multi-node preclaim: %q", multiRecord.Queue.Participant)
	}
}

func seedParticipant346Multi(t *testing.T, svc *app.Service, history fleet.AcceptedSubmission, second registry.AgentRevision) {
	t.Helper()
	ctx := context.Background()
	repo := svc.FleetRepository
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	fact := func(kind string) fleet.AuditFact {
		return fleet.AuditFact{Event: core.AuditEvent{Type: kind, SubjectID: svc.Config.Principal.ID, PrincipalID: svc.Config.Principal.ID, Outcome: "succeeded", Reason: "synthetic historical preclaim fixture"}}
	}
	gr, err := repo.GetGraphRevision(ctx, history.Snapshot.Graph.ID, history.Snapshot.Graph.Revision)
	check(err)
	gr.Revision = 2
	gr.PreviousDigest = gr.Digest
	node := gr.Nodes[0]
	node.ID = "second"
	node.Participant = reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: second.AgentID, Revision: second.Revision, Digest: second.Digest}
	gr.Nodes = append(gr.Nodes, node)
	gr.InputMappings = append(gr.InputMappings, graph.InputMapping{GraphInput: "value", ToNodeID: "second", ToPort: "value"})
	gr, validation, err := graph.NewRevision(gr)
	check(err)
	_, err = repo.PublishGraph(ctx, graph.PublishRequest{Revision: gr, Validation: validation, ExpectedPreviousDigest: gr.PreviousDigest, IdempotencyKey: "multi-346-graph"}, fact("graph.published"))
	check(err)
	snapshot, err := graph.NewRunSnapshot("multi-346-snapshot", gr, history.Snapshot.Inputs)
	check(err)
	ref := func(id, digest string) reference.DigestRef {
		return reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: id, Digest: digest}
	}
	submission := history.Submission
	submission.SubmissionID = "multi-346-submission"
	submission.IdempotencyKey = "multi-346-intent"
	submission.Snapshot = ref(snapshot.SnapshotID, snapshot.Digest)
	submission.OwnerAgentID = "owner-b"
	submission.OwnerID = svc.Config.Principal.ID
	submission, err = queue.NewSubmission(submission)
	check(err)
	item, err := queue.NewItem(queue.Item{ItemID: "multi-346-item", Submission: ref(submission.SubmissionID, submission.Digest), Snapshot: submission.Snapshot, Authority: submission.Authority, GraphRunID: "multi-346-run", MaxAttempts: 1, EnqueuedAt: svc.Now(), AvailableAt: svc.Now()})
	check(err)
	run, err := execution.NewGraphRun(execution.GraphRun{GraphRunID: item.GraphRunID, QueueItem: ref(item.ItemID, item.Digest), Snapshot: item.Snapshot, Authority: item.Authority, CreatedAt: svc.Now()})
	check(err)
	transition, err := queue.NewTransition(queue.QueueTransition{TransitionID: "multi-346-queued", QueueItemID: item.ItemID, To: queue.StateQueued, Reason: "synthetic historical preclaim fixture", OccurredAt: svc.Now()})
	check(err)
	_, err = repo.AcceptSubmission(ctx, fleet.AcceptedSubmission{Snapshot: snapshot, Submission: submission, QueueItem: item, GraphRun: run, InitialTransition: transition}, fact("submission.accepted"))
	check(err)
}
