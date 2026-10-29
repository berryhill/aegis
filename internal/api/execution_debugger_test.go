package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/looprun"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
)

// This controlled no-provider fixture drives the production interpreter and
// durable checkpoints, NOT a model, host edits or fabricated DOM advancement.
// Queue admission/claim is synthetic; interpreter terminal deliberately leaves
// Queue claimed, demonstrating that it cannot manufacture terminal disposition.
type debuggerCursorFixture struct {
	facts    implementation.StepCheckpointStore
	previous string
}

func (s *debuggerCursorFixture) Load(context.Context) (looprun.Cursor, error) {
	return looprun.Cursor{}, nil
}
func (s *debuggerCursorFixture) Save(ctx context.Context, c looprun.Cursor) error {
	wire, _ := json.Marshal(c)
	fact, err := s.facts.Append(ctx, s.previous, wire)
	if err == nil {
		s.previous = fact.Digest
	}
	return err
}

type debuggerExecutorFixture struct {
	started chan struct{}
	release chan struct{}
}

func (e *debuggerExecutorFixture) Execute(ctx context.Context, q looprun.StepRequest) (looprun.StepResult, error) {
	result := looprun.StepResult{Outputs: looprun.Values{}}
	switch q.Step.ID {
	case "eligibility":
		result.Branch = "eligible"
	case "implement":
		if q.Visit == 1 {
			close(e.started)
			select {
			case <-e.release:
			case <-ctx.Done():
				return result, ctx.Err()
			}
		}
	case "verify":
		if q.Visit == 1 {
			result.Branch = "retry"
		} else {
			result.Branch = "verified"
		}
	}
	return result, nil
}

func debuggerFixture(t *testing.T) (*app.Service, core.Subject, loop.LoopRevision, *debuggerExecutorFixture, <-chan error) {
	return debuggerFixtureKind(t, false)
}

func debuggerFixtureKind(t *testing.T, publishedGraph bool) (*app.Service, core.Subject, loop.LoopRevision, *debuggerExecutorFixture, <-chan error) {
	return debuggerFixtureNamed(t, publishedGraph, "")
}

