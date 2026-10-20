package doerbinding

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
)

func bindingFixture(t *testing.T) (loop.LoopRevision, graph.Node, []graph.NormalizedInput) {
	t.Helper()
	r, _, err := loop.NewDoerReusableRevision("doer", 1, "", loop.DoerReusableContract{MaxAttempts: 2})
	if err != nil {
		t.Fatal(err)
	}
	n := graph.Node{ID: "node", Loop: loopReference(r), Participant: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "agent", Revision: 1, Digest: r.Digest}}
	for _, p := range r.Inputs {
		n.Inputs = append(n.Inputs, graph.Port{ID: p.ID, Type: graph.ValueType(p.Type), Required: p.Required})
	}
	for _, p := range r.Outputs {
		n.Outputs = append(n.Outputs, graph.Port{ID: p.ID, Type: graph.ValueType(p.Type), Required: p.Required})
	}
	values := map[string]string{"task": `"Create result"`, "workspace": `"/workspace"`, "writable_files": `["result.txt"]`, "verify_file": `"result.txt"`, "expected_text": `"hello"`}
	var inputs []graph.NormalizedInput
	for _, p := range n.Inputs {
		inputs = append(inputs, graph.NormalizedInput{PortID: p.ID, Type: p.Type, Value: json.RawMessage(values[p.ID])})
	}
	return r, n, inputs
}

