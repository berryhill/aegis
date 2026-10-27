package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"

	"github.com/berryhill/aegis/internal/execution"
	"strconv"
	"strings"
	"testing"
	"time"

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
	installedOnlineDoerExactBytesSuccess(t, false)
}

func TestInstalledOnlineReusableDoerDirectAdmission(t *testing.T) {
	installedOnlineDoerExactBytesSuccess(t, true, "reusable")
}

func TestIssue351InstalledControllerParticipantRunResult(t *testing.T) {
	installedOnlineDoerExactBytesSuccess(t, true, "reusable-controller")
}

func TestIssue351InstalledControllerHistoricalResults(t *testing.T) {
	for _, mode := range []string{"authority-expiry", "host-expiry", "revoked", "disabled", "retired"} {
		t.Run(mode, func(t *testing.T) {
			installedOnlineDoerExactBytesSuccess(t, true, "reusable-controller-history-"+mode)
		})
	}
}

func TestInstalledOnlineReusableDoerFailureContinuity(t *testing.T) {
	installedOnlineDoerExactBytesSuccess(t, true, "reusable-continuity")
}

func TestInstalledOnlineDoerPrincipalSingleRunWithoutProvisioning(t *testing.T) {
	installedOnlineDoerExactBytesSuccess(t, true)
}

func TestRetainedDoerExactRunParity(t *testing.T) {
	installedOnlineDoerExactBytesSuccess(t, true, "retained")
}

func TestRetainedDoerSlowHelperExecutionParity(t *testing.T) {
	t.Parallel()
	installedOnlineDoerExactBytesSuccess(t, true, "retained-slow")
}

func TestInstalledOnlineDoerSameQueuePreparationRecovery(t *testing.T) {
	installedOnlineDoerExactBytesSuccess(t, true, "failed-launch")
}

func TestInstalledOnlineDoerLegacyPreparationRecovery(t *testing.T) {
	installedOnlineDoerExactBytesSuccess(t, true, "legacy")
}

// Reproduce only the old issuer's nil-vs-empty bug in isolated fixture custody.
// The real service still validates the resulting immutable mandate normally.
type legacyEmptyMandateRepository struct {
	core.AuthorityRepository
	once bool
}

func (r *legacyEmptyMandateRepository) CreateMandate(ctx context.Context, m core.Mandate) error {
	if !r.once {
		m.Capabilities = nil
		m.Tools = nil
		r.once = true
	}
	return r.AuthorityRepository.CreateMandate(ctx, m)
}

