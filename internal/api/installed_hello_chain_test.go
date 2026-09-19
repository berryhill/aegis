package api

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
)

// Installed CLI, real Unix peer authentication, application services and stores.
// Only Hermes executable/gateway output is synthetic; this is NOT live acceptance.
func TestInstalledHelloOwningServiceChain(t *testing.T) {
	svc := apiService(t)
	store := configureAPIFleet(t, svc)
	root := t.TempDir()
	mustWrite := func(path string, data []byte, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(path, data, mode); err != nil {
			t.Fatal(err)
		}
	}
	save := func(name string, value any) string {
		t.Helper()
		b, e := json.Marshal(value)
		if e != nil {
			t.Fatal(e)
		}
		p := filepath.Join(root, name)
		mustWrite(p, b, 0600)
		return p
	}
	install := filepath.Join(root, "install")
	if err := os.MkdirAll(filepath.Join(install, "venv", "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	mustWrite(svc.Config.HermesExecutable, []byte("#!/bin/sh\nif [ \"${1:-}\" = \"--version\" ]; then echo 'Hermes Agent v0.18.2'; echo 'Install directory: "+install+"'; exit 0; fi\nsleep 60 &\nwait\n"), 0700)
	gateway := `#!/bin/sh
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready","payload":{}}}'
read create
printf '%s\n' '{"jsonrpc":"2.0","id":"create","result":{"session_id":"hello-fixture"}}'
read prompt
printf '%s\n' '{"jsonrpc":"2.0","id":"prompt","result":{"accepted":true}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.start","session_id":"hello-fixture","payload":{}}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.complete","session_id":"hello-fixture","payload":{"status":"complete","text":"hello"}}}'
while read rest; do :; done
`
	mustWrite(filepath.Join(install, "venv", "bin", "python"), []byte(gateway), 0700)
	adapter, err := orchestration.NewRoutedRuntimeAdapter(svc.Hermes, svc.Config.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := evidence.NewBlobVerifier(svc.Store)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := orchestration.NewQueueWorker(store, svc.Fleet, svc.Store, verifier, adapter, svc.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.ConfigureFleet(store, svc.Fleet, worker); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "import-home")
	if err = os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	mustWrite(filepath.Join(home, "config.yaml"), []byte("version: 1\n"), 0600)
	svc.LocalHermesHome = func(string, string) (string, error) { return home, nil }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	subject, err := svc.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := svc.PrepareLocalHermesAgentImportAs(ctx, subject)
	if err != nil {
		t.Fatal(err)
	}
	initial, _, err := svc.ConfirmLocalHermesAgentImportAs(ctx, subject, proposal.RevisionDigest)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, svc) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	waitFor(t, "unix", svc.Config.API.UnixSocket)
	binary := filepath.Join(root, "aegis")
	build := exec.Command("go", "build", "-p=1", "-ldflags=-X github.com/berryhill/aegis/internal/buildinfo.Version=0.0.0-hello-fixture", "-o", binary, "./cmd/aegis")
	build.Dir = "../.."
	build.Env = append(os.Environ(), "GOMAXPROCS=2")
	if out, e := build.CombinedOutput(); e != nil {
		t.Fatalf("build: %v %s", e, out)
	}
	cfg := save("owner.json", svc.Config)
	run := func(dest any, args ...string) {
		t.Helper()
		time.Sleep(250 * time.Millisecond)
		cmd := exec.Command(binary, append([]string{"--config", cfg, "--target", svc.Config.API.Console.Origin}, args...)...)
		out, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("%v: %v %s", args, e, out)
		}
		if dest != nil {
			if e = json.Unmarshal(out, dest); e != nil {
				t.Fatalf("decode %v: %v %s", args, e, out)
			}
		}
	}
	deny := func(args ...string) {
		t.Helper()
		time.Sleep(250 * time.Millisecond)
		out, e := exec.Command(binary, append([]string{"--config", cfg, "--target", svc.Config.API.Console.Origin}, args...)...).CombinedOutput()
		if e == nil {
			t.Fatalf("unauthorized success %v: %s", args, out)
		}
	}
	var original core.CanonicalCharter
	run(&original, "charter", "show", initial.Revision.AgentID, "1")
	charter := original.Charter
	charter.Revision = 2
	charter.Stanzas[0].Hermes.Model = "fixture-model"
	charter.Stanzas[0].Hermes.Provider = "test"
	charter.Stanzas[0].Hermes.Toolsets = []string{"no_mcp"}
	charter.Stanzas[0].Grant.Tools = []string{"no_mcp"}
	charter.Stanzas[0].Scopes.Credentials = []string{"provider:test"}
	var canonical core.CanonicalCharter
	run(&canonical, "charter", "import", save("charter.json", charter))
	ref := func(id string, rev uint64, digest string) reference.RevisionRef {
		return reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: id, Revision: rev, Digest: digest}
	}
	var agent app.FleetAgent
	run(&agent, "agents", "approve-charter", charter.AgentID, save("successor.json", app.ApproveAgentCharterInput{Expected: ref(initial.Revision.AgentID, 1, initial.Revision.Digest), Charter: ref(charter.AgentID, 2, canonical.Digest)}))
	if agent.Revision.Revision != 2 || agent.Registration != initial.Registration {
		t.Fatal("successor lost imported provenance")
	}
	// Two immutable hello revisions exercise preservation of the exact predecessor.
	var published app.PublishedLoop
	previous := ""
	for rev := uint64(1); rev <= 2; rev++ {
		draft := filepath.Join(root, "hello-"+strconv.FormatUint(rev, 10)+".json")
		input := save("hello-input.json", map[string]any{"agent_id": charter.AgentID, "loop_id": "installed-hello", "revision": rev, "previous_digest": previous, "idempotency_key": "hello-" + strconv.FormatUint(rev, 10)})
		if out, e := exec.Command(binary, "loops", "hello", input, "--output", draft).CombinedOutput(); e != nil {
			t.Fatalf("hello: %v %s", e, out)
		}
		run(&published, "loops", "publish", draft)
		if published.Revision.PreviousDigest != previous {
			t.Fatal("lost exact Loop predecessor")
		}
		previous = published.Revision.Digest
	}
	lr := ref(published.Revision.LoopID, 2, published.Revision.Digest)
	run(nil, "loops", "activate", lr.ID, save("activate.json", app.SetLoopLifecycleInput{AgentID: charter.AgentID, Loop: lr, State: loop.LifecycleActive, EventID: "hello-active"}))
	value := graph.Port{ID: "task", Type: graph.TypeString, Required: true}
	result := graph.Port{ID: "report", Type: graph.TypeString, Required: true}
	criteria := graph.Port{ID: "acceptance_criteria", Type: graph.TypeString, Required: true}
	gr := graph.GraphRevision{GraphID: "installed-hello", Revision: 1, Inputs: []graph.Port{value, criteria}, Outputs: []graph.Port{result}, Nodes: []graph.Node{{ID: "work", Participant: ref(charter.AgentID, 2, agent.Revision.Digest), Loop: lr, Inputs: []graph.Port{value, criteria}, Outputs: []graph.Port{result}}}, InputMappings: []graph.InputMapping{{GraphInput: "task", ToNodeID: "work", ToPort: "task"}, {GraphInput: "acceptance_criteria", ToNodeID: "work", ToPort: "acceptance_criteria"}}, OutputMappings: []graph.OutputMapping{{FromNodeID: "work", FromPort: "report", GraphOutput: "report"}}}
	var gp app.PublishedGraph
	run(&gp, "graphs", "publish", save("graph.json", app.PublishGraphInput{AgentID: charter.AgentID, Revision: gr, IdempotencyKey: "hello-graph"}))
	input := app.SubmitGraphInput{WorkspaceAgentID: charter.AgentID, Graph: ref(gp.Revision.GraphID, 1, gp.Revision.Digest), Inputs: []graph.NormalizedInput{{PortID: "task", Type: graph.TypeString, Value: json.RawMessage(`"Say hello without newline"`)}, {PortID: "acceptance_criteria", Type: graph.TypeString, Value: json.RawMessage(`"exact hello"`)}}, SubmissionID: "hello-submit", IdempotencyKey: "hello-submit", SnapshotID: "hello-snapshot", QueueItemID: "hello-item", GraphRunID: "hello-run", TransitionID: "hello-admit", RejectionID: "hello-reject", MaxAttempts: 1}
	var accepted orchestration.SubmissionDecision
	run(&accepted, "graphs", "submit", save("submit.json", input))
	if accepted.Accepted == nil || accepted.Accepted.InitialTransition.To != queue.StatePreparationPending {
		t.Fatal("workspace invented runtime authority")
	}
	bind := app.BindQueueRuntimeInput{AgentID: charter.AgentID, QueueItemID: input.QueueItemID, Authority: accepted.Accepted.Submission.Authority, BindingID: "hello-bind", TransitionID: "hello-bound"}
	deny("queue", "bind-runtime", save("bind.json", bind))
	var review core.Review
	run(&review, "plan", "preview", charter.AgentID, "--revision", "2")
	var approval core.Approval
	run(&approval, "approval", "request", review.Plan.ID, "--ttl", "1m")
	deny("provision", review.Plan.ID, approval.ID)
	run(&approval, "approval", "approve", approval.ID)
	run(nil, "provision", review.Plan.ID, approval.ID)
	var preview struct {
		Mandate core.Mandate `json:"mandate"`
	}
	run(&preview, "session", "preview", charter.AgentID, "--revision", "2", "--stanza", "principal")
	var session core.Session
	run(&session, "session", "start", preview.Mandate.ID)
	defer func() { _ = svc.TerminateSessionAs(ctx, subject, session.ID, "fixture complete") }()
	var runtimeAuthority reference.DigestRef
	run(&runtimeAuthority, "session", "authority", session.ID)
	bind.Authority = runtimeAuthority
	run(nil, "queue", "bind-runtime", save("bind.json", bind))
	work := orchestration.WorkRequest{Authority: runtimeAuthority, QueueItemID: input.QueueItemID, WorkerID: "hello-worker", LoopExecutionID: "hello-execution", ClaimID: "hello-claim", AttemptID: "hello-attempt", ClaimTransitionID: "hello-claimed", TerminalTransitionID: "hello-terminal", DispositionID: "hello-disposition", ArtifactID: "hello-artifact", LeaseDuration: time.Minute}
	run(nil, "queue", "process", save("process.json", work))
	var view app.QueueExecutionView
	run(&view, "queue", "show", input.QueueItemID)
	if view.Projection.State != queue.StateSucceeded || view.Disposition == nil || view.Disposition.State != execution.StateSucceeded || view.Artifact == nil {
		t.Fatalf("no independently verified hello: %+v", view)
	}
	if len(view.Claims) != 1 || len(view.Attempts) != 1 || len(view.LoopExecutions) != 1 || view.LoopExecutions[0].Loop != lr {
		t.Fatal("execution lineage mismatch")
	}
	// A gateway completion error carrying the expected text is still failure.
	mustWrite(filepath.Join(install, "venv", "bin", "python"), []byte(strings.Replace(gateway, `"status":"complete"`, `"status":"error"`, 1)), 0700)
	input.SubmissionID = "error-submit"
	input.IdempotencyKey = "error-submit"
	input.SnapshotID = "error-snapshot"
	input.QueueItemID = "error-item"
	input.GraphRunID = "error-run"
	input.TransitionID = "error-admit"
	input.RejectionID = "error-reject"
	run(&accepted, "graphs", "submit", save("submit-error.json", input))
	bind.QueueItemID = input.QueueItemID
	bind.BindingID = "error-bind"
	bind.TransitionID = "error-bound"
	run(nil, "queue", "bind-runtime", save("bind-error.json", bind))
	work.QueueItemID = input.QueueItemID
	work.LoopExecutionID = "error-execution"
	work.ClaimID = "error-claim"
	work.AttemptID = "error-attempt"
	work.ClaimTransitionID = "error-claimed"
	work.TerminalTransitionID = "error-terminal"
	work.DispositionID = "error-disposition"
	work.ArtifactID = "error-artifact"
	deny("queue", "process", save("process-error.json", work))
	view = app.QueueExecutionView{}
	run(&view, "queue", "show", input.QueueItemID)
	if view.Projection.State != queue.StateFailed || view.Disposition == nil || view.Disposition.State != execution.StateFailed || view.Artifact != nil {
		t.Fatalf("completion error became success: %+v", view)
	}
}
