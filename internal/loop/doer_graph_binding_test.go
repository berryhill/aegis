package loop_test

import (
	"encoding/json"
	"testing"

	"github.com/berryhill/aegis/internal/doerbinding"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
)

func TestBindDoerGraphRunExactMappedInputs(t *testing.T) {
	revision, _, err := loop.NewDoerReusableRevision("doer", 1, "", loop.DoerReusableContract{MaxAttempts: 2})
	if err != nil {
		t.Fatal(err)
	}
	ports := []graph.Port{}
	mappings := []graph.InputMapping{}
	inputs := []graph.NormalizedInput{}
	values := map[string]string{"task": `"Create result"`, "workspace": `"/workspace"`, "writable_files": `["result.txt"]`, "verify_file": `"result.txt"`}
	for _, port := range revision.Inputs {
		if !port.Required {
			continue
		}
		ports = append(ports, graph.Port{ID: port.ID, Type: graph.ValueType(port.Type), Required: true})
		mappings = append(mappings, graph.InputMapping{GraphInput: port.ID, ToNodeID: "node", ToPort: port.ID})
		inputs = append(inputs, graph.NormalizedInput{PortID: port.ID, Type: graph.ValueType(port.Type), Value: json.RawMessage(values[port.ID])})
	}
	node := graph.Node{ID: "node", Loop: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: revision.LoopID, Revision: 1, Digest: revision.Digest}, Participant: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "agent", Revision: 1, Digest: revision.Digest}, Inputs: ports}
	definition, _, err := graph.NewRevision(graph.GraphRevision{GraphID: "graph", Revision: 1, Inputs: ports, Nodes: []graph.Node{node}, InputMappings: mappings})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := graph.NewRunSnapshot("snapshot", definition, inputs)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := doerbinding.BindDoerGraphRun(revision, definition, definition.Nodes[0], snapshot)
	if err != nil || bound.Task != "Create result" || bound.VerifyFile != "result.txt" {
		t.Fatalf("exact binding: %+v %v", bound, err)
	}
	otherInputs := append([]graph.NormalizedInput(nil), inputs...)
	otherInputs[0].Value = json.RawMessage(`"Create different result"`)
	other, err := graph.NewRunSnapshot("another-snapshot", definition, otherInputs)
	if err != nil {
		t.Fatal(err)
	}
	otherBound, err := doerbinding.BindDoerGraphRun(revision, definition, definition.Nodes[0], other)
	if err != nil {
		t.Fatal(err)
	}
	firstDigest, _ := bound.Digest()
	otherDigest, _ := otherBound.Digest()
	if firstDigest == otherDigest {
		t.Fatal("different runs share an authorization digest")
	}
	withOptional := definition
	withOptional.Inputs = append([]graph.Port(nil), definition.Inputs...)
	withOptional.Inputs = append(withOptional.Inputs, graph.Port{ID: "expected_text", Type: graph.TypeString})
	withOptional.Nodes = append([]graph.Node(nil), definition.Nodes...)
	withOptional.Nodes[0].Inputs = append(append([]graph.Port(nil), ports...), graph.Port{ID: "expected_text", Type: graph.TypeString})
	withOptional.InputMappings = append(append([]graph.InputMapping(nil), definition.InputMappings...), graph.InputMapping{GraphInput: "expected_text", ToNodeID: "node", ToPort: "expected_text"})
	withOptional, _, err = graph.NewRevision(graph.GraphRevision{GraphID: "graph-optional", Revision: 1, Inputs: withOptional.Inputs, Nodes: withOptional.Nodes, InputMappings: withOptional.InputMappings})
	if err != nil {
		t.Fatal(err)
	}
	optionalSnapshot, err := graph.NewRunSnapshot("optional-snapshot", withOptional, inputs)
	if err != nil {
		t.Fatal(err)
	}
	optionalBound, err := doerbinding.BindDoerGraphRun(revision, withOptional, withOptional.Nodes[0], optionalSnapshot)
	if err != nil || optionalBound.ExpectedText != nil {
		t.Fatalf("absent optional input: %+v %v", optionalBound, err)
	}
	wrong := snapshot
	wrong.Inputs = append([]graph.NormalizedInput(nil), snapshot.Inputs...)
	wrong.Inputs[0].Value = json.RawMessage(`"different"`)
	if _, err := doerbinding.BindDoerGraphRun(revision, definition, definition.Nodes[0], wrong); err == nil {
		t.Fatal("modified immutable snapshot accepted")
	}
	for _, mutation := range []func(*graph.GraphRevision){
		func(g *graph.GraphRevision) { g.InputMappings[0].ToPort = "unknown" },
		func(g *graph.GraphRevision) { g.InputMappings[0].ToNodeID = "other" },
		func(g *graph.GraphRevision) { g.InputMappings[0].GraphInput = "unknown" },
	} {
		changed := definition
		changed.InputMappings = append([]graph.InputMapping(nil), definition.InputMappings...)
		mutation(&changed)
		if _, err := doerbinding.BindDoerGraphRun(revision, changed, changed.Nodes[0], snapshot); err == nil {
			t.Fatal("invalid mapping accepted")
		}
	}
}
