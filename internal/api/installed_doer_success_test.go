package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"

	"github.com/berryhill/aegis/internal/execution"
	"strconv"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

// Offline integration only: real built public CLI -> owning Unix service ->
// durable queue and selected-file verifier. Hermes, Laya and approvals are
// synthetic fixtures, NOT real-provider execution or authenticated human consent.
// Fixture preparation stays separate from the unchanged public queue request.
func TestInstalledOnlineDoerExactBytesSuccess(t *testing.T) {
	svc := apiService(t)
	configureAPIFleet(t, svc)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, svc) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	waitFor(t, "unix", svc.Config.API.UnixSocket)
	client := unixClient(svc.Config.API.UnixSocket)
	subject, err := svc.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	model := "proof-no-key"
	cli := newInstalledLoopQueueClient(t, svc)
	charter := core.Charter{SchemaVersion: core.SchemaVersion, AgentID: "journey-agent", Name: "Journey test", Revision: 1,
		Runtime: core.RuntimeConstraint{Adapter: "hermes", Runtime: "hermes-agent", VersionConstraint: ">=0.18.0", Target: "aegis-owned-ephemeral"},
		Stanzas: []core.TrustStanza{{ID: "principal", Name: "Principal", Enabled: true,
			Authentication: core.AuthenticationPolicy{Methods: []string{"local-os"}, Selectors: []core.IdentitySelector{{SubjectIDs: []string{"local-uid:" + strconv.Itoa(os.Getuid())}, PrincipalIDs: []string{svc.Config.Principal.ID}, Issuers: []string{"linux-so-peercred"}, Environments: []string{"local"}}}, RequireFresh: true, MaxAuthAgeSec: 60},
			Grant:          core.Grant{}, Scopes: core.Scopes{}, Session: core.SessionPolicy{MaximumLifetimeSec: 60, RequireReauth: true},
			Approval: core.ApprovalPolicy{RequiredOperations: []string{"provision"}, MaximumLifetimeSec: 60, SingleUse: true}, InformationFlow: core.InformationFlowPolicy{CrossStanza: "deny"},
			Hermes: core.HermesConfig{Model: model, Provider: "none"}}}, CreatedBy: svc.Config.Principal.ID, CreatedAt: svc.Now()}
	wire, err := json.Marshal(charter)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := svc.ImportCharterAs(ctx, subject, wire)
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := json.Marshal(registry.CurrentFleetFixture{SchemaVersion: registry.CurrentFleetFixtureSchemaVersion, FleetID: "journey-fleet", Agents: []registry.CurrentFleetAgent{{SourceID: "journey-source", AgentID: charter.AgentID,
		Runtime:   registry.RuntimeBinding{Adapter: charter.Runtime.Adapter, Runtime: charter.Runtime.Runtime, Target: charter.Runtime.Target},
		Ownership: registry.Ownership{OwnerID: "journey-owner", AccountabilityID: "journey-owner"}, Lifecycle: registry.LifecycleEnabled,
		Charter: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: charter.AgentID, Revision: 1, Digest: canonical.Digest}}}})
	if err != nil {
		t.Fatal(err)
	}
	var registered struct {
		Agent app.FleetAgent `json:"agent"`
	}
	// Registration's public owning-service API is used because the online CLI
	// transport deliberately does not allow registration.
	apiRequest(t, client, http.MethodPost, "/v1/agents", app.NewRegisterFleetAgentInput(fixture, "journey-fleet", "journey-source"), &registered, http.StatusCreated)
	workspace := t.TempDir()
	text := "hello"
	contract := loop.DoerContract{Task: "Create result.txt", Workspace: workspace, WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", ExpectedText: &text, MaxAttempts: 1, ExactBytes: true}
	// Public authoring is a separate real process; stderr carries draft diagnostics.
	write := func(name string, value any) string {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(cli.root, name)
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	authoring := write("authoring.json", map[string]any{"agent_id": charter.AgentID, "loop_id": "journey-doer", "revision": 1, "idempotency_key": "journey-publish", "doer": contract})
	command := exec.Command(cli.binary, "loops", "doer", authoring)
	out, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	var publication app.PublishLoopInput
	if err := json.Unmarshal(out, &publication); err != nil {
		t.Fatal(err)
	}
	revision := publication.Revision
	publicationPath := write("publication.json", publication)
	var validated app.ValidatedLoop
	if err := cli.run(&validated, "loops", "validate", publicationPath); err != nil {
		t.Fatal(err)
	}
	if validated.Published || validated.Validation.Outcome != loop.ValidationValid {
		t.Fatalf("validation: %+v", validated)
	}
	var published app.PublishedLoop
	if err := cli.run(&published, "loops", "publish", publicationPath); err != nil {
		t.Fatal(err)
	}
	var shown app.LoopView
	if err := cli.run(&shown, "loops", "show", revision.LoopID, "1"); err != nil {
		t.Fatal(err)
	}
	if shown.Revision.Digest != published.Revision.Digest || shown.Lifecycle.State != loop.LifecycleDraft {
		t.Fatalf("publication readback: %+v", shown)
	}
	agent := registered.Agent.Revision
	input := app.QueueLoopInput{Agent: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: agent.AgentID, Revision: agent.Revision, Digest: agent.Digest},
		Loop:           reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: revision.LoopID, Revision: 1, Digest: published.Revision.Digest},
		IdempotencyKey: "journey-request", Activate: true}
	var pending app.QueueLoopResult
	pending, err = cli.queue(input)
	if err != nil {
		t.Fatal(err)
	}
	if pending.Reason != "provisioning_receipt_missing" || pending.RequiredAction != "plan_preview_exact_charter" || pending.RequiredCharter == nil || pending.RequiredCharter.Digest != canonical.Digest || pending.RequestID == "" || pending.QueueItemID != "" || pending.Execution != nil {
		t.Fatalf("missing receipt advanced execution: %+v", pending)
	}
	if view, err := svc.GetLoopViewAs(ctx, subject, revision.LoopID, 1); err != nil || view.Lifecycle.State != loop.LifecycleDraft || len(view.History) != 0 {
		t.Fatalf("missing receipt activated Loop: %+v %v", view.Lifecycle, err)
	}

	var blockedItems []app.QueueExecutionView
	if err := cli.run(&blockedItems, "queue", "list"); err != nil {
		t.Fatal(err)
	}
	var blockedGraphs []json.RawMessage
	if err := cli.run(&blockedGraphs, "graphs", "list"); err != nil {
		t.Fatal(err)
	}
	if len(blockedItems) != 0 || len(blockedGraphs) != 0 {
		t.Fatal("prerequisite denial created work")
	}
	if _, err := os.Stat(filepath.Join(workspace, "result.txt")); !os.IsNotExist(err) {
		t.Fatal("prerequisite denial wrote result")
	}

	// Explicit synthetic operator contract allowlist and local Laya process,
	// matching the existing readiness fixture; neither is inferred from the Loop.
	goBinary, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	goBinary, err = filepath.EvalSymlinks(goBinary)
	if err != nil {
		t.Fatal(err)
	}
	laya := filepath.Join(t.TempDir(), "laya")
	layaScript := `#!/bin/sh
read request
case "$request" in
  *'"kind":"gate"'*) printf '%s\n' '{"version":1,"kind":"gate","answers":{"specified":{"choice":"yes","answer_confidence":0.9},"result_defined":{"choice":"yes","answer_confidence":0.9}}}' ;;
  *'"kind":"verdict"'*) printf '%s\n' '{"version":1,"kind":"verdict","answers":{"done":{"choice":"yes","answer_confidence":0.9},"stays_in_scope":{"choice":"yes","answer_confidence":0.9},"fulfills":{"choice":"yes","answer_confidence":0.9},"works":{"choice":"yes","answer_confidence":0.9},"practices":{"choice":"yes","answer_confidence":0.9}}}' ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(laya, []byte(layaScript), 0700); err != nil {
		t.Fatal(err)
	}
	digest, err := contract.Digest()
	if err != nil {
		t.Fatal(err)
	}
	layaHome := t.TempDir()
	if err := os.Chmod(layaHome, 0700); err != nil {
		t.Fatal(err)
	}
	if err := svc.QueueWorker.ConfigureImplementation(config.Implementation{GoBinary: goBinary, AuthorizedContracts: []string{digest}, LayaPython: laya, LayaHome: layaHome}, svc.Config.StateDir, svc.Hermes); err != nil {
		t.Fatal(err)
	}
	if err := svc.QueueWorker.ValidateLoopAdmission(published.Revision, agent); err != nil {
		t.Fatalf("Doer controller fixture prerequisites: %v", err)
	}
	var review core.Review
	apiRequest(t, client, http.MethodPost, "/v1/plans/preview", map[string]any{"agent": charter.AgentID, "revision": 1, "environment": core.Environment{Name: "local"}}, &review, http.StatusCreated)
	var approval core.Approval
	apiRequest(t, client, http.MethodPost, "/v1/approvals", map[string]any{"plan_id": review.Plan.ID, "ttl": "1m"}, &approval, http.StatusCreated)
	apiRequest(t, client, http.MethodPost, "/v1/approvals/"+approval.ID+"/decision", map[string]bool{"approve": true}, &approval, http.StatusOK)
	var receipt core.Receipt
	apiRequest(t, client, http.MethodPost, "/v1/provision", map[string]string{"plan_id": review.Plan.ID, "approval_id": approval.ID}, &receipt, http.StatusCreated)
	if receipt.Status != "verified" {
		t.Fatalf("fixture receipt not verified: %s", receipt.Status)
	}

	// The gateway fixture returns a patch, never a verification claim. The
	// controller's selected-file verifier independently reloads the real bytes.
	root := t.TempDir()
	install := filepath.Join(root, "install")
	if err := os.MkdirAll(filepath.Join(install, "venv", "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(svc.Config.HermesExecutable, []byte("#!/bin/sh\nif [ \"${1:-}\" = \"--version\" ]; then echo 'Hermes Agent v0.18.2'; echo 'Install directory: "+install+"'; exit 0; fi\nsleep 60 &\nwait\n"), 0700); err != nil {
		t.Fatal(err)
	}
	edits, err := json.Marshal(map[string]any{"edits": []implementation.Edit{{Path: "result.txt", Content: []byte(text)}}, "report": "Implemented the selected file"})
	if err != nil {
		t.Fatal(err)
	}
	event, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "event", "params": map[string]any{"type": "message.complete", "session_id": "fixture", "payload": map[string]string{"status": "complete", "text": string(edits)}}})
	if err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready","payload":{}}}'
[ "$HERMES_TUI_TOOLSETS" = "context_engine" ] || exit 90
IFS= read -r tools || exit 1
printf '%%s\n' '{"jsonrpc":"2.0","id":"aegis-tools","result":{"total":0,"sections":[]}}'
while IFS= read -r create; do
  printf '%%s\n' '{"jsonrpc":"2.0","id":"create","result":{"session_id":"fixture"}}'
  IFS= read -r prompt || exit 0
  printf '%%s\n' '{"jsonrpc":"2.0","id":"prompt","result":{"accepted":true}}'
  printf '%%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.start","session_id":"fixture","payload":{}}}'
  printf '%%s\n' '%s'
done
`, string(event))
	if err := os.WriteFile(filepath.Join(install, "venv", "bin", "python"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}

	var result app.QueueLoopResult
	result, err = cli.queue(input)
	if err != nil {
		t.Fatal(err)
	}
	if result.RequestID != pending.RequestID || result.QueueItemID == "" || result.Execution == nil || result.Execution.Projection.State != queue.StateSucceeded {
		t.Fatalf("exact one-request v4 execution blocked: %+v", result)
	}
	view, err := svc.GetLoopViewAs(ctx, subject, revision.LoopID, 1)
	if err != nil || view.Lifecycle.State != loop.LifecycleActive || len(view.History) != 1 {
		t.Fatalf("activation: %+v %v", view.Lifecycle, err)
	}
	graphs, err := svc.ListGraphsAs(ctx, subject)
	if err != nil || len(graphs) != 1 {
		t.Fatalf("graph count: %d %v", len(graphs), err)
	}
	items, err := svc.ListQueueAs(ctx, subject)
	if err != nil || len(items) != 1 {
		t.Fatalf("queue count: %d %v", len(items), err)
	}
	var readback app.QueueExecutionView
	if err := cli.run(&readback, "queue", "show", result.QueueItemID); err != nil {
		t.Fatal(err)
	}
	if readback.Projection.State != queue.StateSucceeded || len(readback.Claims) != 1 || len(readback.Attempts) != 1 || readback.Attempts[0].AttemptNumber != 1 || readback.Artifact == nil || readback.Disposition == nil || len(readback.Receipts) != 1 {
		t.Fatalf("missing terminal attempt/artifact/receipt/disposition: %+v", readback)
	}
	if readback.Receipts[0].Outcome != evidence.Passed || readback.Receipts[0].ArtifactID != readback.Artifact.ID || readback.Receipts[0].VerifierID == "" || readback.Artifact.Digest == "" {
		t.Fatalf("selected-file verification unbound: %+v", readback)
	}
	// Compare full immutable references, not just nonempty IDs/digests.
	if !reflect.DeepEqual(readback, *result.Execution) || len(readback.LoopExecutions) != 1 {
		t.Fatal("independent readback changed execution or child cardinality")
	}
	graphView := graphs[0]
	var g graph.GraphRevision
	if err := cli.run(&g, "graphs", "show", result.Graph.ID, strconv.FormatUint(result.Graph.Revision, 10)); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g, graphView.Revision) {
		t.Fatal("public Graph readback differs from owning service")
	}
	if g.GraphID != result.Graph.ID || g.Revision != result.Graph.Revision || g.Digest != result.Graph.Digest || len(g.Nodes) != 1 || g.Nodes[0].Participant != input.Agent || g.Nodes[0].Loop != input.Loop || len(graphView.Runs) != 1 {
		t.Fatalf("Graph does not pin requested immutable definitions: %+v", graphView)
	}
	run := graphView.Runs[0]
	if run.Snapshot.Graph != result.Graph || !reflect.DeepEqual(run.Snapshot.Participants, []reference.RevisionRef{input.Agent}) || !reflect.DeepEqual(run.Snapshot.Loops, []reference.RevisionRef{input.Loop}) || !reflect.DeepEqual(run.QueueItem, readback.Item) || run.Submission != readback.Submission || run.GraphRun != readback.GraphRun {
		t.Fatal("accepted Graph snapshot/submission lineage differs from Queue")
	}
	itemRef := reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: readback.Item.ItemID, Digest: readback.Item.Digest}
	snapshotRef := reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: run.Snapshot.SnapshotID, Digest: run.Snapshot.Digest}
	submissionRef := reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: readback.Submission.SubmissionID, Digest: readback.Submission.Digest}
	child, attempt, claim := readback.LoopExecutions[0], readback.Attempts[0], readback.Claims[0]
	if readback.Item.ItemID != result.QueueItemID || readback.Item.Submission != submissionRef || readback.Item.Snapshot != snapshotRef || readback.Submission.Snapshot != snapshotRef || readback.GraphRun.Snapshot != snapshotRef || readback.GraphRun.QueueItem != itemRef || readback.Item.GraphRunID != readback.GraphRun.GraphRunID || child.GraphRunID != readback.GraphRun.GraphRunID || child.GraphNodeID != g.Nodes[0].ID || child.Participant != input.Agent || child.Loop != input.Loop || attempt.GraphRunID != child.GraphRunID || attempt.LoopExecutionID != child.LoopExecutionID || attempt.QueueItem != itemRef || attempt.ClaimID != claim.ClaimID || claim.QueueItem != itemRef || claim.AttemptID != attempt.AttemptID {
		t.Fatal("Queue/Graph/Loop/attempt/claim exact lineage mismatch")
	}
	artifact, verification, terminal := readback.Artifact, readback.Receipts[0], readback.Disposition
	if artifact.Validate() != nil || verification.Validate() != nil || artifact.AttemptID != attempt.AttemptID || artifact.OwnerID != input.Agent.ID || artifact.ActionID != "verify" || artifact.RunID != child.LoopExecutionID || verification.AttemptID != attempt.AttemptID || verification.RunID != artifact.RunID || verification.OwnerID != artifact.OwnerID || verification.ActionID != artifact.ActionID || verification.AuthorityContextID != artifact.AuthorityContextID || verification.AuthorityContextDigest != artifact.AuthorityContextDigest || terminal.State != execution.StateSucceeded || terminal.GraphRunID != child.GraphRunID || terminal.LoopExecutionID != child.LoopExecutionID || terminal.AttemptID != attempt.AttemptID || terminal.QueueItem != itemRef || !reflect.DeepEqual(terminal.ArtifactIDs, []string{artifact.ID}) || !reflect.DeepEqual(terminal.ReceiptIDs, []string{verification.ID}) {
		t.Fatal("artifact/verification/disposition exact lineage mismatch")
	}
	if claim.Authority != terminal.Authority || terminal.Authority.ID != artifact.AuthorityContextID || terminal.Authority.Digest != artifact.AuthorityContextDigest || terminal.Authority.Validate() != nil {
		t.Fatal("terminal evidence is not bound to exact claimed authority")
	}
	steps := readback.DoerSteps[readback.Attempts[0].AttemptID]
	verifiedStep := false
	for _, step := range steps {
		verifiedStep = verifiedStep || step.StepID == "verify"
	}
	if !verifiedStep {
		t.Fatalf("independent Queue readback omitted verified cursor: %+v", steps)
	}
	bytes, err := os.ReadFile(filepath.Join(workspace, "result.txt"))
	if err != nil || string(bytes) != text {
		t.Fatalf("selected file: %q %v", bytes, err)
	}
	replayed, err := cli.queue(input)
	if err != nil || replayed.QueueItemID != result.QueueItemID || replayed.RequestID != result.RequestID || replayed.Execution == nil || len(replayed.Execution.Attempts) != 1 {
		t.Fatalf("same-key replay duplicated execution: %+v %v", replayed, err)
	}
	var afterReplay app.QueueExecutionView
	if err := cli.run(&afterReplay, "queue", "show", result.QueueItemID); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(afterReplay, readback) || replayed.Graph != result.Graph || !reflect.DeepEqual(*replayed.Execution, readback) {
		t.Fatal("fresh replay readback changed terminal lineage")
	}
}
