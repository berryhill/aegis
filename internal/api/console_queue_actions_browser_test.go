package api

import (
	"context"
	"encoding/json"
	"github.com/berryhill/aegis/internal/config"
	authoritybadger "github.com/berryhill/aegis/internal/persistence/authority/badger"
	"github.com/berryhill/aegis/internal/runtime/hermes"
	"github.com/berryhill/aegis/internal/store"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/disposition"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
)

// Child-only canonical historical fixture. No provider, authority substitution,
// projection override or expected output file. Stores close before candidate boot.
func TestConsoleQueueActionsBrowserSeed(t *testing.T) {
	root := os.Getenv("AEGIS_QUEUE_ACTIONS_SEED")
	if root == "" {
		t.Skip("child-only seed")
	}
	svc := apiServiceAt(t, root)
	store := configureAPIFleet(t, svc)
	ctx := context.Background()
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	sub, err := svc.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	check(err)
	agent := registerDoerWorkspaceAgent(t, svc, sub, "browser-actions-agent", sub.PrincipalID).Revision
	history := storeAgentExecutionHistory(t, svc, store, agent)
	gr, err := store.GetGraphRevision(ctx, history.Snapshot.Graph.ID, history.Snapshot.Graph.Revision)
	check(err)
	snapshot, err := graph.NewRunSnapshot("browser-actions-snapshot", gr, history.Snapshot.Inputs)
	check(err)
	ref := func(id, digest string) reference.DigestRef {
		return reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: id, Digest: digest}
	}
	now := svc.Now()
	submission, err := queue.NewSubmission(queue.Submission{SubmissionID: "browser-actions-submission", IdempotencyKey: "browser-actions-intent", Snapshot: ref(snapshot.SnapshotID, snapshot.Digest), Authority: history.Submission.Authority, MandateID: history.Submission.MandateID, Runtime: history.Submission.Runtime, SubmittedAt: now, OwnerID: sub.PrincipalID, OwnerAgentID: agent.AgentID})
	check(err)
	item, err := queue.NewItem(queue.Item{ItemID: "browser-actions-item", Submission: ref(submission.SubmissionID, submission.Digest), Snapshot: submission.Snapshot, Authority: submission.Authority, GraphRunID: "browser-actions-run", MaxAttempts: 1, EnqueuedAt: now, AvailableAt: now})
	check(err)
	run, err := execution.NewGraphRun(execution.GraphRun{GraphRunID: item.GraphRunID, QueueItem: ref(item.ItemID, item.Digest), Snapshot: item.Snapshot, Authority: item.Authority, CreatedAt: now})
	check(err)
	transition, err := queue.NewTransition(queue.QueueTransition{TransitionID: "browser-actions-queued", QueueItemID: item.ItemID, To: queue.StateQueued, Reason: "synthetic browser fixture", OccurredAt: now})
	check(err)
	fact := func(kind string) fleet.AuditFact {
		return fleet.AuditFact{Event: core.AuditEvent{Type: kind, SubjectID: sub.ID, PrincipalID: sub.PrincipalID, Outcome: "succeeded", Reason: "synthetic browser fixture"}}
	}
	_, err = store.AcceptSubmission(ctx, fleet.AcceptedSubmission{Snapshot: snapshot, Submission: submission, QueueItem: item, GraphRun: run, InitialTransition: transition}, fact("submission.accepted"))
	check(err)
	cancellation, err := queue.NewCancellation(queue.Cancellation{CancellationID: "browser-actions-cancel", QueueItem: ref(item.ItemID, item.Digest), Reason: "operator_cancelled", OccurredAt: now})
	check(err)
	cancelled, err := queue.NewTransition(queue.QueueTransition{TransitionID: "browser-actions-cancelled", QueueItemID: item.ItemID, From: queue.StateQueued, To: queue.StateCancelled, Reason: "operator_cancelled", OccurredAt: now})
	check(err)
	terminal, err := disposition.New(disposition.Record{DispositionID: "browser-actions-disposition", GraphRunID: item.GraphRunID, QueueItem: ref(item.ItemID, item.Digest), Authority: item.Authority, State: execution.StateCancelled, ReasonCode: "operator_cancelled", OccurredAt: now})
	check(err)
	check(store.CancelQueueItem(ctx, fleet.CancellationMutation{Cancellation: cancellation, Transition: cancelled, Disposition: terminal}, fact("queue.cancelled")))
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	check(err)
	svc.Config.API.Listen = probe.Addr().String()
	check(probe.Close())
	svc.Config.API.Console.Origin = "http://" + svc.Config.API.Listen
	svc.Config.API.UnixSocket = "aegis.sock"
	raw, err := json.Marshal(svc.Config)
	check(err)
	check(os.WriteFile(filepath.Join(root, "owner.json"), raw, 0600))
	view, err := svc.GetQueueItemAs(ctx, sub, item.ItemID)
	check(err)
	raw, err = json.Marshal(view)
	check(err)
	check(os.WriteFile(filepath.Join(root, "history-before.json"), raw, 0600))
}