func installedOnlineDoerExactBytesSuccess(t *testing.T, singleRun bool, recovery ...string) {
	slowHelper := len(recovery) > 0 && recovery[0] == "retained-slow"
	retainedTask := len(recovery) > 0 && (recovery[0] == "retained" || slowHelper)
	if retainedTask {
		recovery = nil
	}
	controllerParticipant := len(recovery) > 0 && strings.HasPrefix(recovery[0], "reusable-controller")
	controllerHistory := ""
	if controllerParticipant {
		controllerHistory = strings.TrimPrefix(recovery[0], "reusable-controller-history-")
	}
	continuity := len(recovery) > 0 && recovery[0] == "reusable-continuity"
	reusable := len(recovery) > 0 && strings.HasPrefix(recovery[0], "reusable")
	if reusable {
		recovery = nil
	}
	svc := apiService(t)
	var requeueClock *requeueStageClock
	if slowHelper || (controllerParticipant && controllerHistory != "reusable-controller") || len(recovery) > 0 && strings.HasPrefix(recovery[0], "linked-requeue") {
		// Install before Fleet, worker and Serve capture Now: every authority
		// consumer, including password/native authentication, shares this clock.
		requeueClock = newRequeueStageClock(svc.Now())
		svc.Now = requeueClock.Now
	}

	if len(recovery) > 0 && recovery[0] == "legacy" {
		svc.Authority = &legacyEmptyMandateRepository{AuthorityRepository: svc.Authority}
	}
	// Explicit fixture wiring suppresses optional host Laya auto-discovery.
	// ConfigureImplementation below installs the synthetic runtime only after
	// the prerequisite denial is checked; no live provider is exercised.
	svc.Config.Implementation.GoBinary = "/fixture/unconfigured-go"
	configureAPIFleet(t, svc)
	// Keep this synthetic fixture independent of an installed host Laya runtime.
	// The explicit controller has no decision helper until fixture setup below.
	if err := svc.QueueWorker.ConfigureImplementation(config.Implementation{GoBinary: svc.Config.HermesExecutable}, svc.Config.StateDir, svc.Hermes); err != nil {
		t.Fatal(err)
	}
	resultBrowser := reusable && os.Getenv("AEGIS_QUEUE_RESULT_BROWSER_TEST") == "1"
	requeueBrowser := len(recovery) > 0 && recovery[0] == "linked-requeue-console-mismatched-authentication" && os.Getenv("AEGIS_QUEUE_REQUEUE_BROWSER_TEST") == "1"
	// One fixture owns Serve and its transport lease. Browser helpers attach
	// to this listener; they must never start another server on these stores.
	if resultBrowser || requeueBrowser || len(recovery) > 0 && strings.HasPrefix(recovery[0], "linked-requeue-console") {
		probe, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		svc.Config.API.Listen = probe.Addr().String()
		_ = probe.Close()
		svc.Config.API.Console.Origin = "http://" + svc.Config.API.Listen
	}
	if len(recovery) > 0 && strings.HasPrefix(recovery[0], "linked-requeue-console") {
		svc.Config.API.Console.SessionTTL = 2 * time.Minute
		svc.Config.API.Console.ActionAuthTTL = time.Minute
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	portalReady := make(chan *DoerContinuationPortal, 1)
	go func() {
		done <- serveWithComposition(ctx, svc, noopTelemetry{}, func(p *DoerContinuationPortal) { portalReady <- p })
	}()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	waitFor(t, "unix", svc.Config.API.UnixSocket)
	owningPortal := <-portalReady
	if resultBrowser || requeueBrowser || len(recovery) > 0 && strings.HasPrefix(recovery[0], "linked-requeue-console") {
		waitFor(t, "tcp", svc.Config.API.Listen)
	}
	client := unixClient(svc.Config.API.UnixSocket)
	subject, err := svc.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	model := "proof-no-key"
	cli := newInstalledLoopQueueClient(t, svc)
	if controllerParticipant && controllerHistory != "reusable-controller" {
		// Cold compilation must not consume a real context deadline before the
		// synthetic clock's first admitted runtime stage. Keep all clock users
		// on the same method value and reauthenticate at the aligned stage.
		if delta := time.Now().UTC().Sub(requeueClock.Now()); delta > 0 {
			requeueClock.Advance(delta)
		}
	}
	subject, err = svc.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	charter := core.Charter{SchemaVersion: core.SchemaVersion, AgentID: "journey-agent", Name: "Journey test", Revision: 1,
		Runtime: core.RuntimeConstraint{Adapter: "hermes", Runtime: "hermes-agent", VersionConstraint: ">=0.18.0", Target: "aegis-owned-ephemeral"},
		Stanzas: []core.TrustStanza{{ID: "principal", Name: "Principal", Enabled: true,
			Authentication: core.AuthenticationPolicy{Methods: []string{"local-os"}, Selectors: []core.IdentitySelector{{SubjectIDs: []string{"local-uid:" + strconv.Itoa(os.Getuid())}, PrincipalIDs: []string{svc.Config.Principal.ID}, Issuers: []string{"linux-so-peercred"}, Environments: []string{"local"}}}, RequireFresh: true, MaxAuthAgeSec: 60},
			Grant:          core.Grant{}, Scopes: core.Scopes{}, Session: core.SessionPolicy{MaximumLifetimeSec: 60, RequireReauth: true},
			Approval: core.ApprovalPolicy{RequiredOperations: []string{"provision"}, MaximumLifetimeSec: 60, SingleUse: true}, InformationFlow: core.InformationFlowPolicy{CrossStanza: "deny"},
			Hermes: core.HermesConfig{Model: model, Provider: "none"}}}, CreatedBy: svc.Config.Principal.ID, CreatedAt: svc.Now()}
	if singleRun {
		// Public JSON commonly uses explicit empty arrays, not null. Their
		// canonical digests must survive mandate and authority projection.
		charter.Stanzas[0].Grant = core.Grant{Capabilities: []string{}, Tools: []string{}}
		charter.Stanzas[0].Scopes = core.Scopes{Memory: []string{}, Credentials: []string{}}
	}
	wire, err := json.Marshal(charter)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := svc.ImportCharterAs(ctx, subject, wire)
	if err != nil {
		t.Fatal(err)
	}
	owner := "journey-owner"
	if singleRun {
		owner = svc.Config.Principal.ID
		svc.Config.Credentials.Authority.DeploymentID = "single-run-fixture"
	}
	if controllerParticipant {
		owner = "separately-recorded-owner"
	}
	fixture, err := json.Marshal(registry.CurrentFleetFixture{SchemaVersion: registry.CurrentFleetFixtureSchemaVersion, FleetID: "journey-fleet", Agents: []registry.CurrentFleetAgent{{SourceID: "journey-source", AgentID: charter.AgentID,
		Runtime:   registry.RuntimeBinding{Adapter: charter.Runtime.Adapter, Runtime: charter.Runtime.Runtime, Target: charter.Runtime.Target},
		Ownership: registry.Ownership{OwnerID: owner, AccountabilityID: owner}, Lifecycle: registry.LifecycleEnabled,
		Charter: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: charter.AgentID, Revision: 1, Digest: canonical.Digest}}}})
	if err != nil {
		t.Fatal(err)
	}
	var registered struct {
		Agent app.FleetAgent `json:"agent"`
	}
	// Exercise the advertised installed CLI through the actual owning service.
	registrationPath := filepath.Join(cli.root, "registration.json")
	registrationJSON, err := json.Marshal(app.NewRegisterFleetAgentInput(fixture, "journey-fleet", "journey-source"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(registrationPath, registrationJSON, 0600); err != nil {
		t.Fatal(err)
	}
	if err := cli.run(&registered, "agents", "register", registrationPath); err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	text := "hello"
	contract := loop.DoerContract{Task: "Create result.txt containing hello", Workspace: workspace, WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", ExpectedText: &text, MaxAttempts: 2, ExactBytes: true}
	betaText := "beta"
	betaContract := contract
	betaContract.Task = "Replace result.txt with beta"
	betaContract.ExpectedText = &betaText
	betaContract.ExactBytes = false
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
	if reusable {
		contract.ExactBytes = false // v5 retains its declared trimmed-text assertion.
		reusablePath := write("reusable-authoring.json", map[string]any{"agent_id": charter.AgentID, "loop_id": "journey-doer", "revision": 1, "idempotency_key": "journey-publish", "doer_reusable": loop.DoerReusableContract{MaxAttempts: 2}})
		reusableBytes, err := exec.Command(cli.binary, "loops", "doer-reusable", reusablePath).Output()
		if err != nil {
			t.Fatal(err)
		}
		// Decode into a fresh envelope: omitted v4-only fields must not survive
		// from the separately constructed selected-file proposal above.
		publication = app.PublishLoopInput{}
		if err := json.Unmarshal(reusableBytes, &publication); err != nil {
			t.Fatal(err)
		}
		revision = publication.Revision
		if len(revision.Outputs) != 0 {
			t.Fatal("canonical reusable Doer unexpectedly declares outputs")
		}
	}
	publicationPath := write("publication.json", publication)
	var validated app.ValidatedLoop
	if err := cli.run(&validated, "loops", "validate", publicationPath); err != nil {
		t.Fatal(err)
	}
	if validated.Published || validated.Validation.Outcome != loop.ValidationValid {
		t.Fatalf("validation: %+v", validated)
	}
	var published app.PublishedLoop
	if !retainedTask {
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
	}
	agent := registered.Agent.Revision
	input := app.QueueLoopInput{Agent: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: agent.AgentID, Revision: agent.Revision, Digest: agent.Digest},
		Loop:           reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: revision.LoopID, Revision: 1, Digest: revision.Digest},
		IdempotencyKey: "journey-request", Activate: true}
	var retained app.DoerDraft
	if retainedTask {
		retainedPath := write("retained.json", app.DoerDraftInput{CreationKey: "retained-parity", Agent: input.Agent, LoopID: revision.LoopID, Revision: 1, Contract: contract})
		if err := cli.run(&retained, "loops", "task-save", retainedPath); err != nil {
			t.Fatal(err)
		}
		input.IdempotencyKey = retained.PublicationKey + "-run"
	}
	if reusable {
		for _, value := range []struct {
			id    string
			kind  graph.ValueType
			value any
		}{
			{"task", graph.TypeString, contract.Task},
			{"workspace", graph.TypeString, contract.Workspace},
			{"writable_files", graph.TypeArray, contract.WritableFiles},
			{"verify_file", graph.TypeString, contract.VerifyFile},
			{"expected_text", graph.TypeString, text},
		} {
			raw, err := json.Marshal(value.value)
			if err != nil {
				t.Fatal(err)
			}
			input.Inputs = append(input.Inputs, graph.NormalizedInput{PortID: value.id, Type: value.kind, Value: raw})
		}
	}
	var pending app.QueueLoopResult
	if !retainedTask {
		pending, err = cli.queue(input)
		if err != nil {
			t.Fatal(err)
		}
		expectedReason, expectedAction := "provisioning_receipt_missing", "plan_preview_exact_charter"
		if singleRun && !controllerParticipant {
			expectedReason, expectedAction = "doer_runtime_unconfigured", "configure_controller_doer_runtime"
		}
		if pending.Reason != expectedReason || pending.RequiredAction != expectedAction || pending.RequiredCharter == nil || pending.RequiredCharter.Digest != canonical.Digest || pending.RequestID == "" || pending.QueueItemID != "" || pending.Execution != nil {
			t.Fatalf("missing receipt advanced execution: %+v; preparation: %+v", pending, pending.Preparation)
		}
		subject, err = svc.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
		if err != nil {
			t.Fatal(err)
		}
		if view, err := svc.GetLoopViewAs(ctx, subject, revision.LoopID, 1); err != nil || view.Lifecycle.State != loop.LifecycleDraft || len(view.History) != 0 {
			t.Fatalf("missing receipt activated Loop: %+v %v", view.Lifecycle, err)
		}
	} else if _, err := svc.GetLoopAs(ctx, subject, revision.LoopID, 1); err == nil {
		t.Fatal("retained candidate must initially be unpublished")
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

	// Synthetic local Laya only. Owner-principal Runs must obtain their exact
	// signed scope through the owning service, never a fixture digest allowlist.
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
	if slowHelper {
		// Actual protocol helper latency exceeds the public inspection deadline.
		// Execution must still qualify the helper, not omit its admission check.
		layaScript = strings.Replace(layaScript, "read request\n", "read request\nsleep 11 >/dev/null 2>&1 &\nwait\n", 1)
	}
	if err := os.WriteFile(laya, []byte(layaScript), 0700); err != nil {
		t.Fatal(err)
	}
	layaHome := t.TempDir()
	if err := os.Chmod(layaHome, 0700); err != nil {
		t.Fatal(err)
	}
	var allowed []string
	if !singleRun {
		digest, err := contract.Digest()
		if err != nil {
			t.Fatal(err)
		}
		allowed = []string{digest}
		if reusable {
			betaDigest, err := betaContract.Digest()
			if err != nil || betaDigest == digest {
				t.Fatal("distinct preauthorized task contracts", err)
			}
			allowed = append(allowed, betaDigest)
		}
	}
	if err := svc.QueueWorker.ConfigureImplementation(config.Implementation{GoBinary: goBinary, AuthorizedContracts: allowed, LayaPython: laya, LayaHome: layaHome}, svc.Config.StateDir, svc.Hermes); err != nil {
		t.Fatal(err)
	}
	if reusable && !singleRun {
		unapproved := input
		unapproved.IdempotencyKey = "journey-unapproved-task"
		unapproved.Inputs = append([]graph.NormalizedInput(nil), input.Inputs...)
		unapproved.Inputs[0].Value = json.RawMessage(`"Different unapproved task"`)
		denied, err := cli.queue(unapproved)
		if err != nil || denied.Reason != "doer_host_approval_absent" || denied.RequiredAction != "review_exact_reusable_run_host_write" || denied.Readiness == nil || denied.Readiness.Controller.State != "blocked" || denied.Readiness.Helper.State != "not_checked" || denied.QueueItemID != "" || denied.Execution != nil || denied.SetupReviewTask == nil {
			t.Fatalf("unapproved task admitted: %+v %v", denied, err)
		}
		var items []app.QueueExecutionView
		if err := cli.run(&items, "queue", "list"); err != nil || len(items) != 0 {
			t.Fatalf("unapproved task created work: %+v %v", items, err)
		}
		if view, err := svc.GetLoopViewAs(ctx, subject, revision.LoopID, 1); err != nil || view.Lifecycle.State != loop.LifecycleDraft || len(view.History) != 0 {
			t.Fatal("unapproved task activated reusable Loop", err)
		}
		if _, err := os.Stat(filepath.Join(workspace, "result.txt")); !os.IsNotExist(err) {
			t.Fatal("unapproved task wrote result")
		}
	}
	if controllerParticipant {
		// A different recorded owner does not inherit the owner-principal's
		// provisioning exemption. The original Run supplies the retained exact
		// review; the fixture separately authenticates on the actual native route.
		if pending.SetupReviewTask == nil || pending.SetupReviewTask.HostRun == nil {
			t.Fatal("missing retained exact provisioning review")
		}
		approveIssue351FixtureProvision(t, svc, *pending.SetupReviewTask)
	}
	if reusable && singleRun {
		readiness, err := svc.ReadDoerReadinessAs(ctx, subject, app.DoerReadinessInput{Agent: input.Agent, Loop: input.Loop, Inputs: input.Inputs, IdempotencyKey: input.IdempotencyKey, Activate: input.Activate})
		if err != nil || readiness.Controller.State != "blocked" {
			t.Fatalf("pure reusable readiness granted scope: %+v %v", readiness, err)
		}
		if _, err := svc.ReadDoerHostWriteApprovalAs(ctx, subject, input.Agent, contract); err == nil {
			t.Fatal("publication, activation intent or readiness minted host approval")
		}
	}
	if controllerParticipant {
		// Isolated synthetic independent-password consent through the real protected
		// review/decision routes. Never a live human approval or provider claim.
		blocked, err := cli.queue(input)
		if err != nil || blocked.SetupReviewTask == nil || blocked.SetupReviewTask.HostRun == nil || blocked.QueueItemID != "" || blocked.Execution != nil || blocked.SetupReviewTask.Agent != input.Agent || blocked.SetupReviewTask.HostRun.Loop != input.Loop || blocked.SetupReviewTask.HostRun.IdempotencyKey != input.IdempotencyKey {
			t.Fatalf("exact retained host-review handoff: %+v %v", blocked, err)
		}
		replay, err := cli.queue(input)
		if err != nil || !reflect.DeepEqual(replay.SetupReviewTask, blocked.SetupReviewTask) || !reflect.DeepEqual(blocked.SetupReviewTask, pending.SetupReviewTask) {
			t.Fatal("same blocked Run created a new review task", err)
		}
		approveIssue351FixtureHost(t, svc, *blocked.SetupReviewTask)
	}
	if err := svc.QueueWorker.ValidateLoopAdmission(published.Revision, agent); err != nil && !singleRun {
		t.Fatalf("Doer controller fixture prerequisites: %v", err)
	}
	if !singleRun {
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

	}
	var reviewedSetup map[string][]json.RawMessage
	if controllerParticipant {
		reviewedSetup = map[string][]json.RawMessage{}
		for _, bucket := range []string{"plans", "approvals", "receipts"} {
			if err := svc.Store.List(bucket, func(raw json.RawMessage) error {
				reviewedSetup[bucket] = append(reviewedSetup[bucket], append(json.RawMessage(nil), raw...))
				return nil
			}); err != nil || len(reviewedSetup[bucket]) != 1 {
				t.Fatalf("missing exact independent fixture setup for %s: %v", bucket, err)
			}
		}
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
	// Synthetic proposals are selected by the explicit task, never by the
	// expected_text assertion. Real controller verification still reads bytes.
	betaEdits, err := json.Marshal(map[string]any{"edits": []implementation.Edit{{Path: "result.txt", Content: []byte("beta")}}, "report": "Replaced the selected file"})
	if err != nil {
		t.Fatal(err)
	}
	betaEvent, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "event", "params": map[string]any{"type": "message.complete", "session_id": "fixture", "payload": map[string]string{"status": "complete", "text": string(betaEdits)}}})
	if err != nil {
		t.Fatal(err)
	}
	promptAssertion := `*expected_text*hello*exact_bytes*true*max_attempts*2*`
	if reusable {
		// v5's closed binding has no exact_bytes port and preserves trimmed text.
		promptAssertion = `*expected_text*hello*max_attempts*2*`
	}
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready","payload":{}}}'
[ "$HERMES_TUI_TOOLSETS" = "context_engine" ] || exit 90
IFS= read -r tools || exit 1
printf '%%s\n' '{"jsonrpc":"2.0","id":"aegis-tools","result":{"total":0,"sections":[]}}'
while IFS= read -r create; do
  printf '%%s\n' '{"jsonrpc":"2.0","id":"create","result":{"session_id":"fixture"}}'
  IFS= read -r prompt || exit 0
  case "$prompt" in
    *'Return only JSON edits'*)
      case "$prompt" in %s|*Replace\ result.txt\ with\ beta*expected_text*beta*max_attempts*2*) ;; *) exit 91 ;; esac
      ;;
  esac
  printf '%%s\n' '{"jsonrpc":"2.0","id":"prompt","result":{"accepted":true}}'
  printf '%%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.start","session_id":"fixture","payload":{}}}'
  case "$prompt" in
    *'Replace result.txt with beta'*) printf '%%s\n' '%s' ;;
    *) printf '%%s\n' '%s' ;;
  esac
