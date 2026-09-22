package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	"github.com/berryhill/aegis/internal/testprocess"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestExactLoopQueueImplementationSuccessReplay(t *testing.T) {
	for _, mode := range []string{"auto", "legacy", "ready", "ready-legacy", "failed-launch", "interrupted"} {
		t.Run(mode, func(t *testing.T) { exactLoopQueueImplementationSuccessReplay(t, mode) })
	}
}
func exactLoopQueueImplementationSuccessReplay(t *testing.T, mode string) {
	ready := mode == "ready" || mode == "ready-legacy"
	svc := apiService(t)
	store := configureAPIFleet(t, svc)
	if mode == "console" {
		configurePreparationHTTP(t, svc)
	}
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
	var consoleClient *http.Client
	if mode == "console" {
		waitFor(t, "tcp", svc.Config.API.Listen)
		consoleClient, _ = loginConsole(t, svc.Config.API.Listen)
		assertPreparationHTTP(t, consoleClient, svc.Config.API.Console.Origin, "", "")
	}
	subject, err := svc.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}

	// Use the existing API execution fixture's exact principal selector, with
	// fixture-only provider configuration and no newly invented authority.
	charter := core.Charter{SchemaVersion: core.SchemaVersion, AgentID: "distributed-agent", Name: "Distributed test", Revision: 1,
		Runtime: core.RuntimeConstraint{Adapter: "hermes", Runtime: "hermes-agent", VersionConstraint: ">=0.18.0", Target: "aegis-owned-ephemeral"},
		Stanzas: []core.TrustStanza{{ID: "principal", Name: "Principal", Enabled: true,
			Authentication: core.AuthenticationPolicy{Methods: []string{"local-os"}, Selectors: []core.IdentitySelector{{SubjectIDs: []string{"local-uid:" + strconv.Itoa(os.Getuid())}, PrincipalIDs: []string{svc.Config.Principal.ID}, Issuers: []string{"linux-so-peercred"}, Environments: []string{"local"}}}, RequireFresh: true, MaxAuthAgeSec: 60},
			Grant:          core.Grant{Capabilities: []string{"chat"}, Tools: nil}, Scopes: core.Scopes{Memory: []string{"principal-memory"}, Credentials: nil},
			Session: core.SessionPolicy{MaximumLifetimeSec: 60, RequireReauth: true}, Approval: core.ApprovalPolicy{RequiredOperations: []string{"provision"}, MaximumLifetimeSec: 60, SingleUse: true}, InformationFlow: core.InformationFlowPolicy{CrossStanza: "deny"},
			Hermes: core.HermesConfig{Toolsets: nil, Model: "proof-no-key", Provider: "none"}}}, CreatedBy: svc.Config.Principal.ID, CreatedAt: svc.Now()}
	charterWire, _ := json.Marshal(charter)
	canonical, err := svc.ImportCharterAs(ctx, subject, charterWire)
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := json.Marshal(registry.CurrentFleetFixture{SchemaVersion: registry.CurrentFleetFixtureSchemaVersion, FleetID: "distributed-fleet", Agents: []registry.CurrentFleetAgent{{SourceID: "distributed-source", AgentID: charter.AgentID,
		Runtime: registry.RuntimeBinding{Adapter: charter.Runtime.Adapter, Runtime: charter.Runtime.Runtime, Target: charter.Runtime.Target}, Ownership: registry.Ownership{OwnerID: "distributed-owner", AccountabilityID: "distributed-team"}, Lifecycle: registry.LifecycleEnabled,
		Charter: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: charter.AgentID, Revision: 1, Digest: canonical.Digest}}}})
	if err != nil {
		t.Fatal(err)
	}
	var registered struct {
		Agent   app.FleetAgent `json:"agent"`
		Created bool           `json:"created"`
	}
	apiRequest(t, client, http.MethodPost, "/v1/agents", app.NewRegisterFleetAgentInput(fixture, "distributed-fleet", "distributed-source"), &registered, http.StatusCreated)
	agent := registered.Agent.Revision
	root := t.TempDir()
	workspace := filepath.Join(root, "source")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"go.mod": "module hello\n\ngo 1.25\n", "hello.go": "package hello\nfunc Hello() string { return \"wrong\" }\n", "hello_test.go": "package hello\nimport \"testing\"\nfunc TestHello(t *testing.T) { if Hello()!=\"hello\" {t.Fatal(Hello())} }\n"} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	contract := loop.ImplementationDraft("Return hello", "TestHello passes")
	contract.Workspace = workspace
	contract.WritableFiles = []string{"hello.go"}
	contract.Policy.Packages = []string{"."}
	contract.Policy.RequiredTests = []loop.RequiredGoTest{{Package: "hello", Name: "TestHello"}}
	lrImpl, _, err := loop.NewImplementationRevision("exact-implementation", 1, "", contract)
	if err != nil {
		t.Fatal(err)
	}
	var published app.PublishedLoop
	apiRequest(t, client, http.MethodPost, "/v1/loops", app.PublishLoopInput{AgentID: agent.AgentID, Revision: lrImpl, IdempotencyKey: "distributed-loop"}, &published, http.StatusCreated)
	lr := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: published.Revision.LoopID, Revision: published.Revision.Revision, Digest: published.Revision.Digest}
	input := app.QueueLoopInput{Activate: true, Agent: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: agent.AgentID, Revision: agent.Revision, Digest: agent.Digest}, Loop: lr, IdempotencyKey: "exact-loop-test", Inputs: []graph.NormalizedInput{}}
	blockedInput := input
	blockedInput.Activate = false
	blockedInput.IdempotencyKey = "inactive-without-intent"
	blockedInput.Inputs = []graph.NormalizedInput{{PortID: "b", Type: graph.TypeString, Value: json.RawMessage(`"second"`)}, {PortID: "a", Type: graph.TypeObject, Value: json.RawMessage(`{"b":2,"a":1}`)}}
	blocked, err := svc.QueueLoopAs(ctx, subject, blockedInput)
	if err != nil || blocked.Rejection == nil || blocked.QueueItemID != "" {
		t.Fatalf("inactive intent not durably rejected: %+v %v", blocked, err)
	}
	if _, err := store.GetRejection(ctx, blocked.Rejection.RejectionID); err != nil {
		t.Fatal(err)
	}
	blockedInput.Inputs[0], blockedInput.Inputs[1] = blockedInput.Inputs[1], blockedInput.Inputs[0]
	blockedInput.Inputs[0].Value = json.RawMessage(`{ "a": 1, "b": 2 }`)
	reordered, err := svc.QueueLoopAs(ctx, subject, blockedInput)
	if err != nil || reordered.Rejection == nil || reordered.Rejection.Digest != blocked.Rejection.Digest {
		t.Fatalf("canonical replay: %+v %v", reordered, err)
	}
	blockedInput.Inputs[0].Value = json.RawMessage(`{"a":3,"b":2}`)
	if _, err := svc.QueueLoopAs(ctx, subject, blockedInput); err == nil {
		t.Fatal("changed request replay accepted")
	}
	queueRequest := func(in app.QueueLoopInput) (app.QueueLoopResult, error) { return svc.QueueLoopAs(ctx, subject, in) }
	var online *installedLoopQueueClient
	if mode == "online" {
		online = newInstalledLoopQueueClient(t, svc)
		queueRequest = online.queue
	}
	var got app.QueueLoopResult
	apiRequest(t, client, http.MethodPost, "/v1/loops/queue", input, &got, http.StatusOK)
	if got.Reason != "provisioning_receipt_missing" || got.Execution == nil || got.Execution.Projection.State != queue.StatePreparationPending || len(got.Execution.Attempts) != 0 {
		t.Fatalf("unexpected result: %+v", got)
	}
	storedPreparation, err := svc.GetQueueItemAs(ctx, subject, got.QueueItemID)
	if err != nil {
		t.Fatal(err)
	}
	foundReason := false
	for _, tr := range storedPreparation.Transitions {
		if tr.Reason == "provisioning_receipt_missing" {
			foundReason = true
		}
	}
	if !foundReason || len(storedPreparation.Claims) != 0 {
		t.Fatal("missing durable diagnostic or unexpected claim")
	}
	var replay app.QueueLoopResult
	apiRequest(t, client, http.MethodPost, "/v1/loops/queue", input, &replay, http.StatusOK)
	if replay.QueueItemID != got.QueueItemID {
		t.Fatal("duplicate execution")
	}
	if mode == "console" {
		assertPreparationHTTP(t, consoleClient, svc.Config.API.Console.Origin, got.QueueItemID, string(queue.StatePreparationPending))
		_, err := svc.CancelQueueItemAs(ctx, subject, app.TerminalQueueItemInput{WorkspaceAgentID: agent.AgentID, QueueItemID: got.QueueItemID, CancellationID: "console-cancel", TransitionID: "console-cancelled", ReasonCode: orchestration.ReasonOperatorCancelled})
		if err != nil {
			t.Fatal(err)
		}
		assertPreparationHTTP(t, consoleClient, svc.Config.API.Console.Origin, got.QueueItemID, string(queue.StateCancelled))
		return
	}
	// All authority and runtime state below is isolated test custody. The gateway
	// proposes a patch; the real native checker independently executes TestHello.
	var review core.Review
	apiRequest(t, client, http.MethodPost, "/v1/plans/preview", map[string]any{"agent": charter.AgentID, "revision": 1, "environment": core.Environment{Name: "local"}}, &review, http.StatusCreated)
	var approval core.Approval
	apiRequest(t, client, http.MethodPost, "/v1/approvals", map[string]any{"plan_id": review.Plan.ID, "ttl": "1m"}, &approval, http.StatusCreated)
	apiRequest(t, client, http.MethodPost, "/v1/approvals/"+approval.ID+"/decision", map[string]bool{"approve": true}, &approval, http.StatusOK)
	var receipt core.Receipt
	apiRequest(t, client, http.MethodPost, "/v1/provision", map[string]string{"plan_id": review.Plan.ID, "approval_id": approval.ID}, &receipt, http.StatusCreated)
	noSession, err := svc.QueueLoopAs(ctx, subject, input)
	if err != nil || noSession.Reason != "implementation_prerequisite_required" || len(noSession.Execution.Claims) != 0 {
		t.Fatalf("no session: %+v %v", noSession, err)
	}
	if ready {
		// Preserve the discoverable zero-tool probe for this provider:none session.
		installation := filepath.Join(filepath.Dir(svc.Config.HermesExecutable), "hermes-install")
		if err := os.WriteFile(svc.Config.HermesExecutable, []byte("#!/bin/sh\nif [ \"${1:-}\" = \"--version\" ]; then echo 'Hermes Agent v0.18.2'; echo 'Install directory: "+installation+"'; exit 0; fi\nsleep 60 &\nwait\n"), 0700); err != nil {
			t.Fatal(err)
		}
		var preview struct {
			Mandate core.Mandate `json:"mandate"`
		}
		apiRequest(t, client, http.MethodPost, "/v1/sessions/preview", map[string]any{"agent": charter.AgentID, "revision": 1, "stanza": "principal", "environment": core.Environment{Name: "local"}}, &preview, http.StatusCreated)
		session, err := svc.StartSessionAs(ctx, subject, preview.Mandate.ID)
		if err != nil {
			t.Fatal(err)
		}
		defer svc.TerminateSessionAs(ctx, subject, session.ID, "fixture complete")
	}
	install := filepath.Join(root, "install")
	if err = os.MkdirAll(filepath.Join(install, "venv", "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(svc.Config.HermesExecutable, []byte("#!/bin/sh\nif [ \"${1:-}\" = \"--version\" ]; then echo 'Hermes Agent v0.18.2'; echo 'Install directory: "+install+"'; exit 0; fi\nsleep 60 &\nwait\n"), 0700); err != nil {
		t.Fatal(err)
	}
	edits, _ := json.Marshal(map[string]any{"edits": []implementation.Edit{{Path: "hello.go", Content: []byte("package hello\nfunc Hello() string {return \"hello\"}\n")}}})
	event, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "event", "params": map[string]any{"type": "message.complete", "session_id": "fixture", "payload": map[string]string{"status": "complete", "text": string(edits)}}})
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready","payload":{}}}'
[ "$HERMES_TUI_TOOLSETS" = "context_engine" ] || exit 90
IFS= read -r tools || exit 1
case "$tools" in *'"method":"tools.show"'*) ;; *) exit 91;; esac
printf '%%s\n' '{"jsonrpc":"2.0","id":"aegis-tools","result":{"total":0,"sections":[]}}'
IFS= read -r create || exit 0
printf '%%s\n' '{"jsonrpc":"2.0","id":"create","result":{"session_id":"fixture"}}'
read prompt
touch '%s'
while [ ! -f '%s' ]; do sleep 0.05; done
printf '%%s\n' '{"jsonrpc":"2.0","id":"prompt","result":{"accepted":true}}'
printf '%%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.start","session_id":"fixture","payload":{}}}'
printf '%%s\n' '%s'
while read rest; do :; done
`, filepath.Join(root, "entered"), filepath.Join(root, "release"), string(event))
	if err = os.WriteFile(filepath.Join(install, "venv", "bin", "python"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
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
	goBinary, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	goBinary, err = filepath.EvalSymlinks(goBinary)
	if err != nil {
		t.Fatal(err)
	}
	digest, _ := contract.Digest()
	if err = worker.ConfigureImplementation(config.Implementation{GoBinary: goBinary, AuthorizedContracts: []string{digest}}, svc.Config.StateDir, svc.Hermes); err != nil {
		t.Fatal(err)
	}
	if err = svc.ConfigureFleet(store, svc.Fleet, worker); err != nil {
		t.Fatal(err)
	}
	var originalBinding queue.RuntimeBinding
	if ready {
		authority, e := svc.FleetCommandAuthorityAs(ctx, subject)
		if e != nil {
			t.Fatal(e)
		}
		key := strings.TrimSuffix(got.QueueItemID, "-queue")
		if _, _, e = svc.BindQueueRuntimeAs(ctx, subject, app.BindQueueRuntimeInput{AgentID: input.Agent.ID, Authority: authority.Authority, QueueItemID: got.QueueItemID, BindingID: key + "-binding", TransitionID: key + "-ready"}); e != nil {
			t.Fatal(e)
		}
		originalBinding, e = store.GetQueueRuntimeBinding(ctx, got.QueueItemID)
		if e != nil {
			t.Fatal(e)
		}
	}
	if mode == "legacy" || mode == "ready-legacy" {
		// Recover an explicitly selected parked ID with a different caller key:
		// no new graph, submission, snapshot, or authority history is minted.
		input.QueueItemID = got.QueueItemID
		input.IdempotencyKey = "explicit-legacy-recovery"
		bad := input
		bad.Inputs = []graph.NormalizedInput{{PortID: "unexpected", Type: graph.TypeString, Value: json.RawMessage(`"x"`)}}
		if _, e := svc.QueueLoopAs(ctx, subject, bad); e == nil {
			t.Fatal("mismatched legacy inputs accepted")
		}
	}
	if mode == "failed-launch" {
		// Fail inside Launch, after authority activation, without starting a process.
		runtimeRoot := filepath.Join(svc.Store.Root(), "runtime")
		if e := os.WriteFile(runtimeRoot, []byte("block runtime directory"), 0600); e != nil {
			t.Fatal(e)
		}
		failed, e := svc.QueueLoopAs(ctx, subject, input)
		if e != nil || failed.Reason != "session_start_preparation_failed" {
			t.Fatalf("launch failure: %+v %v", failed, e)
		}
		if e := os.Remove(runtimeRoot); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(filepath.Join(root, "release"), nil, 0600); e != nil {
			t.Fatal(e)
		}
		for i := 0; i < 2; i++ {
			blocked, e := svc.QueueLoopAs(ctx, subject, input)
			if e != nil || blocked.Reason != "session_preparation_in_progress_or_interrupted" || blocked.Execution == nil || len(blocked.Execution.Claims) != 0 {
				t.Fatalf("failed launch bypassed fence: %+v %v", blocked, e)
			}
			if _, e := store.GetQueueRuntimeBinding(ctx, got.QueueItemID); e == nil {
				t.Fatal("failed launch bound runtime")
			}
		}
		return
	}
	if mode == "interrupted" {
		reservation := sha256.Sum256([]byte(subject.ID + "\x00" + agent.Digest))
		if e := svc.Store.Create("queue-session-preparation", hex.EncodeToString(reservation[:]), map[string]string{"queue_item_id": got.QueueItemID}); e != nil {
			t.Fatal(e)
		}
		for i := 0; i < 2; i++ {
			blocked, e := svc.QueueLoopAs(ctx, subject, input)
			if e != nil || blocked.Reason != "session_preparation_in_progress_or_interrupted" || len(blocked.Execution.Claims) != 0 {
				t.Fatalf("interrupted preparation: %+v %v", blocked, e)
			}
		}
		mandates, e := svc.Authority.ListMandates(ctx)
		if e != nil || len(mandates) != 0 {
			t.Fatalf("interrupted preparation minted authority: %d %v", len(mandates), e)
		}
		return
	}
	originalItem := storedPreparation.Item
	originalSubmission := storedPreparation.Submission
	finished := make(chan struct{})
	go func() { got, err = queueRequest(input); close(finished) }()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, e := os.Stat(filepath.Join(root, "entered")); e == nil {
			break
		}
		select {
		case <-finished:
			t.Fatalf("execution ended before blocked runtime: %+v %v", got, err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("runtime did not reach blocking fixture")
		}
		time.Sleep(10 * time.Millisecond)
	}
	running, replayErr := queueRequest(input)
	if replayErr != nil || running.Execution == nil || len(running.Execution.Claims) != 1 || len(running.Execution.Attempts) != 1 || running.Reason != "existing_execution" {
		t.Fatalf("running replay: %+v %v", running, replayErr)
	}
	if err := os.WriteFile(filepath.Join(root, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	<-finished
	if got.Execution != nil && (got.Execution.Item.Digest != originalItem.Digest || got.Execution.Submission.Digest != originalSubmission.Digest) {
		t.Fatal("recovery changed immutable history")
	}
	if !ready {
		sessions, e := svc.ListSessions()
		if e != nil || len(sessions) != 1 {
			t.Fatalf("automatic session count: %d %v", len(sessions), e)
		}
		defer svc.TerminateSessionAs(ctx, subject, sessions[0].ID, "fixture complete")
	}
	if err != nil || got.Execution == nil || got.Execution.Projection.State != queue.StateSucceeded || got.Execution.Disposition == nil || got.Execution.Artifact == nil {
		t.Fatalf("success: %+v %v", got, err)
	}
	if ready {
		bound, e := store.GetQueueRuntimeBinding(ctx, got.QueueItemID)
		if e != nil || bound.Digest != originalBinding.Digest || bound.BindingID != originalBinding.BindingID {
			t.Fatalf("recovery changed binding: %+v %v", bound, e)
		}
	}
	lv, err := svc.GetLoopViewAs(ctx, subject, lr.ID, lr.Revision)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.SetLoopLifecycleAs(ctx, subject, lr.ID, app.SetLoopLifecycleInput{AgentID: agent.AgentID, Loop: lr, State: loop.LifecycleRetired, EventID: "retire-after-success", ExpectedPreviousDigest: lv.History[len(lv.History)-1].Digest})
	if err != nil {
		t.Fatal(err)
	}
	replay, err = queueRequest(input)
	if err != nil || replay.Execution == nil || len(replay.Execution.Attempts) != 1 || len(replay.Execution.Claims) != 1 || replay.Execution.Disposition.Digest != got.Execution.Disposition.Digest {
		t.Fatalf("replay: %+v %v", replay, err)
	}
	if online != nil {
		online.assertReadback(t, input, got)
	}
	if mode == "auto" {
		expected := input.Agent
		for _, state := range []registry.Lifecycle{registry.LifecycleDisabled, registry.LifecycleEnabled} {
			next, e := svc.SetAgentLifecycleAs(ctx, subject, agent.AgentID, app.SetAgentLifecycleInput{Expected: expected, Lifecycle: state})
			if e != nil {
				t.Fatal(e)
			}
			expected.Revision, expected.Digest = next.Revision.Revision, next.Revision.Digest
			replay, e := svc.QueueLoopAs(ctx, subject, input)
			if e != nil || replay.Execution == nil || replay.Execution.Disposition.Digest != got.Execution.Disposition.Digest || len(replay.Execution.Claims) != 1 || len(replay.Execution.Attempts) != 1 {
				t.Fatalf("historical replay after %s: %+v %v", state, replay, e)
			}
			changed := input
			changed.Activate = !input.Activate
			if _, e := svc.QueueLoopAs(ctx, subject, changed); !errors.Is(e, fleet.ErrConflict) {
				t.Fatalf("changed historical payload: %v", e)
			}
			unauthorized := subject
			unauthorized.PrincipalID = "other-principal"
			if _, e := svc.QueueLoopAs(ctx, unauthorized, input); e == nil {
				t.Fatal("unauthorized historical read")
			}
		}
	}
	check := exec.Command(goBinary, "test", "-count=1", "-run", "^TestHello$", ".")
	check.Dir = workspace
	if output, err := testprocess.CombinedOutput(check, 2*time.Minute); err != nil {
		t.Fatalf("independent TestHello: %v %s", err, output)
	}

}
