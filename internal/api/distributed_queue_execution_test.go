package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
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
)

// Domain contract: workspace authority cannot claim or bind runtime work. A
// separately admitted controller context binds the exact Agent; submission
// replay preserves the original timestamp even after binding and disposition.
// Qualification: real stores and peer-authenticated HTTP runtime binding. apiService's
// synthetic session process is NOT supported-Hermes or installed-agent evidence.
// The configured NoKeyAdapter must reject Hermes work: the resulting failure,
// not a fabricated successful runtime result, is the authoritative disposition.
func TestDQHandoff(t *testing.T) {
	svc := apiService(t)
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
	client := unixClient(svc.Config.API.UnixSocket)
	subject, err := svc.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}

	// Use the existing API execution fixture's exact principal selector, with
	// fixture-only provider configuration and no newly invented authority.
	charter := core.Charter{SchemaVersion: core.SchemaVersion, AgentID: "distributed-agent", Name: "Distributed test", Revision: 1,
		Runtime: core.RuntimeConstraint{Adapter: "hermes", Runtime: "hermes-agent", VersionConstraint: ">=0.18.0,<0.19.0", Target: "aegis-owned-ephemeral"},
		Stanzas: []core.TrustStanza{{ID: "principal", Name: "Principal", Enabled: true,
			Authentication: core.AuthenticationPolicy{Methods: []string{"local-os"}, Selectors: []core.IdentitySelector{{SubjectIDs: []string{"local-uid:" + strconv.Itoa(os.Getuid())}, PrincipalIDs: []string{svc.Config.Principal.ID}, Issuers: []string{"linux-so-peercred"}, Environments: []string{"local"}}}, RequireFresh: true, MaxAuthAgeSec: 60},
			Grant:          core.Grant{Capabilities: []string{"chat"}, Tools: []string{"no_mcp"}}, Scopes: core.Scopes{Memory: []string{"principal-memory"}, Credentials: []string{"provider:test"}},
			Session: core.SessionPolicy{MaximumLifetimeSec: 60, RequireReauth: true}, Approval: core.ApprovalPolicy{RequiredOperations: []string{"provision"}, MaximumLifetimeSec: 60, SingleUse: true}, InformationFlow: core.InformationFlowPolicy{CrossStanza: "deny"},
			Hermes: core.HermesConfig{Toolsets: []string{"no_mcp"}, Model: "fixture-model", Provider: "test"}}}, CreatedBy: svc.Config.Principal.ID, CreatedAt: svc.Now()}
	var canonical core.CanonicalCharter
	apiRequest(t, client, http.MethodPost, "/v1/charters/import", charter, &canonical, http.StatusCreated)
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
	values := validLoopComposerValues()
	values.Set("evidence_claims", "start,output,application/json,sha256:"+strings.Repeat("a", 64)+",sha256,1")
	values.Set("required_evidence", "output,start")
	form, err := decodeLoopComposerForm(composerRequest(values))
	if err != nil {
		t.Fatal(err)
	}
	var published app.PublishedLoop
	apiRequest(t, client, http.MethodPost, "/v1/loops", app.PublishLoopInput{AgentID: agent.AgentID, Revision: form.Revision, IdempotencyKey: "distributed-loop"}, &published, http.StatusCreated)
	lr := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: published.Revision.LoopID, Revision: published.Revision.Revision, Digest: published.Revision.Digest}
	var activated app.LoopLifecycleResult
	apiRequest(t, client, http.MethodPut, "/v1/loops/"+lr.ID+"/lifecycle", app.SetLoopLifecycleInput{AgentID: agent.AgentID, Loop: lr, State: loop.LifecycleActive, EventID: "distributed-activate"}, &activated, http.StatusCreated)
	value := graph.Port{ID: "value", Type: graph.TypeString, Required: true}
	result := graph.Port{ID: "result", Type: graph.TypeString, Required: true}
	revision := graph.GraphRevision{GraphID: "distributed-graph", Revision: 1, Inputs: []graph.Port{value}, Outputs: []graph.Port{result},
		Nodes:         []graph.Node{{ID: "work", Participant: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: agent.AgentID, Revision: agent.Revision, Digest: agent.Digest}, Loop: lr, Inputs: []graph.Port{value}, Outputs: []graph.Port{result}}},
		InputMappings: []graph.InputMapping{{GraphInput: "value", ToNodeID: "work", ToPort: "value"}}, OutputMappings: []graph.OutputMapping{{FromNodeID: "work", FromPort: "result", GraphOutput: "result"}}}
	var gp app.PublishedGraph
	apiRequest(t, client, http.MethodPost, "/v1/graphs", app.PublishGraphInput{AgentID: agent.AgentID, Revision: revision, IdempotencyKey: "distributed-graph"}, &gp, http.StatusCreated)
	input := app.SubmitGraphInput{WorkspaceAgentID: agent.AgentID, Graph: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: gp.Revision.GraphID, Revision: gp.Revision.Revision, Digest: gp.Revision.Digest}, Inputs: []graph.NormalizedInput{{PortID: "value", Type: graph.TypeString, Value: json.RawMessage(`"distributed proof"`)}}, SubmissionID: "distributed-submission", IdempotencyKey: "distributed-submit", SnapshotID: "distributed-snapshot", QueueItemID: "distributed-queue", GraphRunID: "distributed-run", TransitionID: "distributed-transition", RejectionID: "distributed-rejection", MaxAttempts: 2}
	var accepted orchestration.SubmissionDecision
	apiRequest(t, client, http.MethodPost, "/v1/queue", input, &accepted, http.StatusCreated)
	if accepted.Accepted == nil || accepted.Accepted.InitialTransition.To != queue.StateAwaitingRuntime {
		t.Fatal("workspace did not enter awaiting_runtime")
	}
	original := accepted.Accepted.Submission
	read := func() app.QueueExecutionView {
		t.Helper()
		var view app.QueueExecutionView
		apiRequest(t, client, http.MethodGet, "/v1/queue/"+input.QueueItemID, nil, &view, http.StatusOK)
		return view
	}
	replay := func() {
		t.Helper()
		var replay orchestration.SubmissionDecision
		apiRequest(t, client, http.MethodPost, "/v1/queue", input, &replay, http.StatusOK)
		if replay.Created || replay.Accepted == nil || replay.Accepted.Submission.Digest != original.Digest || !replay.Accepted.Submission.SubmittedAt.Equal(original.SubmittedAt) || replay.Accepted.QueueItem.Digest != accepted.Accepted.QueueItem.Digest || replay.Accepted.Snapshot.Digest != accepted.Accepted.Snapshot.Digest {
			t.Fatal("replay changed immutable identity or submission timestamp")
		}
	}
	view := read()
	if view.Projection.State != queue.StateAwaitingRuntime || len(view.Claims) != 0 || view.Disposition != nil {
		t.Fatal("workspace invented runtime execution")
	}
	var listed []app.QueueExecutionView
	apiRequest(t, client, http.MethodGet, "/v1/queue", nil, &listed, http.StatusOK)
	if len(listed) != 1 || listed[0].Projection.State != queue.StateAwaitingRuntime {
		t.Fatal("awaiting_runtime list mismatch")
	}
	bind := app.BindQueueRuntimeInput{AgentID: agent.AgentID, QueueItemID: input.QueueItemID, Authority: original.Authority, BindingID: "distributed-bind", TransitionID: "distributed-bound"}
	apiRequest(t, client, http.MethodPost, "/v1/queue/"+input.QueueItemID+"/bind-runtime", bind, nil, http.StatusForbidden)
	// Authentication and strict decoding must deny before any queue mutation.
	raw, err := json.Marshal(bind)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, body, token string
		status            int
	}{
		{"unauthenticated", string(raw), "", http.StatusUnauthorized},
		{"unknown-field", strings.TrimSuffix(string(raw), "}") + `,"workspace":{}}`, "Bearer transport-secret", http.StatusBadRequest},
		{"trailing-json", string(raw) + ` {}`, "Bearer transport-secret", http.StatusBadRequest},
		{"path-mismatch", strings.Replace(string(raw), input.QueueItemID, "other-item", 1), "Bearer transport-secret", http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Preserve the production five-request/second limiter while adding two HTTP probes.
			time.Sleep(500 * time.Millisecond)
			req, err := http.NewRequest(http.MethodPost, "http://unix/v1/queue/"+input.QueueItemID+"/bind-runtime", strings.NewReader(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", tc.token)
			response, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != tc.status {
				t.Fatalf("status=%d want=%d", response.StatusCode, tc.status)
			}
			if got := read(); got.Projection.State != queue.StateAwaitingRuntime || len(got.Claims) != 0 {
				t.Fatal("denied binding mutated queue")
			}
		})
	}
	work := orchestration.WorkRequest{Authority: original.Authority, QueueItemID: input.QueueItemID, WorkerID: "distributed-worker", LoopExecutionID: "distributed-loop-execution", ClaimID: "distributed-claim", AttemptID: "distributed-attempt", ClaimTransitionID: "distributed-claimed", TerminalTransitionID: "distributed-terminal", DispositionID: "distributed-disposition", ArtifactID: "distributed-artifact", LeaseDuration: time.Minute}
	if _, err := svc.ProcessQueueItemAs(ctx, subject, work); err == nil {
		t.Fatal("workspace processed without runtime binding")
	}
	if view = read(); len(view.Claims) != 0 || view.Projection.State != queue.StateAwaitingRuntime {
		t.Fatal("denied handoff mutated queue")
	}
	replay()

	// Controller authentication, exact approval and session activation use the
	// same real application services as the existing API session test. The
	// synthetic executable's version string is not runtime acceptance evidence.
	var review core.Review
	apiRequest(t, client, http.MethodPost, "/v1/plans/preview", map[string]any{"agent": charter.AgentID, "revision": 1, "environment": core.Environment{Name: "local"}}, &review, http.StatusCreated)
	var approval core.Approval
	apiRequest(t, client, http.MethodPost, "/v1/approvals", map[string]any{"plan_id": review.Plan.ID, "ttl": "1m"}, &approval, http.StatusCreated)
	apiRequest(t, client, http.MethodPost, "/v1/approvals/"+approval.ID+"/decision", map[string]bool{"approve": true}, &approval, http.StatusOK)
	var receipt core.Receipt
	apiRequest(t, client, http.MethodPost, "/v1/provision", map[string]string{"plan_id": review.Plan.ID, "approval_id": approval.ID}, &receipt, http.StatusCreated)
	var preview struct {
		Mandate core.Mandate `json:"mandate"`
	}
	apiRequest(t, client, http.MethodPost, "/v1/sessions/preview", map[string]any{"agent": charter.AgentID, "revision": 1, "stanza": "principal", "environment": core.Environment{Name: "local"}}, &preview, http.StatusCreated)
	var session core.Session
	apiRequest(t, client, http.MethodPost, "/v1/sessions/start", map[string]string{"mandate_id": preview.Mandate.ID}, &session, http.StatusCreated)
	defer apiRequest(t, client, http.MethodPost, "/v1/sessions/"+session.ID+"/terminate", map[string]string{"reason": "distributed_test_complete"}, &map[string]string{}, http.StatusOK)
	authority, err := svc.FleetCommandAuthorityAs(ctx, subject)
	if err != nil {
		t.Fatal(err)
	}
	if authority.StanzaID != "principal" || authority.MandateID != preview.Mandate.ID {
		t.Fatal("controller resolved a different stanza or mandate")
	}
	bind.Authority = authority.Authority
	var bound struct {
		Binding queue.RuntimeBinding `json:"binding"`
		Created bool                 `json:"created"`
	}
	apiRequest(t, client, http.MethodPost, "/v1/queue/"+input.QueueItemID+"/bind-runtime", bind, &bound, http.StatusCreated)
	binding := bound.Binding
	if !bound.Created || binding.Authority != authority.Authority {
		t.Fatalf("controller runtime bind: %+v", bound)
	}
	if view = read(); view.Projection.State != queue.StateQueued || view.Item.Authority != original.Authority {
		t.Fatal("binding rewrote historical authority or failed to queue")
	}
	replay()
	work.Authority = authority.Authority
	// Persist a canonical abandoned claim fixture through the sole fleet store.
	// This qualifies lease recovery, not a real worker crash or Hermes execution.
	now := svc.Now()
	le, err := execution.NewLoopExecution(execution.LoopExecution{LoopExecutionID: work.LoopExecutionID, GraphRunID: input.GraphRunID, GraphNodeID: "work", Loop: lr, Participant: revision.Nodes[0].Participant, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	fact := fleet.AuditFact{Event: core.AuditEvent{Type: "fleet.test.fixture", SubjectID: input.QueueItemID, PrincipalID: subject.PrincipalID, Outcome: "succeeded", Reason: "abandoned claim fixture"}}
	if _, err = store.CreateLoopExecution(ctx, le, fact); err != nil {
		t.Fatal(err)
	}
	claim, err := queue.NewClaim(queue.Claim{ClaimID: "abandoned-claim", QueueItem: binding.QueueItem, AttemptID: "abandoned-attempt", WorkerID: "abandoned-worker", Authority: authority.Authority, ClaimedAt: now, ExpiresAt: now.Add(time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := execution.NewAttempt(execution.Attempt{AttemptID: claim.AttemptID, GraphRunID: input.GraphRunID, LoopExecutionID: le.LoopExecutionID, QueueItem: claim.QueueItem, ClaimID: claim.ClaimID, AttemptNumber: 1, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	transition, err := queue.NewTransition(queue.QueueTransition{TransitionID: "abandoned-transition", QueueItemID: input.QueueItemID, From: queue.StateQueued, To: queue.StateClaimed, ClaimID: claim.ClaimID, Reason: "abandoned fixture", OccurredAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.ClaimQueueItem(ctx, claim, attempt, transition, fact); err != nil {
		t.Fatal(err)
	}
	retryInput := app.RetryQueueItemInput{WorkspaceAgentID: agent.AgentID, QueueItemID: input.QueueItemID, RetryID: "distributed-reclaim", TransitionID: "distributed-reclaimed", Reclaimed: true}
	if _, err = svc.RetryQueueItemAs(ctx, subject, retryInput); err == nil {
		t.Fatal("live lease was reclaimed")
	}
	// Wait for this exact canonical lease, not for guessed server readiness.
	<-time.After(time.Until(claim.ExpiresAt) + time.Millisecond)
	var retried queue.Retry
	apiRequest(t, client, http.MethodPost, "/v1/queue/"+input.QueueItemID+"/retry", retryInput, &retried, http.StatusOK)
	if !retried.Reclaimed || retried.ClaimID != claim.ClaimID || retried.OccurredAt.Before(claim.ExpiresAt) {
		t.Fatal("reclaim lacks canonical expiry evidence")
	}
	replay()
	processed, err := svc.ProcessQueueItemAs(ctx, subject, work)
	if err == nil {
		t.Fatal("failed runtime must return a denied result")
	}
	if processed.Disposition.State != execution.StateFailed {
		view = read()
		t.Fatalf("NoKeyAdapter must persist failure: state=%q err=%v projection=%s claims=%d attempts=%d", processed.Disposition.State, err, view.Projection.State, len(view.Claims), len(view.Attempts))
	}
	view = read()
	if view.Disposition == nil || view.Disposition.State != execution.StateFailed || view.Projection.State != queue.StateFailed || len(view.Claims) != 2 || len(view.Attempts) != 2 || len(view.Retries) != 1 || len(view.LoopExecutions) != 1 || view.Artifact != nil {
		t.Fatal("failed runtime did not persist authoritative failure without invented output")
	}
	replay()
}

// Real-store service denial: historical authority references grant no runtime
// authority. The positive expiry/HTTP proof lives in TestDQHandoff above.
func TestDQHistoricalAuthorityDenied(t *testing.T) {
	svc := apiService(t)
	store := configureAPIFleet(t, svc)
	ctx := context.Background()
	subject, err := svc.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := json.Marshal(registry.CurrentFleetFixture{SchemaVersion: registry.CurrentFleetFixtureSchemaVersion, FleetID: "reclaim-fleet", Agents: []registry.CurrentFleetAgent{{SourceID: "reclaim-source", AgentID: "reclaim-agent", Runtime: registry.RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent", Target: "profile/reclaim"}, Ownership: registry.Ownership{OwnerID: "reclaim-owner", AccountabilityID: "reclaim-team"}, Lifecycle: registry.LifecycleEnabled, Charter: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "reclaim-agent", Revision: 1, Digest: "sha256:" + strings.Repeat("a", 64)}}}})
	if err != nil {
		t.Fatal(err)
	}
	agent, _, err := svc.RegisterFleetAgentAs(ctx, subject, app.NewRegisterFleetAgentInput(fixture, "reclaim-fleet", "reclaim-source"))
	if err != nil {
		t.Fatal(err)
	}
	accepted := storeAgentExecutionHistory(t, svc, store, agent.Revision)
	// The existing historical fixture has no admitted runtime authority.
	now := svc.Now()
	le, err := execution.NewLoopExecution(execution.LoopExecution{LoopExecutionID: "reclaim-loop", GraphRunID: accepted.GraphRun.GraphRunID, GraphNodeID: "work", Loop: accepted.Snapshot.Loops[0], Participant: accepted.Snapshot.Participants[0], CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	fact := fleet.AuditFact{Event: core.AuditEvent{Type: "fleet.test.fixture", SubjectID: accepted.QueueItem.ItemID, PrincipalID: subject.PrincipalID, Outcome: "succeeded", Reason: "canonical expired claim test fixture"}}
	if _, err = store.CreateLoopExecution(ctx, le, fact); err != nil {
		t.Fatal(err)
	}
	claim, err := queue.NewClaim(queue.Claim{ClaimID: "reclaim-claim", QueueItem: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: accepted.QueueItem.ItemID, Digest: accepted.QueueItem.Digest}, AttemptID: "reclaim-attempt", WorkerID: "reclaim-worker", Authority: accepted.Submission.Authority, ClaimedAt: now, ExpiresAt: now.Add(time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := execution.NewAttempt(execution.Attempt{AttemptID: claim.AttemptID, GraphRunID: le.GraphRunID, LoopExecutionID: le.LoopExecutionID, QueueItem: claim.QueueItem, ClaimID: claim.ClaimID, AttemptNumber: 1, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	transition, err := queue.NewTransition(queue.QueueTransition{TransitionID: "reclaim-claimed", QueueItemID: accepted.QueueItem.ItemID, From: queue.StateQueued, To: queue.StateClaimed, ClaimID: claim.ClaimID, Reason: "fixture claim", OccurredAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.ClaimQueueItem(ctx, claim, attempt, transition, fact); err != nil {
		t.Fatal(err)
	}
	_, err = svc.RetryQueueItemAs(ctx, subject, app.RetryQueueItemInput{QueueItemID: accepted.QueueItem.ItemID, Authority: accepted.Submission.Authority, RetryID: "reclaim-retry", TransitionID: "reclaim-transition", Reclaimed: true})
	if err == nil {
		t.Fatal("unadmitted historical authority reclaimed work")
	}
	projection, err := store.GetQueueProjection(ctx, accepted.QueueItem.ItemID)
	if err != nil || projection.State != queue.StateClaimed || projection.Attempts != 1 {
		t.Fatal("denied reclaim changed canonical claim")
	}
}