done
`, promptAssertion, string(betaEvent), string(event))
	if err := os.WriteFile(filepath.Join(install, "venv", "bin", "python"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if continuity {
		installedReusableFailureContinuity(t, ctx, svc, cli, input, workspace, install, script)
		return
	}

	if len(recovery) > 0 && strings.HasPrefix(recovery[0], "linked") {
		if strings.HasPrefix(recovery[0], "linked-requeue") {
			installedPersistedRequeue(t, ctx, svc, owningPortal, requeueClock, cli, input, workspace, install, script, strings.HasSuffix(recovery[0], "failure"), strings.HasPrefix(recovery[0], "linked-requeue-console"), recovery[0] == "linked-requeue-console-mismatched-authentication")
			return
		}
		svc.Config.Principal.AuthTTL = 10 * time.Second
		installedLinkedReexecution(t, ctx, svc, cli, input, workspace, install, script, recovery[0] == "linked-failure")
		return
	}
	var failed app.QueueLoopResult
	if len(recovery) > 0 && recovery[0] == "legacy" {
		// Fault before the new completion-proof write: models a legacy completed
		// transition, not deletion of a fence, mandate, or signed history fact.
		proofBlock := filepath.Join(svc.Store.Root(), "queue-session-preparation-failed")
		if err := os.WriteFile(proofBlock, []byte("isolated old-version fixture"), 0600); err != nil {
			t.Fatal(err)
		}
		_, failureErr := cli.queue(input)
		if failureErr == nil {
			t.Fatal("completion-proof fault did not fail")
		}
		if err := os.Remove(proofBlock); err != nil {
			t.Fatal(err)
		}
		legacyView, e := svc.GetQueueItemAs(ctx, subject, pending.RequestID+"-queue")
		err = e
		failed = app.QueueLoopResult{QueueItemID: pending.RequestID + "-queue", Execution: &legacyView}
		if err != nil || failed.Execution == nil || len(failed.Execution.Transitions) == 0 || failed.Execution.Transitions[len(failed.Execution.Transitions)-1].Reason != "session_start_preparation_failed" || len(failed.Execution.Attempts) != 0 {
			t.Fatalf("legacy failure not completed: %+v %v", failed, err)
		}
		contexts, e := svc.Authority.ListAuthorityContexts(ctx)
		if e != nil || len(contexts) != 0 {
			t.Fatal("legacy validation failure issued authority", e)
		}
		input.QueueItemID = failed.QueueItemID
	} else if len(recovery) > 0 {
		runtimeRoot := filepath.Join(svc.Store.Root(), "runtime")
		if err := os.WriteFile(runtimeRoot, []byte("isolated failed-launch fixture"), 0600); err != nil {
			t.Fatal(err)
		}
		failed, err = cli.queue(input)
		if err != nil || failed.Reason != "session_start_preparation_failed" || failed.Preparation == nil || failed.Preparation.Stage != "runtime_launch" || !failed.Preparation.RetrySafe || failed.Execution == nil || len(failed.Execution.Attempts) != 0 || failed.Execution.RuntimeAuthorityBinding != nil {
			t.Fatalf("failed preparation: %+v %v", failed, err)
		}
		if err := os.Remove(runtimeRoot); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(workspace, "result.txt")); !os.IsNotExist(err) {
			t.Fatal("preparation wrote selected file")
		}
		input.QueueItemID = failed.QueueItemID
	}
	var result app.QueueLoopResult
	if retainedTask {
		if slowHelper {
			if err := os.WriteFile(laya, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
				t.Fatal(err)
			}
			blocked, e := svc.ExecuteDoerTaskAs(ctx, subject, app.ExecuteDoerTaskInput{ID: retained.ID, ExpectedVersion: retained.Version})
			if e != nil || blocked.Readiness.Reason != "local_laya_unavailable" || blocked.Readiness.CanExecute || blocked.Readiness.Helper.State != "blocked" || blocked.Publication != nil || blocked.Run != nil || blocked.Task.ExecutionDigest != revision.Digest {
				t.Fatalf("unqualified helper admitted or intent not frozen: %+v %v", blocked, e)
			}
			if _, e := svc.GetLoopAs(ctx, subject, revision.LoopID, revision.Revision); e == nil {
				t.Fatal("unqualified helper published retained candidate")
			}
			items, e := svc.ListQueueAs(ctx, subject)
			if e != nil || len(items) != 0 {
				t.Fatalf("unqualified helper created Queue work: %+v %v", items, e)
			}
			if _, e := os.Stat(filepath.Join(workspace, "result.txt")); !os.IsNotExist(e) {
				t.Fatalf("unqualified helper wrote result: %v", e)
			}
			if e := os.WriteFile(laya, []byte(layaScript), 0700); e != nil {
				t.Fatal(e)
			}
			// An exact-Run signed scope is not authority for public observation.
			// The independent preauthorized fixture tests the public probe deadline.
			readiness, e := svc.ProbeDoerCandidateReadinessAs(ctx, subject, app.DoerCandidateReadinessInput{Agent: input.Agent, Candidate: revision})
			if e != nil || readiness.CanExecute || readiness.Helper.State != "not_checked" || readiness.Controller.State != "blocked" {
				t.Fatalf("public inspection reused private Run scope: %+v err=%v", readiness, e)
			}
		}
		start := time.Now()
		observed, retainedErr := svc.ExecuteDoerTaskAs(ctx, subject, app.ExecuteDoerTaskInput{ID: retained.ID, ExpectedVersion: retained.Version})
		if retainedErr != nil || observed.Run == nil || observed.Publication == nil || observed.Status == "independent_setup_required" {
			t.Fatalf("retained status=%s readiness=%s err=%v", observed.Status, observed.Readiness.Reason, retainedErr)
		}
		if slowHelper {
			if !observed.Readiness.CanExecute || observed.Readiness.Helper.State != "ready" || time.Since(start) < 11*time.Second {
				t.Fatalf("execution skipped slow helper qualification: %+v", observed.Readiness)
			}
			t.Logf("retained execution qualified slow helper and ran in %s", time.Since(start))
		}
		if observed.Task.ID != retained.ID || observed.Task.Version != retained.Version || observed.Task.PublicationKey != retained.PublicationKey || observed.Publication.Revision.Digest != revision.Digest {
			t.Fatal("retained execution changed immutable identity")
		}
		pending = *observed.Run
		if slowHelper {
			// Independent replay happens at a new authority stage, not after
			// fixture helper latency has consumed the principal's short TTL.
			// Authenticate the real native subject again; preserve all Run IDs.
			requeueClock.Advance(time.Second)
			subject, err = svc.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
			if err != nil {
				t.Fatal(err)
			}
		}
		replayed, replayErr := svc.ExecuteDoerTaskAs(ctx, subject, app.ExecuteDoerTaskInput{ID: retained.ID, ExpectedVersion: retained.Version})
		if replayErr != nil || replayed.Run == nil || replayed.Publication == nil || !reflect.DeepEqual(replayed.Task, observed.Task) || !reflect.DeepEqual(replayed.Publication.Revision, observed.Publication.Revision) || replayed.Publication.Validation.Digest != observed.Publication.Validation.Digest || !replayed.Publication.Decision.Idempotent || replayed.Run.RequestID != observed.Run.RequestID || replayed.Run.QueueItemID != observed.Run.QueueItemID {
			t.Fatalf("retained replay changed immutable identity: %+v %v", replayed, replayErr)
		}
	}
	result, err = cli.queue(input)
	if err != nil {
		t.Fatal(err)
	}
	if result.RequestID != pending.RequestID || result.QueueItemID == "" || result.Execution == nil || result.Execution.Projection.State != queue.StateSucceeded {
		t.Fatalf("exact one-request v4 execution blocked: %+v", result)
	}
	if len(recovery) > 0 && recovery[0] == "legacy" {
		if result.Preparation == nil || result.Preparation.Stage != "legacy_mandate_empty_slice_mismatch" {
			t.Fatal("legacy failure not proven")
		}
		failed.Preparation = result.Preparation
		failed.Graph = result.Graph
	}
	if len(recovery) > 0 {
		if result.QueueItemID != failed.QueueItemID || result.Graph != failed.Graph || result.Preparation == nil || result.Preparation.Digest != failed.Preparation.Digest || !reflect.DeepEqual(result.Execution.Item, failed.Execution.Item) || result.Execution.Submission != failed.Execution.Submission {
			t.Fatal("recovery rewrote immutable identity or lost completed failure")
		}
		var retained json.RawMessage
		if err := svc.Store.Load("queue-session-preparation", failed.Preparation.ReservationID, &retained); err != nil {
			t.Fatal("original fence lost", err)
		}
		if result.Execution.RuntimeAuthorityBinding == nil || result.Execution.RuntimeAuthorityBinding.Authority.ID == failed.Preparation.AuthorityID {
			t.Fatal("failed authority reused")
		}
		found := false
		for _, tr := range result.Execution.Transitions {
			if tr.TransitionID == failed.Preparation.TransitionID && tr.Digest == failed.Preparation.TransitionDigest {
				found = true
			}
		}
		if !found {
			t.Fatal("failed transition lost")
		}
	}
	view, err := svc.GetLoopViewAs(ctx, subject, revision.LoopID, 1)
	if err != nil || view.Lifecycle.State != loop.LifecycleActive || len(view.History) != 1 {
		t.Fatalf("activation: %+v %v", view.Lifecycle, err)
	}
	graphs, err := svc.ListGraphsAs(ctx, subject)
	if err != nil || len(graphs) != 0 || result.Graph != (reference.RevisionRef{}) {
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
	// CLI indentation differs when a view is nested in a Run response.
	// Marshal compacts RawMessage input values; compare every semantic field.
	equalViews := func(a, b app.QueueExecutionView) bool {
		left, leftErr := json.Marshal(a)
		right, rightErr := json.Marshal(b)
		return leftErr == nil && rightErr == nil && string(left) == string(right)
	}
	if !equalViews(readback, *result.Execution) || len(readback.LoopExecutions) != 1 {
		t.Fatal("independent readback changed execution or child cardinality")
	}
	history, err := svc.ListSubmissionHistoryAs(ctx, subject)
	if err != nil || len(history.Accepted) != 1 {
		t.Fatalf("direct history: %+v %v", history, err)
	}
	run := history.Accepted[0]
	if run.Snapshot.SchemaVersion != graph.DirectRunSnapshotSchemaVersion || run.Snapshot.Execution == nil || run.Snapshot.Graph != (reference.RevisionRef{}) || !reflect.DeepEqual(run.Snapshot.Participants, []reference.RevisionRef{input.Agent}) || !reflect.DeepEqual(run.Snapshot.Loops, []reference.RevisionRef{input.Loop}) || !reflect.DeepEqual(run.QueueItem, readback.Item) || run.Submission != readback.Submission || run.GraphRun != readback.GraphRun {
		t.Fatal("direct snapshot exact lineage mismatch")
	}
	node := run.Snapshot.Execution.Topology.Nodes[0]
	if node.Participant != input.Agent || node.Loop != input.Loop {
		t.Fatal("direct node exact refs mismatch")
	}
	if reusable {
		boundInputs := make([]loop.DoerInput, 0, len(run.Snapshot.Execution.Inputs))
		for _, value := range run.Snapshot.Execution.Inputs {
			boundInputs = append(boundInputs, loop.DoerInput{PortID: value.PortID, Type: loop.ValueType(value.Type), Value: value.Value})
		}
		bound, err := loop.NormalizeDoerBinding(published.Revision, boundInputs)
		if err != nil || !reflect.DeepEqual(bound, contract) || len(node.Outputs) != 0 || readback.Source != input.Loop || readback.Item.MaxAttempts != 1 {
			t.Fatalf("reusable direct input/output/contract binding changed: %+v %v", bound, err)
		}
	}
	itemRef := reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: readback.Item.ItemID, Digest: readback.Item.Digest}
	snapshotRef := reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: run.Snapshot.SnapshotID, Digest: run.Snapshot.Digest}
	submissionRef := reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: readback.Submission.SubmissionID, Digest: readback.Submission.Digest}
	child, attempt, claim := readback.LoopExecutions[0], readback.Attempts[0], readback.Claims[0]
	if readback.Item.ItemID != result.QueueItemID || readback.Item.Submission != submissionRef || readback.Item.Snapshot != snapshotRef || readback.Submission.Snapshot != snapshotRef || readback.GraphRun.Snapshot != snapshotRef || readback.GraphRun.QueueItem != itemRef || readback.Item.GraphRunID != readback.GraphRun.GraphRunID || child.GraphRunID != readback.GraphRun.GraphRunID || child.GraphNodeID != node.ID || child.Participant != input.Agent || child.Loop != input.Loop || attempt.GraphRunID != child.GraphRunID || attempt.LoopExecutionID != child.LoopExecutionID || attempt.QueueItem != itemRef || attempt.ClaimID != claim.ClaimID || claim.QueueItem != itemRef || claim.AttemptID != attempt.AttemptID {
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
	entries, err := os.ReadDir(workspace)
	if err != nil || len(entries) != 1 || entries[0].Name() != "result.txt" || entries[0].IsDir() {
		t.Fatalf("unexpected implementation or agent-written acceptance output: %v %v", entries, err)
	}
	bytes, err := os.ReadFile(filepath.Join(workspace, "result.txt"))
	if err != nil || string(bytes) != text {
		t.Fatalf("selected file: %q %v", bytes, err)
	}
	if reusable {
		var named app.QueueResult
		if err := cli.run(&named, "queue", "result", result.QueueItemID); err != nil {
			t.Fatal(err)
		}
		if named.Result == nil || named.Verification != "verified" || named.Result.Content != text || named.Result.Encoding != "utf-8" || named.Result.SizeBytes != len(bytes) || named.Result.Artifact != *readback.Artifact || named.Result.Receipt != readback.Receipts[0] || named.Result.Loop != input.Loop || named.Result.Participant != input.Agent || named.Result.AttemptID != readback.Attempts[0].AttemptID || named.GraphRunID != readback.Item.GraphRunID || named.Task != contract.Task || named.Usage != "unavailable" || named.Elapsed == nil || named.Elapsed.Nanoseconds != readback.Disposition.OccurredAt.Sub(readback.Item.EnqueuedAt).Nanoseconds() {
			t.Fatalf("unbound named result: %+v", named)
		}
		stored, err := svc.Store.GetBlob(named.Result.FileRef)
		if err != nil || string(stored) != string(bytes) {
			t.Fatalf("stored selected bytes: %q %v", stored, err)
		}
	}
	if singleRun {
		for _, bucket := range []string{"plans", "approvals", "receipts"} {
			var observed []json.RawMessage
			if err := svc.Store.List(bucket, func(raw json.RawMessage) error {
				observed = append(observed, append(json.RawMessage(nil), raw...))
				return nil
			}); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if controllerParticipant {
				if !reflect.DeepEqual(observed, reviewedSetup[bucket]) {
					t.Fatalf("Run changed or fabricated reviewed %s", bucket)
				}
			} else if len(observed) != 0 {
				t.Errorf("single Run fabricated %s", bucket)
			}
		}
		grant, err := svc.ReadDoerHostWriteApprovalAs(ctx, subject, input.Agent, contract)
		if err != nil || grant.RunRequestID != result.RequestID || grant.Candidate != input.Loop || !reflect.DeepEqual(grant.Contract, contract) || !reflect.DeepEqual(grant.DoerReusable, published.Revision.DoerReusable) || (!controllerParticipant && grant.ExpiresAt.After(grant.ApprovedAt.Add(svc.Config.Principal.AuthTTL))) {
			t.Fatalf("exact Run scope: %+v %v", grant, err)
		}
		currentCharter, err := svc.GetCharter(charter.AgentID, 1)
		if err != nil || currentCharter.Digest != canonical.Digest || !reflect.DeepEqual(currentCharter.Charter.Stanzas[0].Approval.RequiredOperations, []string{"provision"}) {
			t.Fatal("immutable charter changed", err)
		}
		changed := input
		changed.Loop.Digest = "sha256:" + strings.Repeat("a", 64)
		if _, err := cli.queue(changed); err == nil {
			t.Fatal("substituted published Loop accepted")
		}
	}
	replayed, err := cli.queue(input)
	if err != nil || replayed.QueueItemID != result.QueueItemID || replayed.RequestID != result.RequestID || replayed.Execution == nil || len(replayed.Execution.Attempts) != 1 {
		t.Fatalf("same-key replay duplicated execution: %+v %v", replayed, err)
	}
	var afterReplay app.QueueExecutionView
	if err := cli.run(&afterReplay, "queue", "show", result.QueueItemID); err != nil {
		t.Fatal(err)
	}
	if controllerParticipant {
		testQueueResultDenials(t, svc, subject, result.QueueItemID, client)
		if controllerHistory != "reusable-controller" {
			testIssue351ControllerHistoricalResult(t, svc, input, contract, result.QueueItemID, requeueClock, controllerHistory)
		}
		return
	}
	if reusable {
		second := input
		second.IdempotencyKey = "journey-request-beta"
		second.Inputs = append([]graph.NormalizedInput(nil), input.Inputs...)
		second.Inputs[0].Value, _ = json.Marshal(betaContract.Task)
		second.Inputs[4].Value, _ = json.Marshal(betaText)
		// Keep hello intact until the second admitted runtime replaces it. It
		// does not satisfy beta's predeclared trimmed-text assertion.
		before, err := os.ReadFile(filepath.Join(workspace, "result.txt"))
		if err != nil || string(before) != "hello" || strings.TrimSpace(string(before)) == betaText {
			t.Fatal("second task precondition", err)
		}
		secondRun, err := cli.queue(second)
		if err != nil || secondRun.Execution == nil || secondRun.Execution.Projection.State != queue.StateSucceeded || secondRun.QueueItemID == result.QueueItemID || secondRun.RequestID == result.RequestID {
			t.Fatalf("second Run: %+v %v", secondRun, err)
		}
		var beta, original app.QueueResult
		if err := cli.run(&beta, "queue", "result", secondRun.QueueItemID); err != nil {
			t.Fatal(err)
		}
		if err := cli.run(&original, "queue", "result", result.QueueItemID); err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			named      app.QueueResult
			run        app.QueueLoopResult
			task, text string
			contract   loop.DoerContract
		}{{original, result, contract.Task, text, contract}, {beta, secondRun, betaContract.Task, betaText, betaContract}} {
			v, named := tc.run.Execution, tc.named
			wantDigest, err := tc.contract.Digest()
			if err != nil || v == nil || len(v.Attempts) != 1 || len(v.Claims) != 1 || len(v.LoopExecutions) != 1 || len(v.Receipts) != 1 || v.Disposition == nil || v.Artifact == nil {
				t.Fatal("second-task evidence incomplete", err)
			}
			if named.Result == nil || named.Status != queue.StateSucceeded || named.Verification != "verified" || named.Task != tc.task || named.Result.Content != tc.text || named.Result.ContractDigest != wantDigest || named.Result.Loop != input.Loop || named.Source != input.Loop || named.Result.Participant != input.Agent || named.QueueItem.ID != tc.run.QueueItemID || named.QueueItem.Digest != v.Item.Digest || named.GraphRunID != v.Item.GraphRunID || named.Result.AttemptID != v.Attempts[0].AttemptID || named.Result.LoopExecutionID != v.LoopExecutions[0].LoopExecutionID || named.Result.Artifact != *v.Artifact || named.Result.Receipt != v.Receipts[0] || named.Result.Receipt.Outcome != evidence.Passed || named.Elapsed == nil || named.Elapsed.Source != "queue.item.enqueued_at_to_disposition.occurred_at" || named.Elapsed.Nanoseconds != v.Disposition.OccurredAt.Sub(v.Item.EnqueuedAt).Nanoseconds() || named.Usage != "unavailable" {
				t.Fatalf("exact two-task result: %+v", named)
			}
		}
		if os.Getenv("AEGIS_QUEUE_RESULT_BROWSER_TEST") == "1" {
			proof := exec.Command("python3", "../../scripts/queue_result_browser_test.py")
			proof.Env = append(os.Environ(), "AEGIS_CONSOLE_BASE="+svc.Config.API.Console.Origin, "AEGIS_CONSOLE_PASSWORD="+principalTestSecret(), "AEGIS_QUEUE_RESULT_RECORDS="+write("browser-results.json", []app.QueueResult{original, beta}))
			output, err := proof.CombinedOutput()
			t.Log(string(output))
			if err != nil {
				t.Fatal("authenticated Chrome result inspection", err)
			}
		}
		if original.Result.FileRef == beta.Result.FileRef || original.Result.ContractDigest == beta.Result.ContractDigest || original.Result.Artifact.Digest == beta.Result.Artifact.Digest {
			t.Fatal("distinct results collapsed")
		}
		after, err := os.ReadFile(filepath.Join(workspace, "result.txt"))
		if err != nil || string(after) != betaText {
			t.Fatal("runtime did not replace hello with beta", err)
		}
		for _, request := range []app.QueueLoopInput{input, second} {
			replay, err := cli.queue(request)
			want := result
			if request.IdempotencyKey == second.IdempotencyKey {
				want = secondRun
			}
			if err != nil || replay.QueueItemID != want.QueueItemID || replay.Execution == nil || len(replay.Execution.Attempts) != 1 {
				t.Fatal("two-task replay duplicated attempt", err)
			}
			changed := request
			changed.Inputs = append([]graph.NormalizedInput(nil), request.Inputs...)
			changed.Inputs[0].Value = json.RawMessage(`"Changed payload"`)
			if _, err := cli.queue(changed); err == nil {
				t.Fatal("changed payload did not conflict")
			}
		}
		graphs, err := svc.ListGraphsAs(ctx, subject)
		if err != nil || len(graphs) != 0 {
			t.Fatal("reusable run published Graph", err)
		}
		view, err := svc.GetLoopViewAs(ctx, subject, revision.LoopID, 1)
		if err != nil || view.Revision.Digest != input.Loop.Digest || len(view.History) != 1 {
			t.Fatal("second run republished/reactivated Loop", err)
		}
		items, err := svc.ListQueueAs(ctx, subject)
		if err != nil || len(items) != 2 {
			t.Fatal("two-task queue cardinality", err)
		}
		// Tampering fixtures run only after the untouched public Run→Run proof.
		testQueueResultDenials(t, svc, subject, result.QueueItemID, client)
	}
	if !equalViews(afterReplay, readback) || replayed.Graph != result.Graph || !equalViews(*replayed.Execution, readback) {
		t.Fatal("fresh replay readback changed terminal lineage")
	}
}
