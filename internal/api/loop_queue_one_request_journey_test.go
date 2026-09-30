package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	consoleweb "github.com/berryhill/aegis/web/console"
)

func TestQueueConsoleDoerRetryUsesAuthoritativeCursorWithoutUpgradingFailure(t *testing.T) {
	view := app.QueueExecutionView{
		Item:       queue.Item{ItemID: "doer-queue"},
		Projection: queue.Projection{State: queue.StateFailed},
		Attempts:   []execution.Attempt{{AttemptID: "later-attempt", AttemptNumber: 2}, {AttemptID: "doer-attempt", AttemptNumber: 1}},
		DoerSteps: map[string][]orchestration.DoerStepView{"doer-attempt": {
			{Sequence: 0, StepID: "eligibility", Digest: "sha256:gate"},
			{Sequence: 1, StepID: "verify", Digest: "sha256:first-check", Visit: 1},
			{Sequence: 2, StepID: "diagnosis", Digest: "sha256:diagnosis", Visit: 1},
			{Sequence: 3, StepID: "implement", Digest: "sha256:retry", Visit: 2},
			{Sequence: 4, StepID: "verify", Digest: "sha256:second-check", Visit: 2},
		}, "later-attempt": {{Sequence: 0, StepID: "verify", Digest: "sha256:later-check"}}},
	}
	record := consoleQueueRecord(view)
	if record.Lifecycle != "failed" || record.Queue == nil || len(record.Queue.DoerSteps) != 6 || record.Queue.DoerSteps[0].AttemptNumber != 1 || record.Queue.DoerSteps[5].AttemptNumber != 2 {
		t.Fatalf("cursor or authoritative Queue state missing: %+v", record)
	}
	var ids []string
	for _, step := range record.Queue.DoerSteps {
		if step.Digest == "" || (step.AttemptNumber == 1 && step.AttemptID != "doer-attempt") || (step.AttemptNumber == 2 && step.AttemptID != "later-attempt") {
			t.Fatalf("unbound cursor: %+v", step)
		}
		ids = append(ids, step.StepID)
	}
	if strings.Join(ids, ",") != "eligibility,verify,diagnosis,implement,verify,verify" {
		t.Fatalf("retry path not in canonical cursor order: %v", ids)
	}
	html, err := renderConsole(context.Background(), consoleweb.QueueWorkspace(consoleweb.SurfaceModel{Domain: "queue"}, &record, consoleweb.QueuePinnedTopology{}))
	if err != nil {
		t.Fatal(err)
	}
	text := string(html)
	for _, step := range []string{"verify", "diagnosis", "implement"} {
		if !strings.Contains(text, `data-doer-step="`+step+`"`) {
			t.Fatalf("cursor step %s absent from Console", step)
		}
	}
	if strings.Index(text, `data-doer-step="diagnosis"`) > strings.Index(text, `data-doer-step="implement"`) || strings.Contains(text, "Authoritative terminal disposition · succeeded") {
		t.Fatal("Console fabricated a successful outcome or reordered retry")
	}
}

// This isolated controller fixture is not approval or execution against an
// installed Agent. The unusable and approved paths have independent stores.
func TestLoopQueueOneRequestApprovalResume(t *testing.T) {
	for _, usable := range []bool{false, true} {
		name := "unusable-model"
		if usable {
			name = "independently-approved-usable-model"
		}
		t.Run(name, func(t *testing.T) { loopQueueOneRequestJourney(t, usable) })
	}
}

