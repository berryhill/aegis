package api

import (
	"context"
	"encoding/json"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestExactLoopQueueMissingAuthority(t *testing.T) {
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
	_, err := svc.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}

	// Use the existing API execution fixture's exact principal selector, with
	// fixture-only provider configuration and no newly invented authority.
	charter := core.Charter{SchemaVersion: core.SchemaVersion, AgentID: "distributed-agent", Name: "Distributed test", Revision: 1,
		Runtime: core.RuntimeConstraint{Adapter: "hermes", Runtime: "hermes-agent", VersionConstraint: ">=0.18.0", Target: "aegis-owned-ephemeral"},
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
	input := app.QueueLoopInput{Agent: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: agent.AgentID, Revision: agent.Revision, Digest: agent.Digest}, Loop: lr, IdempotencyKey: "exact-loop-test", Inputs: []graph.NormalizedInput{{PortID: "value", Type: graph.TypeString, Value: json.RawMessage(`"proof"`)}}}
	var got app.QueueLoopResult
	apiRequest(t, client, http.MethodPost, "/v1/loops/queue", input, &got, http.StatusOK)
	if got.Reason != "provisioning_receipt_missing" || got.Execution == nil || got.Execution.Projection.State != queue.StatePreparationPending || len(got.Execution.Attempts) != 0 {
		t.Fatalf("unexpected result: %+v", got)
	}
	var replay app.QueueLoopResult
	apiRequest(t, client, http.MethodPost, "/v1/loops/queue", input, &replay, http.StatusOK)
	if replay.QueueItemID != got.QueueItemID {
		t.Fatal("duplicate execution")
	}
}