func directFixture(t *testing.T, n graph.Node, inputs []graph.NormalizedInput) graph.GraphRunSnapshot {
	t.Helper()
	e, err := graph.NewDirectLoopSnapshot("snapshot", n, inputs)
	if err != nil {
		t.Fatal(err)
	}
	s, err := graph.NewDirectRunSnapshot(e)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestDirectBindingAndGraphParity(t *testing.T) {
	r, n, inputs := bindingFixture(t)
	for _, optional := range []bool{false, true} {
		t.Run(map[bool]string{false: "without_optional", true: "with_optional"}[optional], func(t *testing.T) {
			var values []graph.NormalizedInput
			for _, v := range inputs {
				if optional || v.PortID != "expected_text" {
					values = append(values, v)
				}
			}
			s := directFixture(t, n, values)
			before, err := graph.MarshalRunSnapshot(s)
			if err != nil {
				t.Fatal(err)
			}
			bound, err := BindDoerDirectRun(r, n, s)
			if err != nil {
				t.Fatal(err)
			}
			if bound.Task != "Create result" || bound.VerifyFile != "result.txt" || bound.MaxAttempts != 2 || (bound.ExpectedText != nil) != optional {
				t.Fatalf("bad binding: %+v", bound)
			}
			var mappings []graph.InputMapping
			for _, p := range n.Inputs {
				mappings = append(mappings, graph.InputMapping{GraphInput: p.ID, ToNodeID: n.ID, ToPort: p.ID})
			}
			g, _, err := graph.NewRevision(graph.GraphRevision{GraphID: "graph", Revision: 1, Inputs: n.Inputs, Nodes: []graph.Node{n}, InputMappings: mappings})
			if err != nil {
				t.Fatal(err)
			}
			gs, err := graph.NewRunSnapshot("graph-snapshot", g, values)
			if err != nil {
				t.Fatal(err)
			}
			wire, err := graph.MarshalRunSnapshot(gs)
			if err != nil {
				t.Fatal(err)
			}
			other, err := BindDoerGraphRun(r, g, g.Nodes[0], gs)
			if err != nil || !reflect.DeepEqual(bound, other) {
				t.Fatalf("Graph parity: %+v %v", other, err)
			}
			after, _ := graph.MarshalRunSnapshot(gs)
			if string(wire) != string(after) {
				t.Fatal("historical Graph snapshot changed")
			}
			after, _ = graph.MarshalRunSnapshot(s)
			if string(before) != string(after) {
				t.Fatal("direct snapshot changed")
			}
			if _, err := BindDoerDirectRun(r, n, gs); err == nil {
				t.Fatal("Graph source accepted as direct")
			}
			if _, err := BindDoerGraphRun(r, g, n, s); err == nil {
				t.Fatal("direct source accepted as Graph")
			}
		})
	}
}

func TestDirectBindingRejectsTampering(t *testing.T) {
	cases := map[string]func(*loop.LoopRevision, *graph.Node, *graph.GraphRunSnapshot){
		"envelope_schema": func(_ *loop.LoopRevision, _ *graph.Node, s *graph.GraphRunSnapshot) { s.SchemaVersion = "unknown" },
		"nested_schema": func(_ *loop.LoopRevision, _ *graph.Node, s *graph.GraphRunSnapshot) {
			s.Execution.SchemaVersion = "unknown"
		},
		"digest": func(_ *loop.LoopRevision, _ *graph.Node, s *graph.GraphRunSnapshot) { s.Digest = "sha256:bad" },
		"nested_digest": func(_ *loop.LoopRevision, _ *graph.Node, s *graph.GraphRunSnapshot) {
			s.Execution.Digest = "sha256:bad"
		},
		"source": func(_ *loop.LoopRevision, _ *graph.Node, s *graph.GraphRunSnapshot) {
			s.Execution.SourceKind = graph.SourcePublishedGraph
		},
		"graph_claim":          func(_ *loop.LoopRevision, n *graph.Node, s *graph.GraphRunSnapshot) { s.Graph = n.Loop },
		"missing_execution":    func(_ *loop.LoopRevision, _ *graph.Node, s *graph.GraphRunSnapshot) { s.Execution = nil },
		"loop_id":              func(_ *loop.LoopRevision, n *graph.Node, _ *graph.GraphRunSnapshot) { n.Loop.ID = "other" },
		"loop_revision":        func(_ *loop.LoopRevision, n *graph.Node, _ *graph.GraphRunSnapshot) { n.Loop.Revision++ },
		"loop_ref_schema":      func(_ *loop.LoopRevision, n *graph.Node, _ *graph.GraphRunSnapshot) { n.Loop.SchemaVersion = "unknown" },
		"participant":          func(_ *loop.LoopRevision, n *graph.Node, _ *graph.GraphRunSnapshot) { n.Participant.ID = "other" },
		"participant_revision": func(_ *loop.LoopRevision, n *graph.Node, _ *graph.GraphRunSnapshot) { n.Participant.Revision++ },
		"participant_digest": func(_ *loop.LoopRevision, n *graph.Node, _ *graph.GraphRunSnapshot) {
			n.Participant.Digest = "sha256:bad"
		},
		"node": func(_ *loop.LoopRevision, n *graph.Node, _ *graph.GraphRunSnapshot) { n.ID = "other" },
		"interface": func(_ *loop.LoopRevision, n *graph.Node, _ *graph.GraphRunSnapshot) {
			n.Inputs[0].Required = !n.Inputs[0].Required
		},
		"loop_content": func(r *loop.LoopRevision, _ *graph.Node, _ *graph.GraphRunSnapshot) { r.DoerReusable.MaxAttempts = 3 },
		"extra_input": func(_ *loop.LoopRevision, _ *graph.Node, s *graph.GraphRunSnapshot) {
			s.Inputs = append(s.Inputs, graph.NormalizedInput{PortID: "extra", Type: graph.TypeString, Value: json.RawMessage(`"x"`)})
		},
		"duplicate_input": func(_ *loop.LoopRevision, _ *graph.Node, s *graph.GraphRunSnapshot) {
			s.Inputs = append(s.Inputs, s.Inputs[0])
		},
		"wrong_type": func(_ *loop.LoopRevision, _ *graph.Node, s *graph.GraphRunSnapshot) {
			s.Inputs[0].Type = graph.TypeBoolean
		},
		"wrong_value": func(_ *loop.LoopRevision, _ *graph.Node, s *graph.GraphRunSnapshot) {
			s.Inputs[0].Value = json.RawMessage(`true`)
		},
		"mapping": func(_ *loop.LoopRevision, _ *graph.Node, s *graph.GraphRunSnapshot) {
			s.Execution.Topology.InputMappings[0].ToPort = "other"
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r, n, values := bindingFixture(t)
			s := directFixture(t, n, values)
			mutate(&r, &n, &s)
			if _, err := BindDoerDirectRun(r, n, s); err == nil {
				t.Fatal("tampering accepted")
			}
		})
	}
}

func TestDirectBindingRejectsResealedInterfaceSubstitution(t *testing.T) {
	r, n, values := bindingFixture(t)
	n.Inputs[0].Required = !n.Inputs[0].Required
	s := directFixture(t, n, values)
	if _, err := BindDoerDirectRun(r, n, s); err == nil {
		t.Fatal("self-consistent false Loop interface accepted")
	}
}

func TestDirectBindingRejectsResealedInvalidTask(t *testing.T) {
	r, n, values := bindingFixture(t)
	for i := range values {
		if values[i].PortID == "verify_file" {
			values[i].Value = json.RawMessage(`"../escape"`)
		}
	}
	s := directFixture(t, n, values)
	if _, err := BindDoerDirectRun(r, n, s); err == nil {
		t.Fatal("invalid normalized Doer contract accepted")
	}
}
