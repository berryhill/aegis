package graph

import (
	"bytes"
	"testing"
)

func producerConsumerCandidate() GraphRevision {
	r := graphCandidate()
	r.Nodes[0].Outputs[0].Type = TypeArtifact
	r.Nodes[1].Inputs[0].Type = TypeArtifact
	r.Nodes[1].Participant = r.Nodes[0].Participant
	return r
}

func producerConsumerFixture(t *testing.T, text string) (GraphRevision, GraphRunSnapshot) {
	t.Helper()
	r, _, err := NewRevision(producerConsumerCandidate())
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewRunSnapshot("chain-snapshot", r, []NormalizedInput{{PortID: "value", Type: TypeString, Value: []byte(`"` + text + `"`)}})
	if err != nil {
		t.Fatal(err)
	}
	return r, s
}

func TestProducerConsumerPlanResolvesExactSnapshotNotNodeOrder(t *testing.T) {
	for _, text := range []string{"hello", "github:berryhill/aegis#317"} {
		t.Run(text, func(t *testing.T) {
			r, s := producerConsumerFixture(t, text)
			p, err := ResolveProducerConsumerPlan(s, r)
			if err != nil {
				t.Fatal(err)
			}
			if p.Producer.ID != "review" || p.Consumer.ID != "publish" || p.Edge.ID != "review-before-publish" || len(p.ProducerInputs) != 1 || len(p.ConsumerInputs) != 0 || !bytes.Equal(p.ProducerInputs[0].Value, []byte(`"`+text+`"`)) {
				t.Fatalf("wrong plan: %+v", p)
			}
			// Canonical sorting puts publish before review. Edge direction, not slice
			// position or a task string, determines the one supported execution order.
			if r.Nodes[0].ID != "publish" {
				t.Fatal("fixture must expose ordering trap")
			}
			p.Producer.Inputs[0].ID = "changed"
			p.Edge.Mappings[0].ToPort = "changed"
			p.ProducerInputs[0].Value[0] = '!'
			if _, err = ResolveProducerConsumerPlan(s, r); err != nil {
				t.Fatalf("returned plan aliases immutable source: %v", err)
			}
		})
	}
}

func TestProducerConsumerPlanRejectsOtherShapesAndSnapshotSubstitution(t *testing.T) {
	tests := map[string]func(*GraphRevision){
		"string edge": func(r *GraphRevision) {
			r.Nodes[0].Outputs[0].Type = TypeString
			r.Nodes[1].Inputs[0].Type = TypeString
		},
		"foreign participant": func(r *GraphRevision) { r.Nodes[1].Participant = exactRevision("foreign", 1, "e") },
		"parallel nodes": func(r *GraphRevision) {
			r.Dependencies = nil
			r.Inputs = append(r.Inputs, Port{ID: "other", Type: TypeArtifact, Required: false})
			r.Nodes[1].Inputs[0].Required = false
		},
		"producer graph result": func(r *GraphRevision) {
			r.OutputMappings[0].FromNodeID = "review"
			r.OutputMappings[0].FromPort = "draft"
		},
		"additional node": func(r *GraphRevision) { n := r.Nodes[0]; n.ID = "third"; n.Inputs = nil; r.Nodes = append(r.Nodes, n) },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			c := producerConsumerCandidate()
			mutate(&c)
			r, _, err := NewRevision(c)
			if err != nil {
				t.Fatal("negative fixture must be structurally valid:", err)
			}
			s, err := NewRunSnapshot("negative", r, []NormalizedInput{{PortID: "value", Type: TypeString, Value: []byte(`"hello"`)}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = ResolveProducerConsumerPlan(s, r); err == nil {
				t.Fatal("unsupported chain accepted")
			}
		})
	}
	r, s := producerConsumerFixture(t, "hello")
	s.Inputs[0].Value = []byte(`"github:berryhill/aegis#317"`)
	if _, err := ResolveProducerConsumerPlan(s, r); err == nil {
		t.Fatal("changed snapshot accepted")
	}
	r, s = producerConsumerFixture(t, "hello")
	r.Nodes[0].Loop = exactRevision("substitute", 1, "f")
	if _, err := ResolveProducerConsumerPlan(s, r); err == nil {
		t.Fatal("changed definition accepted")
	}
}
