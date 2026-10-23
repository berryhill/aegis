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
	"time"

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
	// Persist an actual failed projection through canonical claim/completion APIs.
	// This synthetic history is not a live provider or runtime admission proof.
	loopExecution, err := execution.NewLoopExecution(execution.LoopExecution{LoopExecutionID: "browser-actions-loop", GraphRunID: run.GraphRunID, GraphNodeID: gr.Nodes[0].ID, Loop: snapshot.Loops[0], Participant: snapshot.Participants[0], CreatedAt: now})
	check(err)
	_, err = store.CreateLoopExecution(ctx, loopExecution, fact("loop-execution.created"))
	check(err)
	claim, err := queue.NewClaim(queue.Claim{ClaimID: "browser-actions-claim", QueueItem: ref(item.ItemID, item.Digest), AttemptID: "browser-actions-attempt", WorkerID: "browser-actions-worker", Authority: item.Authority, ClaimedAt: now, ExpiresAt: now.Add(time.Minute)})
	check(err)
	attempt, err := execution.NewAttempt(execution.Attempt{AttemptID: claim.AttemptID, GraphRunID: run.GraphRunID, LoopExecutionID: loopExecution.LoopExecutionID, QueueItem: claim.QueueItem, ClaimID: claim.ClaimID, AttemptNumber: 1, CreatedAt: now})
	check(err)
	claimed, err := queue.NewTransition(queue.QueueTransition{TransitionID: "browser-actions-claimed", QueueItemID: item.ItemID, From: queue.StateQueued, To: queue.StateClaimed, ClaimID: claim.ClaimID, Reason: "synthetic browser fixture", OccurredAt: now})
	check(err)
	check(store.ClaimQueueItem(ctx, claim, attempt, claimed, fact("queue.claimed")))
	failed, err := queue.NewTransition(queue.QueueTransition{TransitionID: "browser-actions-failed", QueueItemID: item.ItemID, From: queue.StateClaimed, To: queue.StateFailed, ClaimID: claim.ClaimID, Reason: "runtime_exit_nonzero", OccurredAt: now})
	check(err)
	terminal, err := disposition.New(disposition.Record{DispositionID: "browser-actions-disposition", GraphRunID: item.GraphRunID, LoopExecutionID: loopExecution.LoopExecutionID, AttemptID: attempt.AttemptID, QueueItem: claim.QueueItem, Authority: item.Authority, State: execution.StateFailed, ReasonCode: failed.Reason, OccurredAt: now})
	check(err)
	completionFact := fact("fleet.disposition.recorded")
	completionFact.Event.AgentID = loopExecution.Participant.ID
	completionFact.Event.StanzaID = "principal"
	completionFact.Event.MandateID = submission.MandateID
	completionFact.Event.Outcome = string(terminal.State)
	completionFact.Event.Reason = terminal.ReasonCode
	completionFact.Event.Metadata = map[string]string{"disposition_id": terminal.DispositionID, "transition_id": failed.TransitionID, "terminal_at": now.UTC().Format(time.RFC3339Nano)}
	check(store.CompleteQueueItem(ctx, fleet.Completion{Claim: claim, Disposition: terminal, Transition: failed}, completionFact, nil))
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
	evidence := filepath.Join(repo, ".candidate", "issue365")
	if err := os.MkdirAll(evidence, 0700); err != nil {
		t.Fatal(err)
	}
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
	if view.Projection.State != queue.StateFailed || view.Disposition == nil || view.Disposition.State != execution.StateFailed {
		t.Fatal("browser fixture must retain canonical FAILED history")
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
