package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

// Domain contract: workspace publication and submission must be usable through
// public typed adapters and real isolated persistence. An awaiting-runtime item
// is valid historical work, not a terminal execution missing its disposition.
// This is service/HTTP evidence, not a laptop or installed-agent runtime proof.
func TestWorkspaceSkillSubmissionPublicReadback(t *testing.T) {
	t.Run("single_node", func(t *testing.T) { workspaceSkillSubmissionPublicReadback(t, false) })
	t.Run("multi_node", func(t *testing.T) { workspaceSkillSubmissionPublicReadback(t, true) })
}

func workspaceSkillSubmissionPublicReadback(t *testing.T, multiNode bool) {
	t.Helper()
	svc := apiService(t)
	configureAPIFleet(t, svc)
	probe := &skillParticipantIntegrityProbe{Repository: svc.FleetRepository, multiNode: multiNode}
	svc.FleetRepository = probe
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
	form, err := decodeLoopComposerForm(composerRequest(validLoopComposerValues()))
	if err != nil {
		t.Fatal(err)
	}
	var published app.PublishedLoop
	apiRequest(t, client, http.MethodPost, "/v1/loops", app.PublishLoopInput{AgentID: agent.AgentID, Revision: form.Revision, IdempotencyKey: "skill-proof-loop"}, &published, http.StatusCreated)
	loopRef := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: published.Revision.LoopID, Revision: published.Revision.Revision, Digest: published.Revision.Digest}
	var activated app.LoopLifecycleResult
	apiRequest(t, client, http.MethodPut, "/v1/loops/"+loopRef.ID+"/lifecycle", app.SetLoopLifecycleInput{AgentID: agent.AgentID, Loop: loopRef, State: loop.LifecycleActive, EventID: "skill-proof-activate"}, &activated, http.StatusCreated)
	value := graph.Port{ID: "value", Type: graph.TypeString, Required: true}
	result := graph.Port{ID: "result", Type: graph.TypeString, Required: true}
	revision := graph.GraphRevision{GraphID: "skill-proof-graph", Revision: 1, Inputs: []graph.Port{value}, Outputs: []graph.Port{result},
		Nodes:         []graph.Node{{ID: "work", Participant: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: agent.AgentID, Revision: agent.Revision, Digest: agent.Digest}, Loop: loopRef, Inputs: []graph.Port{value}, Outputs: []graph.Port{result}}},
		InputMappings: []graph.InputMapping{{GraphInput: "value", ToNodeID: "work", ToPort: "value"}}, OutputMappings: []graph.OutputMapping{{FromNodeID: "work", FromPort: "result", GraphOutput: "result"}},
	}
	if multiNode {
		second := revision.Nodes[0]
		second.ID = "second"
		revision.Nodes = append(revision.Nodes, second)
		revision.InputMappings = append(revision.InputMappings, graph.InputMapping{GraphInput: "value", ToNodeID: "second", ToPort: "value"})
	}
	var graphPublished app.PublishedGraph
	apiRequest(t, client, http.MethodPost, "/v1/graphs", app.PublishGraphInput{AgentID: agent.AgentID, Revision: revision, IdempotencyKey: "skill-proof-graph"}, &graphPublished, http.StatusCreated)
	var decision orchestration.SubmissionDecision
	input := app.SubmitGraphInput{WorkspaceAgentID: agent.AgentID, Graph: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: graphPublished.Revision.GraphID, Revision: graphPublished.Revision.Revision, Digest: graphPublished.Revision.Digest}, Inputs: []graph.NormalizedInput{{PortID: "value", Type: graph.TypeString, Value: json.RawMessage(`"skill proof"`)}}, SubmissionID: "skill-proof-submission", IdempotencyKey: "skill-proof-submit", SnapshotID: "skill-proof-snapshot", QueueItemID: "skill-proof-queue", GraphRunID: "skill-proof-run", TransitionID: "skill-proof-transition", RejectionID: "skill-proof-rejection", MaxAttempts: 1}
	t.Run("invented_field_denied_before_mutation", func(t *testing.T) {
		encoded, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		var malformed map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &malformed); err != nil {
			t.Fatal(err)
		}
		// Regression for the operator trace: this is not a supported field.
		malformed["rejection_idempotency_key"] = json.RawMessage(`"invented-key"`)
		apiRequest(t, client, http.MethodPost, "/v1/queue", malformed, nil, http.StatusBadRequest)
		var items []app.QueueExecutionView
		apiRequest(t, client, http.MethodGet, "/v1/queue", nil, &items, http.StatusOK)
		if len(items) != 0 {
			t.Fatal("malformed submission created Queue work")
		}
	})
	apiRequest(t, client, http.MethodPost, "/v1/queue", input, &decision, http.StatusCreated)
	if decision.Accepted == nil || decision.Accepted.InitialTransition.To != queue.StateAwaitingRuntime {
		t.Fatal("workspace submission did not enter awaiting-runtime")
	}
	t.Run("same_intent_replay", func(t *testing.T) {
		var replay orchestration.SubmissionDecision
		apiRequest(t, client, http.MethodPost, "/v1/queue", input, &replay, http.StatusOK)
		if replay.Created || replay.Accepted == nil || replay.Rejection != nil {
			t.Fatal("same-intent replay did not recover the accepted submission")
		}
		if replay.Accepted.Submission.Digest != decision.Accepted.Submission.Digest ||
			replay.Accepted.Snapshot.Digest != decision.Accepted.Snapshot.Digest ||
			replay.Accepted.QueueItem.Digest != decision.Accepted.QueueItem.Digest {
			t.Fatal("same-intent replay changed immutable submission identities")
		}
	})
	t.Run("same_key_changed_inputs_denied", func(t *testing.T) {
		changed := input
		changed.Inputs = []graph.NormalizedInput{{PortID: "value", Type: graph.TypeString, Value: json.RawMessage(`"different intent"`)}}
		apiRequest(t, client, http.MethodPost, "/v1/queue", changed, nil, http.StatusConflict)
	})
	t.Run("list", func(t *testing.T) {
		var items []app.QueueExecutionView
		apiRequest(t, client, http.MethodGet, "/v1/queue", nil, &items, http.StatusOK)
		if len(items) != 1 || items[0].Projection.QueueItemID != input.QueueItemID {
			t.Fatal("submission recovery did not preserve exactly one Queue item")
		}
	})
	t.Run("show", func(t *testing.T) {
		var item app.QueueExecutionView
		apiRequest(t, client, http.MethodGet, "/v1/queue/skill-proof-queue", nil, &item, http.StatusOK)
		if item.Projection.State != queue.StateAwaitingRuntime || item.Disposition != nil || len(item.Claims) != 0 || len(item.Attempts) != 0 {
			t.Fatal("historical readback invented execution or disposition")
		}
		if len(item.NodeRuntimes) != len(revision.Nodes) {
			t.Fatal("readback omitted per-node runtime provenance")
		}
		for _, node := range revision.Nodes {
			if item.NodeRuntimes[node.ID] != agent.Runtime {
				t.Fatal("incorrect node runtime provenance")
			}
		}
		if multiNode && item.Runtime != (registry.RuntimeBinding{}) {
			t.Fatal("multi-node readback claimed a single representative runtime")
		}
	})
	t.Run("participant_integrity_denied", func(t *testing.T) {
		probe.enabled.Store(true)
		defer probe.enabled.Store(false)
		for _, path := range []string{"/v1/queue", "/v1/queue/skill-proof-queue"} {
			probe.calls.Store(0)
			apiRequest(t, client, http.MethodGet, path, nil, nil, http.StatusServiceUnavailable)
		}
	})
}

// Inject corrupt read evidence, not a second store writer. In the multi-node
// case only the second participant lookup is damaged, proving all nodes are
// checked rather than merely dropping the old single-node cardinality guard.
type skillParticipantIntegrityProbe struct {
	fleet.Repository
	multiNode bool
	enabled   atomic.Bool
	calls     atomic.Int32
}

func (p *skillParticipantIntegrityProbe) GetAgentRevision(ctx context.Context, id string, revision uint64) (registry.AgentRevision, error) {
	value, err := p.Repository.GetAgentRevision(ctx, id, revision)
	if p.enabled.Load() && (!p.multiNode || p.calls.Add(1) == 2) {
		value.Digest = "sha256:" + strings.Repeat("f", 64)
	}
	return value, err
}
