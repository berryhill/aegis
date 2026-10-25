package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
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
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := probe.Addr().String()
	_ = probe.Close()
	svc.Config.API.Listen = address
	svc.Config.API.Console.Origin = "http://" + address
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
		Ownership: registry.Ownership{OwnerID: subject.PrincipalID, AccountabilityID: subject.PrincipalID}, Lifecycle: registry.LifecycleEnabled,
		Charter: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: charter.AgentID, Revision: 1, Digest: canonical.Digest}}}})
	if err != nil {
		t.Fatal(err)
	}
	var registered struct {
		Agent app.FleetAgent `json:"agent"`
	}
	apiRequest(t, client, http.MethodPost, "/v1/agents", app.NewRegisterFleetAgentInput(fixture, "journey-fleet", "journey-source"), &registered, http.StatusCreated)
	workspace := t.TempDir()
	text := "hello"
	contract := loop.DoerContract{Task: "Create result.txt", Workspace: workspace, WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", ExpectedText: &text, ExactBytes: true, MaxAttempts: 2}
	revision, _, err := loop.NewDoerRevision("journey-doer", 1, "", contract)
	if err != nil {
		t.Fatal(err)
	}
	agent := registered.Agent.Revision
	var published app.PublishedLoop
	var input app.QueueLoopInput
	if !usable {
		published, err = svc.PublishLoopAs(ctx, subject, app.PublishLoopInput{AgentID: charter.AgentID, Revision: revision, IdempotencyKey: "journey-publish"})
		if err != nil {
			t.Fatal(err)
		}
		agent := registered.Agent.Revision
		input = app.QueueLoopInput{Agent: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: agent.AgentID, Revision: agent.Revision, Digest: agent.Digest},
			Loop:           reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: revision.LoopID, Revision: 1, Digest: published.Revision.Digest},
			IdempotencyKey: "journey-request", Activate: true}
	}
	var retained app.ExecuteDoerTaskResult
	var resume app.ExecuteDoerTaskInput

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

	create := app.ExecuteDoerTaskInput{Create: &app.DoerDraftInput{CreationKey: "synthetic-task", Agent: app.RevisionReference(agent.AgentID, agent.Revision, agent.Digest), LoopID: revision.LoopID, Revision: 1, Contract: contract}}
	apiRequest(t, client, http.MethodPost, "/v1/loops/doer/tasks/execute", create, &retained, http.StatusOK)
	var repeated app.ExecuteDoerTaskResult
	apiRequest(t, client, http.MethodPost, "/v1/loops/doer/tasks/execute", create, &repeated, http.StatusOK)
	if retained.Task.ID == "" || retained.Task.ID != repeated.Task.ID || retained.ConfirmationURL != repeated.ConfirmationURL || retained.Readiness.Reason != "provisioning_receipt_missing" {
		t.Fatalf("block: %+v %+v", retained, repeated)
	}
	loops, e := svc.ListLoopsAs(ctx, subject)
	if e != nil || len(loops) != 0 {
		t.Fatal("blocked publication", e)
	}
	resume = app.ExecuteDoerTaskInput{ID: retained.Task.ID, ExpectedVersion: retained.Task.Version}
	stale := resume
	stale.ExpectedVersion++
	if _, e = svc.ExecuteDoerTaskAs(ctx, subject, stale); !errors.Is(e, app.ErrConflict) {
		t.Fatal("stale accepted", e)
	}
	foreign := subject
	foreign.PrincipalID = "foreign"
	if _, e = svc.ExecuteDoerTaskAs(ctx, foreign, resume); e == nil {
		t.Fatal("foreign accepted")
	}
	changed := *create.Create
	changed.Contract.Task += " changed"
	if _, e = svc.ExecuteDoerTaskAs(ctx, subject, app.ExecuteDoerTaskInput{Create: &changed}); !errors.Is(e, app.ErrConflict) {
		t.Fatal("changed accepted", e)
	}

	// Publish first, then explicitly approve an immediate same-Agent successor
	// in this isolated synthetic fixture. Historical creation stays untouched.
	priorPublication, err := svc.PublishLoopAs(ctx, subject, app.PublishLoopInput{AgentID: agent.AgentID, Revision: revision, IdempotencyKey: retained.Task.PublicationKey})
	if err != nil {
		t.Fatal(err)
	}
	priorView, err := svc.GetLoopViewAs(ctx, subject, revision.LoopID, revision.Revision)
	if err != nil {
		t.Fatal(err)
	}
	charter.Revision++
	wire, err = json.Marshal(charter)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err = svc.ImportCharterAs(ctx, subject, wire)
	if err != nil {
		t.Fatal(err)
	}
	next, err := svc.ApproveAgentCharterAs(ctx, subject, agent.AgentID, app.ApproveAgentCharterInput{Expected: retained.Task.Agent, Charter: app.RevisionReference(charter.AgentID, charter.Revision, canonical.Digest)})
	if err != nil {
		t.Fatal(err)
	}
	rebound, err := svc.ContinueDoerDraftSuccessorAs(ctx, subject, retained.Task.ID, retained.Task.Version, retained.Task.Agent, next.Revision.Charter)
	if err != nil {
		t.Fatal(err)
	}
	if rebound.LoopID != retained.Task.LoopID || rebound.PublicationKey != retained.Task.PublicationKey || rebound.Version != retained.Task.Version+1 {
		t.Fatal("successor replaced original identity")
	}
	retained.Task, agent = rebound, next.Revision
	resume.ExpectedVersion = rebound.Version
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
	if err := svc.QueueWorker.ValidateLoopAdmission(revision, agent); err != nil {
		t.Fatalf("Doer controller fixture prerequisites: %v", err)
	}

	// Real isolated password-authenticated Manager + setup handlers; independent
	// human-simulated decisions. Synthetic runtime only, not live provider proof.
	svc.Config.Credentials.Authority.DeploymentID = "synthetic-task-deployment"
	// Use the same production manager that owns service admission. A second
	// isolated manager must not authenticate identities for this live service.
	waitFor(t, "tcp", address)
	browser, csrf := loginConsole(t, address)
	post := func(action, receipt string) *httptest.ResponseRecorder {
		v := url.Values{"csrf": {csrf}, "draft_id": {retained.Task.ID}, "action": {action}}
		if receipt != "" {
			v.Set("receipt", receipt)
		}
		req, err := http.NewRequest(http.MethodPost, "http://"+address+"/console/loops/doer/setup", strings.NewReader(v.Encode()))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "http://"+address)
		response, err := browser.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		rr := httptest.NewRecorder()
		rr.Code = response.StatusCode
		if _, err := io.Copy(rr.Body, response.Body); err != nil {
			t.Fatal(err)
		}
		return rr
	}
	token := func(rr *httptest.ResponseRecorder) string {
		t.Helper()
		m := regexp.MustCompile(`name="receipt" value="([^"]+)"`).FindStringSubmatch(rr.Body.String())
		if rr.Code != 200 || len(m) != 2 {
			t.Fatalf("review: %d %s", rr.Code, rr.Body.String())
		}
		return html.UnescapeString(m[1])
	}
	review := token(post("preview", ""))
	decision := token(post("confirm", review))
	apply := token(post("confirm", decision))
	rr := post("confirm", apply)
	if !strings.Contains(rr.Body.String(), "Authoritative provisioning receipt:") {
		t.Fatalf("provision: %s", rr.Body.String())
	}
	host := token(post("host-review", ""))
	rr = post("confirm", host)
	if !strings.Contains(rr.Body.String(), "Authoritative signed host-write approval read back:") {
		t.Fatalf("host: %s", rr.Body.String())
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

	// Concurrent initial handler requests share publication and Run identities.
	var initialWG sync.WaitGroup
	initialErrors := make(chan error, 3)
	for i := 0; i < 3; i++ {
		initialWG.Add(1)
		go func() {
			defer initialWG.Done()
			raw, _ := json.Marshal(resume)
			req, _ := http.NewRequest(http.MethodPost, "http://unix/v1/loops/doer/tasks/execute", strings.NewReader(string(raw)))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer transport-secret")
			response, e := client.Do(req)
			if e != nil {
				initialErrors <- e
				return
			}
			defer response.Body.Close()
			var result app.ExecuteDoerTaskResult
			if response.StatusCode != 200 {
				initialErrors <- fmt.Errorf("concurrent status %d", response.StatusCode)
				return
			}
			if e = json.NewDecoder(response.Body).Decode(&result); e != nil {
				initialErrors <- e
				return
			}
			if result.Run == nil || result.Run.Execution == nil || result.Run.Execution.Projection.State != queue.StateSucceeded {
				initialErrors <- fmt.Errorf("concurrent execution: %+v", result)
			}
		}()
	}
	initialWG.Wait()
	close(initialErrors)
	for e := range initialErrors {
		t.Error(e)
	}
	var executed app.ExecuteDoerTaskResult
	apiRequest(t, client, http.MethodPost, "/v1/loops/doer/tasks/execute", resume, &executed, http.StatusOK)
	if executed.Publication == nil || executed.Run == nil || executed.Task.ID != retained.Task.ID || executed.Task.Version != retained.Task.Version || executed.Task.Agent != retained.Task.Agent || executed.Task.PublicationKey != retained.Task.PublicationKey {
		t.Fatalf("task: %+v", executed)
	}
	result := *executed.Run
	if result.Execution == nil || result.Execution.Projection.State != queue.StateSucceeded {
		t.Fatalf("execution: %+v", result)
	}

	view, err := svc.GetLoopViewAs(ctx, subject, revision.LoopID, 1)
	if err != nil || view.Lifecycle.State != loop.LifecycleActive || len(view.History) != 1 {
		t.Fatalf("activation: %+v %v", view.Lifecycle, err)
	}
	if view.Provenance.Digest != priorView.Provenance.Digest || executed.Publication.Revision.Digest != priorPublication.Revision.Digest || !executed.Publication.Decision.Idempotent {
		t.Fatal("task execution replaced historical publication")
	}
	graphs, err := svc.ListGraphsAs(ctx, subject)
	if err != nil || len(graphs) != 0 {
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
	if !readback.IsDirectLoop() || readback.Source.ID != revision.LoopID || readback.Source.Digest != revision.Digest {
		t.Fatal("execution did not retain the exact direct Loop source")
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

	var wg sync.WaitGroup
	failures := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := svc.ExecuteDoerTaskAs(ctx, subject, resume)
			if e != nil {
				failures <- e
				return
			}
			if r.Run == nil || r.Run.QueueItemID != result.QueueItemID || r.Run.Execution == nil || len(r.Run.Execution.Attempts) != 1 {
				failures <- fmt.Errorf("duplicate: %+v", r)
			}
		}()
	}
	wg.Wait()
	close(failures)
	for e := range failures {
		t.Error(e)
	}
	loops, err = svc.ListLoopsAs(ctx, subject)
	if err != nil || len(loops) != 1 {
		t.Fatal("duplicate publication", len(loops), err)
	}
	edit := *create.Create
	edit.CreationKey = ""
	edit.ID = retained.Task.ID
	edit.ExpectedVersion = retained.Task.Version
	edit.Contract.Task += " changed"
	if _, err = svc.SaveDoerDraftAs(ctx, subject, edit); !errors.Is(err, app.ErrConflict) {
		t.Fatal("executed task changed", err)
	}
}
