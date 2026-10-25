package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	"github.com/berryhill/aegis/internal/testprocess"
	"strconv"
)

// Contract: compiled candidate CLI -> real Serve peer/bearer transport -> shared
// application -> isolated Badger. apiService supplies a synthetic runtime; this
// is lifecycle qualification, NOT managed-provider or workstation acceptance.
func TestInstalledOnlineLifecycle(t *testing.T) {
	svc := apiService(t)
	clock := newRequeueStageClock(time.Now().UTC())
	svc.Now = clock.Now
	store := configureAPIFleet(t, svc)
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
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(repo, ".aegis350-lifecycle-test-bin")
	t.Cleanup(func() { os.Remove(binary) })
	build := exec.Command("sh", "scripts/build-source.sh", binary)
	build.Dir = repo
	build.Env = append(os.Environ(), "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=core.worktree", "GIT_CONFIG_VALUE_0="+repo)
	if out, err := testprocess.CombinedOutput(build, 2*time.Minute); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	save := func(name string, value any) string {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	cfg := save("owner.json", svc.Config)
	target := svc.Config.API.Console.Origin + "/console"
	run := func(config, destination string, args ...string) ([]byte, error) {
		t.Helper()
		// Each CLI call reads config and performs its operation: pace the real limiter.
		time.Sleep(500 * time.Millisecond)
		cmd := exec.Command(binary, append([]string{"--config", config, "--target", destination}, args...)...)
		return testprocess.CombinedOutput(cmd, 10*time.Second)
	}
	ok := func(args ...string) []byte {
		t.Helper()
		out, err := run(cfg, target, args...)
		if err != nil {
			t.Fatalf("%v: %v %s", args, err, out)
		}
		return out
	}
	deny := func(args ...string) {
		t.Helper()
		out, err := run(cfg, target, args...)
		if err == nil {
			t.Fatalf("admitted %v: %s", args, out)
		}
	}

	subject, err := svc.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	charter := core.Charter{SchemaVersion: core.SchemaVersion, AgentID: "builder", Name: "Distributed test", Revision: 1,
		Runtime: core.RuntimeConstraint{Adapter: "hermes", Runtime: "hermes-agent", VersionConstraint: ">=0.18.0", Target: "aegis-owned-ephemeral"},
		Stanzas: []core.TrustStanza{{ID: "principal", Name: "Principal", Enabled: true,
			Authentication: core.AuthenticationPolicy{Methods: []string{"local-os"}, Selectors: []core.IdentitySelector{{SubjectIDs: []string{"local-uid:" + strconv.Itoa(os.Getuid())}, PrincipalIDs: []string{svc.Config.Principal.ID}, Issuers: []string{"linux-so-peercred"}, Environments: []string{"local"}}}, RequireFresh: true, MaxAuthAgeSec: 60},
			Grant:          core.Grant{Capabilities: []string{"chat"}, Tools: []string{"no_mcp"}}, Scopes: core.Scopes{Memory: []string{"principal-memory"}, Credentials: []string{"provider:test"}},
			Session: core.SessionPolicy{MaximumLifetimeSec: 60, RequireReauth: true}, Approval: core.ApprovalPolicy{RequiredOperations: []string{"provision"}, MaximumLifetimeSec: 60, SingleUse: true}, InformationFlow: core.InformationFlowPolicy{CrossStanza: "deny"},
			Hermes: core.HermesConfig{Toolsets: []string{"no_mcp"}, Model: "fixture-model", Provider: "test"}}}, CreatedBy: svc.Config.Principal.ID, CreatedAt: svc.Now()}

	charterRaw, err := json.Marshal(charter)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := svc.ImportCharterAs(ctx, subject, charterRaw)
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := json.Marshal(registry.CurrentFleetFixture{SchemaVersion: registry.CurrentFleetFixtureSchemaVersion, FleetID: "lifecycle-fleet", Agents: []registry.CurrentFleetAgent{{SourceID: "source", AgentID: "builder", Runtime: registry.RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent", Target: charter.Runtime.Target}, Ownership: registry.Ownership{OwnerID: "owner", AccountabilityID: "team"}, Lifecycle: registry.LifecycleEnabled, Charter: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "builder", Revision: 1, Digest: canonical.Digest}}}})
	if err != nil {
		t.Fatal(err)
	}
	registration := app.NewRegisterFleetAgentInput(fixture, "lifecycle-fleet", "source")
	registrationPath := save("registration.json", registration)
	var registered struct {
		Agent   app.FleetAgent `json:"agent"`
		Created bool           `json:"created"`
	}
	if err := json.Unmarshal(ok("agents", "register", registrationPath), &registered); err != nil || !registered.Created {
		t.Fatalf("registration: %v %+v", err, registered)
	}
	var replay struct {
		Agent   app.FleetAgent `json:"agent"`
		Created bool           `json:"created"`
	}
	if err := json.Unmarshal(ok("agents", "register", registrationPath), &replay); err != nil || replay.Created || !reflect.DeepEqual(registered.Agent, replay.Agent) {
		t.Fatalf("idempotency: %v %+v", err, replay)
	}

	conflict := registration
	conflict.Fixture = json.RawMessage(strings.Replace(string(fixture), `"owner"`, `"foreign-owner"`, 1))
	deny("agents", "register", save("registration-conflict.json", conflict))
	current := registered.Agent
	readAgent := func() app.FleetAgent {
		t.Helper()
		var got app.FleetAgent
		if err := json.Unmarshal(ok("agents", "show", "builder", fmt.Sprint(current.Revision.Revision)), &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := readAgent(); !reflect.DeepEqual(got, current) {
		t.Fatal("exact registration readback differs")
	}
	lifecycleInput := func(a app.FleetAgent) app.SetAgentLifecycleInput {
		return app.SetAgentLifecycleInput{Expected: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: a.Revision.AgentID, Revision: a.Revision.Revision, Digest: a.Revision.Digest}}
	}
	stale := lifecycleInput(current)
	for _, verb := range []string{"disable", "enable"} {
		input := lifecycleInput(current)
		var changed app.FleetAgent
		if err := json.Unmarshal(ok("agents", verb, "builder", save("lifecycle.json", input)), &changed); err != nil {
			t.Fatal(err)
		}
		want := registry.LifecycleDisabled
		if verb == "enable" {
			want = registry.LifecycleEnabled
		}
		if changed.Revision.Lifecycle != want || changed.Revision.Revision != current.Revision.Revision+1 {
			t.Fatalf("lifecycle %s: %+v", verb, changed)
		}
		current = changed
		if got := readAgent(); !reflect.DeepEqual(got, current) {
			t.Fatal("durable lifecycle readback differs")
		}
	}
	deny("agents", "disable", "builder", save("stale.json", stale))
	wrong := lifecycleInput(current)
	wrong.Expected.Digest = "sha256:" + strings.Repeat("b", 64)
	deny("agents", "disable", "builder", save("wrong.json", wrong))
	foreign := lifecycleInput(current)
	foreign.Expected.ID = "foreign-agent"
	deny("agents", "disable", "builder", save("foreign.json", foreign))
	var history []registry.AgentRevision
	if err := json.Unmarshal(ok("agents", "history", "builder"), &history); err != nil || len(history) != 3 || !reflect.DeepEqual(history[0], registered.Agent.Revision) || !reflect.DeepEqual(history[2], current.Revision) {
		t.Fatalf("append-only history or immutable registration drift: %v %+v", err, history)
	}
	if got := readAgent(); !reflect.DeepEqual(got, current) {
		t.Fatal("denial mutated Agent")
	}
	raw, _ := json.Marshal(lifecycleInput(current))
	for name, data := range map[string][]byte{"unknown": append([]byte(`{"unknown":true,`), raw[1:]...), "trailing": append(append([]byte(nil), raw...), []byte(` {}`)...), "malformed": []byte(`{`)} {
		p := filepath.Join(t.TempDir(), name+".json")
		if err := os.WriteFile(p, data, 0600); err != nil {
			t.Fatal(err)
		}
		deny("agents", "disable", "builder", p)
	}
	wrongConfig := svc.Config
	wrongConfig.StateDir = filepath.Join(t.TempDir(), "absent")
	if out, err := run(save("wrong-owner.json", wrongConfig), target, "agents", "disable", "builder", save("input.json", lifecycleInput(current))); err == nil || !bytes.Contains(out, []byte("owning_instance_mismatch")) {
		t.Fatalf("config mismatch: %v %s", err, out)
	}
	if _, err := os.Stat(wrongConfig.StateDir); !os.IsNotExist(err) {
		t.Fatalf("second store: %v", err)
	}
	if out, err := run(cfg, "http://127.0.0.1:9999/console", "agents", "show", "builder"); err == nil || !bytes.Contains(out, []byte("owning_instance_mismatch")) {
		t.Fatalf("target mismatch: %v %s", err, out)
	}
	wrongConfig = svc.Config
	wrongConfig.API.Token = "invalid-fixture-token"
	if out, err := run(save("unauthorized.json", wrongConfig), target, "agents", "show", "builder"); err == nil || !bytes.Contains(out, []byte("HTTP 401")) {
		t.Fatalf("auth: %v %s", err, out)
	}

	// Seed through real application services, never a handler or product fallback.
	subject, err = svc.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	form, err := decodeLoopComposerForm(composerRequest(validLoopComposerValues()))
	if err != nil {
		t.Fatal(err)
	}
	published, err := svc.PublishLoopAs(ctx, subject, app.PublishLoopInput{AgentID: "builder", Revision: form.Revision, IdempotencyKey: "lifecycle-loop"})
	if err != nil {
		t.Fatal(err)
	}
	lr := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: published.Revision.LoopID, Revision: published.Revision.Revision, Digest: published.Revision.Digest}
	if _, err := svc.SetLoopLifecycleAs(ctx, subject, lr.ID, app.SetLoopLifecycleInput{AgentID: "builder", Loop: lr, State: loop.LifecycleActive, EventID: "lifecycle-active"}); err != nil {
		t.Fatal(err)
	}
	inputPort := graph.Port{ID: "value", Type: graph.TypeString, Required: true}
	outputPort := graph.Port{ID: "result", Type: graph.TypeString, Required: true}
	gr, err := svc.PublishGraphAs(ctx, subject, app.PublishGraphInput{AgentID: "builder", IdempotencyKey: "lifecycle-graph", Revision: graph.GraphRevision{GraphID: "lifecycle-graph", Revision: 1, Inputs: []graph.Port{inputPort}, Outputs: []graph.Port{outputPort}, Nodes: []graph.Node{{ID: "work", Participant: lifecycleInput(current).Expected, Loop: lr, Inputs: []graph.Port{inputPort}, Outputs: []graph.Port{outputPort}}}, InputMappings: []graph.InputMapping{{GraphInput: "value", ToNodeID: "work", ToPort: "value"}}, OutputMappings: []graph.OutputMapping{{FromNodeID: "work", FromPort: "result", GraphOutput: "result"}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, verb := range []string{"cancel", "revoke"} {
		t.Run("queue-"+verb, func(t *testing.T) {
			id := "lifecycle-" + verb
			accepted, err := svc.SubmitGraphAs(ctx, subject, orchestration.SubmitGraphRequest{WorkspaceAgentID: "builder", Graph: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: gr.Revision.GraphID, Revision: gr.Revision.Revision, Digest: gr.Revision.Digest}, Inputs: []graph.NormalizedInput{{PortID: "value", Type: graph.TypeString, Value: json.RawMessage(`"lifecycle"`)}}, SubmissionID: id + "-submission", IdempotencyKey: id, SnapshotID: id + "-snapshot", QueueItemID: id, GraphRunID: id + "-run", TransitionID: id + "-accepted", RejectionID: id + "-rejected", MaxAttempts: 2})
			if err != nil || accepted.Accepted == nil {
				t.Fatalf("seed: %v %+v", err, accepted)
			}
			terminal := app.TerminalQueueItemInput{WorkspaceAgentID: "builder", QueueItemID: id, CancellationID: id + "-cancel", TransitionID: id + "-terminal"}
			var cancellation queue.Cancellation
			if err := json.Unmarshal(ok("queue", verb, save("terminal.json", terminal)), &cancellation); err != nil {
				t.Fatal(err)
			}
			var view app.QueueExecutionView
			if err := json.Unmarshal(ok("queue", "show", id), &view); err != nil {
				t.Fatal(err)
			}
			want := queue.StateCancelled
			if verb == "revoke" {
				want = queue.StateRevoked
			}
			projection, err := store.GetQueueProjection(ctx, id)
			if err != nil || projection.State != want || view.Projection.State != want || view.Disposition == nil || string(view.Disposition.State) != string(want) || view.Item.Digest != accepted.Accepted.QueueItem.Digest || view.Item.MaxAttempts != 2 || len(view.Claims) != 0 || cancellation.QueueItem.Digest != view.Item.Digest {
				t.Fatalf("durable terminal: %v %+v", err, view)
			}
			deny("queue", verb, save("terminal-again.json", terminal))
			after, err := store.GetQueueProjection(ctx, id)
			if err != nil || !reflect.DeepEqual(after, projection) {
				t.Fatalf("terminal denial mutation: %v", err)
			}
		})
	}

	// Real application setup only; all lifecycle decisions below use the compiled CLI.
	review, err := svc.PreviewPlanAs(ctx, subject, "builder", 1, core.Environment{Name: "local"})
	if err != nil {
		t.Fatal(err)
	}
	approval, err := svc.RequestApprovalAs(ctx, subject, review.Plan.ID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	approval, err = svc.DecideApprovalAs(ctx, subject, approval.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ApplyAs(ctx, subject, review.Plan.ID, approval.ID); err != nil {
		t.Fatal(err)
	}
	mandate, _, err := svc.PreviewSessionAs(ctx, subject, "builder", 1, "principal", core.Environment{Name: "local"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := svc.StartSessionAs(ctx, subject, mandate.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.TerminateSessionAs(ctx, subject, session.ID, "fixture_complete")
	authority, err := svc.FleetCommandAuthorityAs(ctx, subject)
	if err != nil {
		t.Fatal(err)
	}
	readQueue := func(id string) app.QueueExecutionView {
		t.Helper()
		var v app.QueueExecutionView
		if err := json.Unmarshal(ok("queue", "show", id), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	id := "lifecycle-reclaim"
	submission := app.SubmitGraphInput{WorkspaceAgentID: "builder", Graph: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: gr.Revision.GraphID, Revision: gr.Revision.Revision, Digest: gr.Revision.Digest}, Inputs: []graph.NormalizedInput{{PortID: "value", Type: graph.TypeString, Value: json.RawMessage(`"reclaim"`)}}, SubmissionID: id + "-submission", IdempotencyKey: id, SnapshotID: id + "-snapshot", QueueItemID: id, GraphRunID: id + "-run", TransitionID: id + "-accepted", RejectionID: id + "-rejected", MaxAttempts: 2}
	var admitted orchestration.SubmissionDecision
	if err := json.Unmarshal(ok("graphs", "submit", save("reclaim-submit.json", submission)), &admitted); err != nil || admitted.Accepted == nil {
		t.Fatalf("submission %v %+v", err, admitted)
	}
	binding := app.BindQueueRuntimeInput{AgentID: "builder", Authority: authority.Authority, QueueItemID: id, BindingID: id + "-binding", TransitionID: id + "-bound"}
	ok("queue", "bind-runtime", save("binding.json", binding))
	// Canonical abandoned-claim fixture through the sole already-open fixture store;
	// never a second writer or a fallback for a failed CLI operation (same as TestDQHandoff).
	now := clock.Now()
	le, err := execution.NewLoopExecution(execution.LoopExecution{LoopExecutionID: id + "-loop", GraphRunID: id + "-run", GraphNodeID: "work", Loop: lr, Participant: lifecycleInput(current).Expected, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	fact := fleet.AuditFact{Event: core.AuditEvent{Type: "fleet.test.fixture", SubjectID: id, PrincipalID: subject.PrincipalID, Outcome: "succeeded", Reason: "abandoned claim fixture"}}
	if _, err = store.CreateLoopExecution(ctx, le, fact); err != nil {
		t.Fatal(err)
	}
	seedClaim := func(n uint32) {
		t.Helper()
		claim, err := queue.NewClaim(queue.Claim{ClaimID: fmt.Sprintf("%s-claim-%d", id, n), QueueItem: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: id, Digest: admitted.Accepted.QueueItem.Digest}, AttemptID: fmt.Sprintf("%s-attempt-%d", id, n), WorkerID: "abandoned", Authority: authority.Authority, ClaimedAt: clock.Now(), ExpiresAt: clock.Now().Add(time.Second)})
		if err != nil {
			t.Fatal(err)
		}
		attempt, err := execution.NewAttempt(execution.Attempt{AttemptID: claim.AttemptID, GraphRunID: le.GraphRunID, LoopExecutionID: le.LoopExecutionID, QueueItem: claim.QueueItem, ClaimID: claim.ClaimID, AttemptNumber: n, CreatedAt: clock.Now()})
		if err != nil {
			t.Fatal(err)
		}
		transition, err := queue.NewTransition(queue.QueueTransition{TransitionID: claim.ClaimID + "-transition", QueueItemID: id, From: queue.StateQueued, To: queue.StateClaimed, ClaimID: claim.ClaimID, Reason: "abandoned fixture", OccurredAt: clock.Now()})
		if err != nil {
			t.Fatal(err)
		}
		if err = store.ClaimQueueItem(ctx, claim, attempt, transition, fact); err != nil {
			t.Fatal(err)
		}
	}
	seedClaim(1)
	before := readQueue(id)
	retry := app.RetryQueueItemInput{WorkspaceAgentID: "builder", QueueItemID: id, RetryID: id + "-retry", TransitionID: id + "-retried", Reclaimed: true}
	denyUnchanged := func(input app.RetryQueueItemInput) {
		t.Helper()
		projection, err := store.GetQueueProjection(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		deny("queue", "retry", save("denied-retry.json", input))
		after, err := store.GetQueueProjection(ctx, id)
		if err != nil || !reflect.DeepEqual(projection, after) {
			t.Fatal("denied retry mutated projection", err)
		}
	}
	denyUnchanged(retry) // live lease
	ordinary := retry
	ordinary.Reclaimed = false
	denyUnchanged(ordinary) // no stop acknowledgement exists; ordinary retry unavailable
	wrongAuthority := retry
	wrongAuthority.WorkspaceAgentID = ""
	wrongAuthority.Authority = authority.Authority
	wrongAuthority.Authority.Digest = "sha256:" + strings.Repeat("f", 64)
	denyUnchanged(wrongAuthority)
	clock.Advance(2 * time.Second)
	ordinary.Reclaimed = false
	denyUnchanged(ordinary)
	var reclaimed queue.Retry
	if err := json.Unmarshal(ok("queue", "retry", save("reclaim.json", retry)), &reclaimed); err != nil {
		t.Fatal(err)
	}
	after := readQueue(id)
	if !reclaimed.Reclaimed || reclaimed.AttemptNumber != 1 || reclaimed.ClaimID != before.Claims[0].ClaimID || reclaimed.OccurredAt.Before(before.Claims[0].ExpiresAt) || after.Projection.State != queue.StateQueued || after.Projection.Attempts != 1 || after.Item.Digest != before.Item.Digest || after.Item.MaxAttempts != 2 || !reflect.DeepEqual(after.GraphRun, before.GraphRun) || !reflect.DeepEqual(after.LoopExecutions, before.LoopExecutions) || !reflect.DeepEqual(after.Attempts, before.Attempts) || len(after.Retries) != 1 {
		t.Fatalf("reclaim identity/budget drift: before=%+v after=%+v", before, after)
	}
	denyUnchanged(retry) // stale replay cannot release a second claim
	seedClaim(2)
	clock.Advance(2 * time.Second)
	retry.RetryID += "-exhausted"
	retry.TransitionID += "-exhausted"
	denyUnchanged(retry)
	exhausted := readQueue(id)
	if exhausted.Projection.Attempts != 2 || len(exhausted.Attempts) != 2 || len(exhausted.LoopExecutions) != 1 || exhausted.Item.Digest != before.Item.Digest {
		t.Fatal("consumed budget or causal identities reset")
	}
	ok("queue", "cancel", save("cancel-reclaim.json", app.TerminalQueueItemInput{WorkspaceAgentID: "builder", QueueItemID: id, CancellationID: id + "-cancel", TransitionID: id + "-cancelled"}))
	denyUnchanged(retry) // terminal cannot reopen

	if err := json.Unmarshal(ok("agents", "retire", "builder", save("retire.json", lifecycleInput(current))), &current); err != nil {
		t.Fatal(err)
	}
	retired := readAgent()
	if retired.Revision.Lifecycle != registry.LifecycleRetired {
		t.Fatal("retirement not durable")
	}
	deny("agents", "enable", "builder", save("enable-retired.json", lifecycleInput(retired)))
	if got := readAgent(); !reflect.DeepEqual(got, retired) {
		t.Fatal("retired denial mutated Agent")
	}
	t.Log("PASS: compiled CLI, real Serve/Unix peer+bearer, Badger Agent replay/lifecycle/stale/digest/retired denial; Queue cancel/revoke durable dispositions and terminal denial; strict decode, auth, target/config no-second-store")
}