func TestConsoleQueueActionsBrowser(t *testing.T) {
	candidate := os.Getenv("AEGIS_QUEUE_ACTIONS_CANDIDATE")
	if candidate == "" {
		t.Skip("set AEGIS_QUEUE_ACTIONS_CANDIDATE for real candidate browser/restart acceptance")
	}
	candidate, err := filepath.Abs(candidate)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	evidence := filepath.Join(repo, ".candidate", "issue332-completion")
	root, err := os.MkdirTemp(evidence, "actions-browser-")
	if err != nil {
		t.Fatal(err)
	}
	t.Log("private evidence:", root)
	seed := exec.Command(os.Args[0], "-test.run=^TestConsoleQueueActionsBrowserSeed$", "-test.v")
	seed.Env = append(os.Environ(), "AEGIS_QUEUE_ACTIONS_SEED="+root)
	if out, err := seed.CombinedOutput(); err != nil {
		t.Fatalf("seed: %v %s", err, out)
	}
	cmd := exec.Command("python3", filepath.Join(repo, "scripts", "queue_actions_browser_test.py"), candidate, root)
	cmd.Env = append(os.Environ(), "AEGIS_CONSOLE_PASSWORD="+principalTestSecret())
	out, err := cmd.CombinedOutput()
	t.Log(string(out))
	if err != nil {
		t.Fatal(err)
	}
	// Independently reopen durable repositories after both real processes exit.
	rawConfig, err := os.ReadFile(filepath.Join(root, "owner.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg config.Config
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		t.Fatal(err)
	}
	state, err := store.Open(cfg.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := authoritybadger.Open(context.Background(), filepath.Join(cfg.StateDir, "persistence", "authority-v1"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = authority.Close() })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := app.New(cfg, state, authority, authority, hermes.New(cfg.HermesExecutable, logger), logger)
	configureAPIFleet(t, svc)
	sub, err := svc.AuthenticateUnixPeer(context.Background(), uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	view, err := svc.GetQueueItemAs(context.Background(), sub, "browser-actions-item")
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, "history-before.json"))
	if err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("browser presentation/unsupported requeue changed canonical history")
	}
	var slot app.LoopReexecutionFact
	if err := svc.Store.Load("queue-loop-reexecution-successor", view.Item.ItemID, &slot); !os.IsNotExist(err) {
		t.Fatal("unsupported browser requeue reserved successor", err)
	}
	archive, err := svc.GetQueueArchiveAs(context.Background(), sub, reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: view.Item.ItemID, Digest: view.Item.Digest})
	if err != nil || archive.Archived || archive.Revision != 2 {
		t.Fatalf("durable unarchive: %+v %v", archive, err)
	}
	t.Log("PASS: independent reopened canonical history unchanged; no successor; durable unarchive revision 2")
}
