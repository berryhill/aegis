package consoleweb

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

func TestGraphConsolePublicationUsesExactServerResolvedBindings(t *testing.T) {
	digest := func(marker string) string { return "sha256:" + strings.Repeat(marker, 64) }
	agent := registry.AgentRevision{AgentID: "agent-1", Revision: 3, Digest: digest("a"), Lifecycle: registry.LifecycleEnabled}
	loopRevision := loop.LoopRevision{LoopID: "loop-1", Revision: 4, Digest: digest("b"), Inputs: []loop.Port{{ID: "request", Type: loop.TypeString, Required: true}}, Outputs: []loop.Port{{ID: "result", Type: loop.TypeArtifact, Required: true}}}
	surface := app.FleetSurface{Agents: []app.FleetAgent{{Revision: agent}}, Loops: []app.LoopView{{Revision: loopRevision, Lifecycle: loop.Lifecycle{LoopID: loopRevision.LoopID, State: loop.LifecycleActive, ActiveRevision: loopRevision.Revision, ActiveDigest: loopRevision.Digest}}}}
	values := url.Values{
		"csrf": {"token"}, "authority_session_id": {"session-1"}, "graph_id": {"graph-1"}, "revision": {"1"}, "idempotency_key": {"publish-1"},
		"input_id_1": {"request"}, "input_type_1": {"string"}, "input_required_1": {"true"},
		"output_id_1": {"result"}, "output_type_1": {"artifact"}, "output_required_1": {"true"},
		"node_id_1": {"work"}, "node_agent_1": {agent.Digest}, "node_loop_1": {loopRevision.Digest},
		"input_map_graph_1": {"request"}, "input_map_node_1": {"work"}, "input_map_port_1": {"request"},
		"output_map_node_1": {"work"}, "output_map_port_1": {"result"}, "output_map_graph_1": {"result"},
	}
	authority := reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: "authority-1", Digest: digest("c")}
	input, err := ParseGraphPublication(values, surface, authority)
	if err != nil {
		t.Fatal(err)
	}
	if len(input.Revision.Nodes) != 1 || input.Revision.Nodes[0].Participant.Digest != agent.Digest || input.Revision.Nodes[0].Loop.Digest != loopRevision.Digest || len(input.Revision.Nodes[0].Inputs) != 1 || input.Revision.Nodes[0].Inputs[0].ID != "request" {
		t.Fatalf("publication did not preserve exact server-resolved bindings: %+v", input.Revision)
	}
	values.Set("node_loop_1", digest("f"))
	if _, err = ParseGraphPublication(values, surface, authority); err == nil {
		t.Fatal("unavailable mutable/latest-style Loop selection was accepted")
	}
}

func TestGraphConsoleSubmissionPreservesTypedFailuresForDurableAdmission(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	revision := graph.GraphRevision{GraphID: "graph-1", Revision: 2, Digest: digest, Inputs: []graph.Port{{ID: "count", Type: graph.TypeInteger, Required: true}}}
	authority := reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: "authority-1", Digest: "sha256:" + strings.Repeat("b", 64)}
	values := url.Values{"csrf": {"token"}, "authority_session_id": {"session-1"}, "graph_id": {revision.GraphID}, "graph_revision": {"2"}, "graph_digest": {digest}, "idempotency_key": {"submit-1"}, "max_attempts": {"2"}, "input.count": {"not-json"}, "input.unknown": {"value"}}
	input, err := ParseGraphSubmission(values, revision, authority)
	if err != nil {
		t.Fatal(err)
	}
	if len(input.Inputs) != 2 || string(input.Inputs[0].Value) != "not-json" || input.QueueItemID == "" || input.SubmissionID == "" {
		t.Fatalf("typed failures were not preserved for durable fail-closed admission: %+v", input)
	}
	replay, err := ParseGraphSubmission(values, revision, authority)
	if err != nil || replay.QueueItemID != input.QueueItemID || replay.SubmissionID != input.SubmissionID {
		t.Fatalf("idempotent replay identities changed: first=%+v replay=%+v err=%v", input, replay, err)
	}
	values.Set("idempotency_key", "submit-2")
	changed, err := ParseGraphSubmission(values, revision, authority)
	if err != nil || changed.QueueItemID == input.QueueItemID {
		t.Fatalf("different idempotency key reused immutable identity: %+v err=%v", changed, err)
	}
}

func TestGraphConsoleFormRejectsDuplicateControlFieldsButCarriesDuplicateInputs(t *testing.T) {
	request := httptest.NewRequest("POST", "/console/graphs/submit", strings.NewReader("csrf=one&csrf=two"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if _, err := DecodeGraphConsoleForm(request); err == nil {
		t.Fatal("duplicate CSRF control was accepted")
	}
	request = httptest.NewRequest("POST", "/console/graphs/submit", strings.NewReader("input.value=one&input.value=two"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	values, err := DecodeGraphConsoleForm(request)
	if err != nil || len(values["input.value"]) != 2 {
		t.Fatalf("duplicate typed inputs were not preserved for domain rejection: values=%v err=%v", values, err)
	}
}
