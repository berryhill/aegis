package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/disposition"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
)

// Canonical synthetic history only; every tested mutation uses the real browser,
// Serve, password verifier, Manager registry, service and durable repository.
func seedPresentationJourneys(t *testing.T, svc *app.Service) {
	t.Helper()
	store := configureAPIFleet(t, svc)
	ctx := context.Background()
	sub, err := svc.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	agent := registerDoerWorkspaceAgent(t, svc, sub, "presentation-owner", sub.PrincipalID).Revision
	revision, _, err := loop.NewDoerRevision("presentation-loop", 1, "", loop.DoerContract{Task: "Presentation fixture; never execute", Workspace: t.TempDir(), WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", MaxAttempts: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.PublishLoopAs(ctx, sub, app.PublishLoopInput{AgentID: agent.AgentID, Revision: revision, IdempotencyKey: "presentation-publish"}); err != nil {
		t.Fatal(err)
	}
	newer := revision
	newer.Revision = 2
	newer.PreviousDigest = revision.Digest
	newer, _, err = loop.NewRevision(newer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.PublishLoopAs(ctx, sub, app.PublishLoopInput{AgentID: agent.AgentID, Revision: newer, ExpectedPreviousDigest: revision.Digest, IdempotencyKey: "presentation-publish-two"}); err != nil {
		t.Fatal(err)
	}
	old := storeAgentExecutionHistory(t, svc, store, agent)
	gr, err := store.GetGraphRevision(ctx, old.Snapshot.Graph.ID, old.Snapshot.Graph.Revision)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := graph.NewRunSnapshot("presentation-snapshot", gr, old.Snapshot.Inputs)
	if err != nil {
		t.Fatal(err)
	}
	ref := func(id, digest string) reference.DigestRef {
		return reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: id, Digest: digest}
	}
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	now := svc.Now()
	submission, err := queue.NewSubmission(queue.Submission{SubmissionID: "presentation-submission", IdempotencyKey: "presentation-submission", Snapshot: ref(snapshot.SnapshotID, snapshot.Digest), Authority: old.Submission.Authority, MandateID: "presentation-mandate", Runtime: "hermes", SubmittedAt: now, OwnerID: sub.PrincipalID, OwnerAgentID: agent.AgentID})
	check(err)
	item, err := queue.NewItem(queue.Item{ItemID: "presentation-item", Submission: ref(submission.SubmissionID, submission.Digest), Snapshot: submission.Snapshot, Authority: submission.Authority, GraphRunID: "presentation-run", MaxAttempts: 1, EnqueuedAt: now, AvailableAt: now})
	check(err)
	run, err := execution.NewGraphRun(execution.GraphRun{GraphRunID: item.GraphRunID, QueueItem: ref(item.ItemID, item.Digest), Snapshot: item.Snapshot, Authority: item.Authority, CreatedAt: now})
	check(err)
	tr, err := queue.NewTransition(queue.QueueTransition{TransitionID: "presentation-queued", QueueItemID: item.ItemID, To: queue.StateQueued, Reason: "synthetic history", OccurredAt: now})
	check(err)
	fact := func(kind string) fleet.AuditFact {
		return fleet.AuditFact{Event: core.AuditEvent{Type: kind, SubjectID: sub.ID, PrincipalID: sub.PrincipalID, Outcome: "succeeded", Reason: "synthetic presentation history"}}
	}
	_, err = store.AcceptSubmission(ctx, fleet.AcceptedSubmission{Snapshot: snapshot, Submission: submission, QueueItem: item, GraphRun: run, InitialTransition: tr}, fact("submission.accepted"))
	check(err)
	cancellation, err := queue.NewCancellation(queue.Cancellation{CancellationID: "presentation-cancel", QueueItem: run.QueueItem, Reason: "operator cancelled", OccurredAt: now})
	check(err)
	terminal, err := queue.NewTransition(queue.QueueTransition{TransitionID: "presentation-terminal", QueueItemID: item.ItemID, From: queue.StateQueued, To: queue.StateCancelled, Reason: cancellation.Reason, OccurredAt: now})
	check(err)
	d, err := disposition.New(disposition.Record{DispositionID: "presentation-disposition", GraphRunID: run.GraphRunID, QueueItem: run.QueueItem, Authority: item.Authority, State: execution.StateCancelled, ReasonCode: cancellation.Reason, OccurredAt: now})
	check(err)
	check(store.CancelQueueItem(ctx, fleet.CancellationMutation{Cancellation: cancellation, Transition: terminal, Disposition: d}, fact("queue.cancelled")))
	// Foreign execution ownership is canonical disposable fixture data, not a
	// forged browser subject. The real password session must be denied that write.
	foreign := registerDoerWorkspaceAgent(t, svc, sub, "presentation-foreign", "foreign-principal").Revision
	foreignGraph := gr
	foreignGraph.GraphID, foreignGraph.Digest = "foreign-presentation-graph", ""
	foreignGraph.Nodes = append([]graph.Node(nil), gr.Nodes...)
	for i := range foreignGraph.Nodes {
		foreignGraph.Nodes[i].Participant = app.RevisionReference(foreign.AgentID, foreign.Revision, foreign.Digest)
	}
	foreignGraph, graphValidation, err := graph.NewRevision(foreignGraph)
	check(err)
	_, err = store.PublishGraph(ctx, graph.PublishRequest{Revision: foreignGraph, Validation: graphValidation, IdempotencyKey: "foreign-fixture-graph"}, fact("graph.published"))
	check(err)
	foreignSnapshot, err := graph.NewRunSnapshot("foreign-presentation-snapshot", foreignGraph, old.Snapshot.Inputs)
	check(err)
	foreignSubmission, err := queue.NewSubmission(queue.Submission{SubmissionID: "foreign-presentation-submission", IdempotencyKey: "foreign-presentation-submission", Snapshot: ref(foreignSnapshot.SnapshotID, foreignSnapshot.Digest), Authority: old.Submission.Authority, MandateID: "foreign-presentation-mandate", Runtime: "hermes", SubmittedAt: now, OwnerID: "foreign-principal", OwnerAgentID: foreign.AgentID})
	check(err)
	foreignItem, err := queue.NewItem(queue.Item{ItemID: "foreign-presentation-item", Submission: ref(foreignSubmission.SubmissionID, foreignSubmission.Digest), Snapshot: foreignSubmission.Snapshot, Authority: foreignSubmission.Authority, GraphRunID: "foreign-presentation-run", MaxAttempts: 1, EnqueuedAt: now, AvailableAt: now})
	check(err)
	foreignRun, err := execution.NewGraphRun(execution.GraphRun{GraphRunID: foreignItem.GraphRunID, QueueItem: ref(foreignItem.ItemID, foreignItem.Digest), Snapshot: foreignItem.Snapshot, Authority: foreignItem.Authority, CreatedAt: now})
	check(err)
	foreignQueued, err := queue.NewTransition(queue.QueueTransition{TransitionID: "foreign-presentation-queued", QueueItemID: foreignItem.ItemID, To: queue.StateQueued, Reason: "synthetic foreign history", OccurredAt: now})
	check(err)
	_, err = store.AcceptSubmission(ctx, fleet.AcceptedSubmission{Snapshot: foreignSnapshot, Submission: foreignSubmission, QueueItem: foreignItem, GraphRun: foreignRun, InitialTransition: foreignQueued}, fact("submission.accepted"))
	check(err)
	foreignCancel, err := queue.NewCancellation(queue.Cancellation{CancellationID: "foreign-presentation-cancel", QueueItem: foreignRun.QueueItem, Reason: "operator cancelled", OccurredAt: now})
	check(err)
	foreignTerminal, err := queue.NewTransition(queue.QueueTransition{TransitionID: "foreign-presentation-terminal", QueueItemID: foreignItem.ItemID, From: queue.StateQueued, To: queue.StateCancelled, Reason: foreignCancel.Reason, OccurredAt: now})
	check(err)
	foreignDisposition, err := disposition.New(disposition.Record{DispositionID: "foreign-presentation-disposition", GraphRunID: foreignRun.GraphRunID, QueueItem: foreignRun.QueueItem, Authority: foreignItem.Authority, State: execution.StateCancelled, ReasonCode: foreignCancel.Reason, OccurredAt: now})
	check(err)
	check(store.CancelQueueItem(ctx, fleet.CancellationMutation{Cancellation: foreignCancel, Transition: foreignTerminal, Disposition: foreignDisposition}, fact("queue.cancelled")))
	metadata, err := json.Marshal(map[string]string{"foreign_queue_digest": foreignItem.Digest})
	check(err)
	check(os.WriteFile(filepath.Join(os.Getenv("AEGIS_PRESENTATION_PROOF"), "foreign-fixture.json"), metadata, 0600))
}

func TestPresentationRealBrowser(t *testing.T) {
	python := os.Getenv("AEGIS_BROWSER_PYTHON")
	if python == "" {
		t.Skip("opt-in real browser")
	}
	port := os.Getenv("AEGIS_ACCEPTANCE_PORT")
	if port == "" {
		t.Fatal("lifetime candidate port claim required")
	}
	root := os.Getenv("AEGIS_PRESENTATION_PROOF")
	if root == "" {
		t.Fatal("private proof root required")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	svc := apiService(t)
	logFile, err := os.Create(filepath.Join(root, "serve.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	svc.Log = slog.New(slog.NewTextHandler(logFile, nil))
	start := time.Now().UTC()
	var elapsed atomic.Int64
	svc.Now = func() time.Time { return start.Add(time.Duration(elapsed.Load())) }
	seedPresentationJourneys(t, svc)
	beforeSubject, err := svc.AuthenticateUnixPeer(context.Background(), uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	beforeQueue, err := svc.GetQueueItemAs(context.Background(), beforeSubject, "presentation-item")
	if err != nil {
		t.Fatal(err)
	}
	svc.Config.API.Listen = net.JoinHostPort("127.0.0.1", port)
	svc.Config.API.Console.Origin = "http://" + svc.Config.API.Listen
	svc.Config.API.Console.SessionTTL = 6 * time.Hour
	svc.Config.API.Console.ActionAuthTTL = 2 * time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, svc) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	waitFor(t, "tcp", svc.Config.API.Listen)
	// Stage clock is private fixture orchestration, not a product endpoint.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				raw, err := os.ReadFile(filepath.Join(root, "stage.json"))
				if err == nil {
					var seconds int64
					if json.Unmarshal(raw, &seconds) == nil {
						elapsed.Store(int64(time.Duration(seconds) * time.Second))
					}
				}
			}
		}
	}()
	repo, _ := filepath.Abs("../..")
	cmd := exec.Command(python, filepath.Join(repo, "scripts/presentation_console_browser_test.py"))
	cmd.Env = append(os.Environ(), "AEGIS_PRESENTATION_ORIGIN="+svc.Config.API.Console.Origin)
	out, err := cmd.CombinedOutput()
	t.Log(string(out))
	if err != nil {
		t.Fatal(err)
	}
	afterSubject, err := svc.AuthenticateUnixPeer(context.Background(), uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	afterQueue, err := svc.GetQueueItemAs(context.Background(), afterSubject, "presentation-item")
	if err != nil || !reflect.DeepEqual(beforeQueue.Item, afterQueue.Item) || !reflect.DeepEqual(beforeQueue.Attempts, afterQueue.Attempts) || !reflect.DeepEqual(beforeQueue.Claims, afterQueue.Claims) || !reflect.DeepEqual(beforeQueue.Transitions, afterQueue.Transitions) || !reflect.DeepEqual(beforeQueue.Disposition, afterQueue.Disposition) {
		t.Fatal("presentation changed canonical execution history", err)
	}
}
