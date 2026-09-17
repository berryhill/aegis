package api

import (
	"context"
	"encoding/json"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	"net/http"
	"strings"
	"testing"
)

// Uses real authenticated Unix HTTP and isolated fleet persistence. No runtime,
// credentials, provider, activation or Queue submission is involved.
func TestImplementationDraftAuthenticatedPublishReadback(t *testing.T) {
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
	fixture, err := json.Marshal(registry.CurrentFleetFixture{
		SchemaVersion: registry.CurrentFleetFixtureSchemaVersion, FleetID: "skill-proof-fleet",
		Agents: []registry.CurrentFleetAgent{{SourceID: "skill-proof-source", AgentID: "skill-proof-agent",
			Runtime:   registry.RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent", Target: "profile/skill-proof"},
			Ownership: registry.Ownership{OwnerID: "skill-proof-owner", AccountabilityID: "skill-proof-team"}, Lifecycle: registry.LifecycleEnabled,
			Charter: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "skill-proof-agent", Revision: 1, Digest: "sha256:" + strings.Repeat("a", 64)},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var registered struct {
		Agent   app.FleetAgent `json:"agent"`
		Created bool           `json:"created"`
	}
	apiRequest(t, client, http.MethodPost, "/v1/agents", app.NewRegisterFleetAgentInput(fixture, "skill-proof-fleet", "skill-proof-source"), &registered, http.StatusCreated)
	agent := registered.Agent.Revision

	contract := loop.ImplementationDraft("implement addition", "native Go tests pass")
	contract.Policy.RequiredTests = []loop.RequiredGoTest{{Package: "synthetic", Name: "TestValue"}}
	contract.Workspace = t.TempDir()
	contract.WritableFiles = []string{"sum.go"}
	revision, _, err := loop.NewImplementationRevision("implementation-draft", 1, "", contract)
	if err != nil {
		t.Fatal(err)
	}
	var published app.PublishedLoop
	apiRequest(t, client, http.MethodPost, "/v1/loops", app.PublishLoopInput{AgentID: agent.AgentID, Revision: revision, IdempotencyKey: "implementation-draft"}, &published, http.StatusCreated)
	reloaded, err := svc.FleetRepository.GetLoopRevision(context.Background(), revision.LoopID, revision.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Digest != revision.Digest || published.Revision.Digest != revision.Digest {
		t.Fatal("immutable readback mismatch")
	}
	wire, _ := loop.MarshalRevision(reloaded)
	expected, _ := loop.MarshalRevision(revision)
	if string(wire) != string(expected) {
		t.Fatal("contract changed in persistence")
	}
	var items []app.QueueExecutionView
	apiRequest(t, client, http.MethodGet, "/v1/queue", nil, &items, http.StatusOK)
	if len(items) != 0 {
		t.Fatal("draft publication submitted queue work")
	}
}