func debuggerFixtureNamed(t *testing.T, publishedGraph bool, name string) (*app.Service, core.Subject, loop.LoopRevision, *debuggerExecutorFixture, <-chan error) {
	var svc *app.Service
	if root := os.Getenv("AEGIS_DEBUGGER_SEED_ROOT"); root != "" {
		svc = apiServiceAt(t, root)
	} else {
		svc = apiService(t)
	}
	store := configureAPIFleet(t, svc)
	ctx := context.Background()
	subject, err := svc.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	agent := registerDoerWorkspaceAgent(t, svc, subject, "debug-agent", "owner-one").Revision
	var admittedAuthority reference.DigestRef
	if publishedGraph {
		agent, admittedAuthority = debuggerGraphAuthority(t, svc, subject)
	}
	ar := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: agent.AgentID, Revision: agent.Revision, Digest: agent.Digest}
	text := "hello"
	rev, _, err := loop.NewDoerRevision("debug-loop", 1, "", loop.DoerContract{Task: "secret-canary-task-325", Workspace: t.TempDir(), WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", ExpectedText: &text, ExactBytes: true, MaxAttempts: 2})
	if err != nil {
		t.Fatal(err)
	}
	if name != "" {
		rev.Name = name
		rev, _, err = loop.NewRevision(rev)
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = svc.PublishLoopAs(ctx, subject, app.PublishLoopInput{AgentID: agent.AgentID, Revision: rev, IdempotencyKey: "debug-publish"})
	if err != nil {
		t.Fatal(err)
	}
	lr := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: rev.LoopID, Revision: rev.Revision, Digest: rev.Digest}
	direct, err := graph.NewDirectLoopSnapshot("debug-snapshot", graph.Node{ID: "debug-node", Participant: ar, Loop: lr}, nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := graph.NewDirectRunSnapshot(direct)
	if err != nil {
		t.Fatal(err)
	}
	ref := func(id, digest string) reference.DigestRef {
		return reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: id, Digest: digest}
	}
	now := svc.Now()
	authority := ref("fixture-authority", "sha256:"+strings.Repeat("f", 64))
	sub, err := queue.NewSubmission(queue.Submission{SubmissionID: "debug-submission", IdempotencyKey: "debug-intent", Snapshot: ref(snapshot.SnapshotID, snapshot.Digest), Authority: authority, MandateID: "fixture-mandate", Runtime: "hermes-agent", SubmittedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	item, err := queue.NewItem(queue.Item{ItemID: "debug-queue", Submission: ref(sub.SubmissionID, sub.Digest), Snapshot: sub.Snapshot, Authority: authority, GraphRunID: "debug-run", MaxAttempts: 1, EnqueuedAt: now, AvailableAt: now})
	if err != nil {
		t.Fatal(err)
	}
	run, err := execution.NewGraphRun(execution.GraphRun{GraphRunID: item.GraphRunID, QueueItem: ref(item.ItemID, item.Digest), Snapshot: sub.Snapshot, Authority: authority, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	transition, err := queue.NewTransition(queue.QueueTransition{TransitionID: "debug-queued", QueueItemID: item.ItemID, To: queue.StateQueued, Reason: "controlled fixture", OccurredAt: now})
	if err != nil {
		t.Fatal(err)
	}
	audit := fleet.AuditFact{Event: core.AuditEvent{Type: "fixture.debugger", SubjectID: subject.ID, PrincipalID: subject.PrincipalID, Outcome: "succeeded", Reason: "synthetic no-provider fixture"}}
	if publishedGraph {
		accepted := debuggerPublishGraph(t, svc, subject, ar, lr, admittedAuthority)
		snapshot, sub, item, run = accepted.Snapshot, accepted.Submission, accepted.QueueItem, accepted.GraphRun
		authority = admittedAuthority
		now = svc.Now()
	} else if _, err = store.AcceptSubmission(ctx, fleet.AcceptedSubmission{Snapshot: snapshot, Submission: sub, QueueItem: item, GraphRun: run, InitialTransition: transition}, audit); err != nil {
		t.Fatal(err)
	}
	child, err := execution.NewLoopExecution(execution.LoopExecution{LoopExecutionID: "debug-execution", GraphRunID: run.GraphRunID, GraphNodeID: "debug-node", Loop: lr, Participant: ar, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.CreateLoopExecution(ctx, child, audit); err != nil {
		t.Fatal(err)
	}
	claim, err := queue.NewClaim(queue.Claim{ClaimID: "debug-claim", QueueItem: run.QueueItem, AttemptID: "debug-attempt", WorkerID: "fixture-worker", Authority: authority, ClaimedAt: now, ExpiresAt: now.Add(5 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := execution.NewAttempt(execution.Attempt{AttemptID: claim.AttemptID, GraphRunID: run.GraphRunID, LoopExecutionID: child.LoopExecutionID, QueueItem: run.QueueItem, ClaimID: claim.ClaimID, AttemptNumber: 1, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := queue.NewTransition(queue.QueueTransition{TransitionID: "debug-claimed", QueueItemID: item.ItemID, From: queue.StateQueued, To: queue.StateClaimed, ClaimID: claim.ClaimID, Reason: "controlled fixture", OccurredAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.ClaimQueueItem(ctx, claim, attempt, claimed, audit); err != nil {
		t.Fatal(err)
	}
	// Publish a later definition; the run must continue to use revision 1.
	newer, _, err := loop.NewDoerRevision(rev.LoopID, 2, rev.Digest, loop.DoerContract{Task: "newer secret task", Workspace: t.TempDir(), WritableFiles: []string{"other.txt"}, VerifyFile: "other.txt", ExpectedText: &text, ExactBytes: true, MaxAttempts: 1})
	if err != nil {
		t.Fatal(err)
	}
	if name != "" {
		newer.Name = "New renamed Loop"
		newer, _, err = loop.NewRevision(newer)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err = svc.PublishLoopAs(ctx, subject, app.PublishLoopInput{AgentID: agent.AgentID, Revision: newer, ExpectedPreviousDigest: rev.Digest, IdempotencyKey: "debug-newer"}); err != nil {
		t.Fatal(err)
	}
	executor := &debuggerExecutorFixture{started: make(chan struct{}), release: make(chan struct{})}
	done := make(chan error, 1)
	runctx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	go func() {
		_, err := looprun.Run(runctx, attempt.AttemptID, rev, looprun.Values{}, &debuggerCursorFixture{facts: implementation.StepCheckpointStore{DB: store.ImplementationStore(), RunID: attempt.AttemptID, RevisionDigest: rev.Digest}}, executor, nil)
		done <- err
	}()
	select {
	case <-executor.started:
	case <-time.After(5 * time.Second):
		t.Fatal("interpreter did not block")
	}
	return svc, subject, rev, executor, done
}

func TestExecutionDebuggerDurableLiveRead(t *testing.T) {
	executionDebuggerDurableLiveRead(t, false)
}

func TestExecutionDebuggerPublishedGraphDurableLiveRead(t *testing.T) {
	executionDebuggerDurableLiveRead(t, true)
}

func executionDebuggerDurableLiveRead(t *testing.T, publishedGraph bool) {
	t.Helper()
	svc, subject, rev, executor, done := debuggerFixtureKind(t, publishedGraph)
	ctx := context.Background()
	first, err := svc.ExecutionDebuggerAs(ctx, subject, "debug-queue", "")
	if err != nil {
		t.Fatal(err)
	}
	if first.Direct == publishedGraph || (!publishedGraph && first.Source.Digest != rev.Digest) || (publishedGraph && (first.Source.ID != "debug-graph" || first.Source.Revision != 1)) || len(first.Nodes) != 1 || first.Nodes[0].Loop.Digest != rev.Digest || first.Nodes[0].Loop.Revision != 1 || first.Terminal {
		t.Fatalf("wrong pinned read: %+v", first)
	}
	a := first.Nodes[0].Attempts[0]
	last := a.Facts[len(a.Facts)-1]
	if last.Kind != "step_start" || last.StepID != "implement" || last.Pass != 1 || a.Number != 1 {
		t.Fatalf("missing durable in-progress start: %+v", last)
	}
	if _, err = svc.ExecutionDebuggerAs(ctx, subject, "debug-queue", strings.Repeat("a", 64)+":"+strings.Repeat("b", 64)); err == nil {
		t.Fatal("cross-run cursor accepted")
	}
	close(executor.release)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	final, err := svc.ExecutionDebuggerAs(ctx, subject, "debug-queue", first.Watermark)
	if err != nil {
		t.Fatal(err)
	}
	if final.Terminal || final.State != "claimed" || final.Version <= first.Version {
		t.Fatal("interpreter terminal fabricated Queue completion")
	}
	failures, passed := 0, 0
	for _, f := range final.Nodes[0].Attempts[0].Facts {
		if f.Kind == "verification" {
			if f.Outcome == "failed" && f.Pass == 1 {
				failures++
			}
			if f.Outcome == "passed" && f.Pass == 2 {
				passed++
			}
		}
	}
	if failures != 1 || passed != 1 {
		t.Fatalf("pass history lost: %d %d", failures, passed)
	}
	wire, _ := json.Marshal(final)
	if strings.Contains(string(wire), "secret") || strings.Contains(string(wire), "result.txt") {
		t.Fatal("private cursor material disclosed")
	}
	same, err := svc.ExecutionDebuggerAs(ctx, subject, "debug-queue", final.Watermark)
	if err != nil || same.Watermark != final.Watermark {
		t.Fatal("unstable read fingerprint", err)
	}
}

func TestExecutionDebuggerReducerBrowser(t *testing.T) {
	if os.Getenv("AEGIS_DEBUGGER_BROWSER_TEST") != "1" {
		t.Skip("opt-in synthetic transport browser regression")
	}
	python := os.Getenv("AEGIS_GRAPH_BROWSER_PYTHON")
	if python == "" {
		python = "python3"
	}
	output, err := exec.Command(python, "../../scripts/execution_debugger_reducer_test.py").CombinedOutput()
	t.Log(string(output))
	if err != nil {
		t.Fatal(err)
	}
}

func TestExecutionDebuggerConsoleBrowser(t *testing.T) {
	executionDebuggerConsoleBrowser(t, false)
}

func TestExecutionDebuggerPublishedGraphConsoleBrowser(t *testing.T) {
	executionDebuggerConsoleBrowser(t, true)
}

func executionDebuggerConsoleBrowser(t *testing.T, publishedGraph bool, terminal ...bool) {
	if os.Getenv("AEGIS_DEBUGGER_BROWSER_TEST") != "1" {
		t.Skip("opt-in Python Playwright controlled fixture")
	}
	svc, subject, _, executor, done := debuggerFixtureKind(t, publishedGraph)
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := probe.Addr().String()
	probe.Close()
	svc.Config.API.Listen = address
	svc.Config.API.Console.Origin = "http://" + address
	ctx, cancel := context.WithCancel(context.Background())
	serverDone := make(chan error, 1)
	go func() { serverDone <- Serve(ctx, svc) }()
	defer func() {
		cancel()
		if err := <-serverDone; err != nil {
			t.Error(err)
		}
	}()
	waitFor(t, "tcp", address)
	python := os.Getenv("AEGIS_GRAPH_BROWSER_PYTHON")
	if python == "" {
		python = "python3"
	}
	command := exec.Command(python, "../../scripts/execution_debugger_browser_test.py")
	command.Env = append(os.Environ(), "AEGIS_CONSOLE_BASE=http://"+address, "AEGIS_CONSOLE_PASSWORD="+principalTestSecret())
	if publishedGraph {
		command.Env = append(command.Env, "AEGIS_DEBUGGER_GRAPH=1")
	}
	if len(terminal) > 0 && terminal[0] {
		command.Env = append(command.Env, "AEGIS_DEBUGGER_TERMINAL=1")
		if len(terminal) > 1 && terminal[1] {
			command.Env = append(command.Env, "AEGIS_DEBUGGER_SUCCESS=1")
		}
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stderr = os.Stderr
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(stdout)
	released := false
	for scanner.Scan() {
		line := scanner.Text()
		if line == "RECORD_CONTROLLED_DISPOSITION" && len(terminal) > 0 && terminal[0] {
			recordDebuggerDisposition(t, svc, subject, len(terminal) > 1 && terminal[1])
		}
		t.Log(line)
		if line == "RELEASE_CONTROLLED_EXECUTOR" && !released {
			released = true
			close(executor.release)
		}
	}
	if err = command.Wait(); err != nil {
		t.Fatal(err)
	}
	if !released {
		t.Fatal("browser never advanced actual interpreter")
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}