func loopQueueOneRequestJourney(t *testing.T, usable bool) {
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
	model := "none"
	if usable {
		model = "proof-no-key"
	}
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
	apiRequest(t, client, http.MethodPost, "/v1/agents", app.NewRegisterFleetAgentInput(fixture, "journey-fleet", "journey-source"), &registered, http.StatusCreated)
	workspace := t.TempDir()
	text := "hello from fixture"
	contract := loop.DoerContract{Task: "Create result.txt", Workspace: workspace, WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", ExpectedText: &text, MaxAttempts: 1}
	revision, _, err := loop.NewDoerRevision("journey-doer", 1, "", contract)
	if err != nil {
		t.Fatal(err)
	}
	published, err := svc.PublishLoopAs(ctx, subject, app.PublishLoopInput{AgentID: charter.AgentID, Revision: revision, IdempotencyKey: "journey-publish"})
	if err != nil {
		t.Fatal(err)
	}
	agent := registered.Agent.Revision
	input := app.QueueLoopInput{Agent: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: agent.AgentID, Revision: agent.Revision, Digest: agent.Digest},
		Loop:           reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: revision.LoopID, Revision: 1, Digest: published.Revision.Digest},
		IdempotencyKey: "journey-request", Activate: true}
	if !usable {
		var first, replay app.QueueLoopResult
		apiRequest(t, client, http.MethodPost, "/v1/loops/queue", input, &first, http.StatusOK)
		apiRequest(t, client, http.MethodPost, "/v1/loops/queue", input, &replay, http.StatusOK)
		if first.RequestID == "" || first.RequestID != replay.RequestID || first.RequiredAction != "review_charter_successor_with_usable_model" || first.Reason != "doer_model_required" || first.RequiredCharter == nil || first.RequiredCharter.Digest != canonical.Digest || first.QueueItemID != "" || first.Execution != nil {
			t.Fatalf("blocked exact request was not stable: first=%+v replay=%+v", first, replay)
		}
		var sealed string
		if err := svc.Store.Load("queue-loop-intent", first.RequestID, &sealed); err != nil || len(sealed) != 64 {
			t.Fatalf("blocked request not sealed: %q %v", sealed, err)
		}
		changed := input
		changed.Activate = false
		if _, err := svc.QueueLoopAs(ctx, subject, changed); !errors.Is(err, fleet.ErrConflict) {
			t.Fatalf("changed-payload same-key replay: %v", err)
		}
		view, err := svc.GetLoopViewAs(ctx, subject, revision.LoopID, 1)
		if err != nil || view.Lifecycle.State != loop.LifecycleDraft || len(view.History) != 0 {
			t.Fatalf("blocked request activated Loop: %+v %v", view.Lifecycle, err)
		}
		graphs, err := svc.ListGraphsAs(ctx, subject)
		if err != nil || len(graphs) != 0 {
			t.Fatalf("blocked request published Graph: %d %v", len(graphs), err)
		}
		items, err := svc.ListQueueAs(ctx, subject)
		if err != nil || len(items) != 0 {
			t.Fatalf("blocked request submitted Queue work: %d %v", len(items), err)
		}
		if _, err := os.Stat(filepath.Join(workspace, "result.txt")); !os.IsNotExist(err) {
			t.Fatalf("blocked request wrote file: %v", err)
		}
		return
	}
	var pending app.QueueLoopResult
	apiRequest(t, client, http.MethodPost, "/v1/loops/queue", input, &pending, http.StatusOK)
	if pending.Reason != "provisioning_receipt_missing" || pending.RequiredAction != "plan_preview_exact_charter" || pending.RequiredCharter == nil || pending.RequiredCharter.Digest != canonical.Digest || pending.RequestID == "" || pending.QueueItemID != "" || pending.Execution != nil {
		t.Fatalf("missing receipt advanced execution: %+v", pending)
	}
	if view, err := svc.GetLoopViewAs(ctx, subject, revision.LoopID, 1); err != nil || view.Lifecycle.State != loop.LifecycleDraft || len(view.History) != 0 {
		t.Fatalf("missing receipt activated Loop: %+v %v", view.Lifecycle, err)
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
	apiRequest(t, client, http.MethodPost, "/v1/loops/queue", input, &result, http.StatusOK)
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
	readback, err := svc.GetQueueItemAs(ctx, subject, result.QueueItemID)
	if err != nil {
		t.Fatal(err)
	}
	if readback.Projection.State != queue.StateSucceeded || len(readback.Claims) != 1 || len(readback.Attempts) != 1 || readback.Attempts[0].AttemptNumber != 1 || readback.Artifact == nil || readback.Disposition == nil || len(readback.Receipts) != 1 {
		t.Fatalf("missing terminal attempt/artifact/receipt/disposition: %+v", readback)
	}
	if readback.Receipts[0].Outcome != evidence.Passed || readback.Receipts[0].ArtifactID != readback.Artifact.ID || readback.Receipts[0].VerifierID == "" || readback.Artifact.Digest == "" {
		t.Fatalf("selected-file verification unbound: %+v", readback)
	}
	steps := readback.DoerSteps[readback.Attempts[0].AttemptID]
	verifiedStep := false
	for _, step := range steps {
		verifiedStep = verifiedStep || step.StepID == "verify"
	}
	if !verifiedStep {
		t.Fatalf("independent Queue readback omitted verified cursor: %+v", steps)
	}
	queueRecord := consoleQueueRecord(readback)
	if queueRecord.Lifecycle != "succeeded" || queueRecord.Queue == nil || len(queueRecord.Queue.DoerSteps) != len(steps) {
		t.Fatalf("Console lost authoritative cursor or disposition: %+v", queueRecord)
	}
	html, err := renderConsole(context.Background(), consoleweb.QueueWorkspace(consoleweb.SurfaceModel{Domain: "queue"}, &queueRecord, consoleweb.QueuePinnedTopology{}))
	if err != nil || !strings.Contains(string(html), `data-doer-step="verify"`) {
		t.Fatalf("verified cursor not rendered in Queue detail: %v", err)
	}
	bytes, err := os.ReadFile(filepath.Join(workspace, "result.txt"))
	if err != nil || string(bytes) != text {
		t.Fatalf("selected file: %q %v", bytes, err)
	}
	replayed, err := svc.QueueLoopAs(ctx, subject, input)
	if err != nil || replayed.QueueItemID != result.QueueItemID || replayed.RequestID != result.RequestID || len(replayed.Execution.Attempts) != 1 {
		t.Fatalf("same-key replay duplicated execution: %+v %v", replayed, err)
	}
}
