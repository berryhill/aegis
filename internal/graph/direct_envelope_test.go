package graph

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestDirectEnvelopeRoundtripAndDisagreement(t *testing.T) {
	e, err := NewDirectLoopSnapshot("direct", directFixture(), executionInputs())
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewDirectRunSnapshot(e)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := MarshalRunSnapshot(s)
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnmarshalRunSnapshot(wire)
	if err != nil || got.Digest != s.Digest {
		t.Fatalf("roundtrip: %v", err)
	}
	node := directFixture()
	if _, err = ResolveRunTopology(got, nil, &node); err != nil {
		t.Fatal(err)
	}
	if _, err = ResolveRunTopology(got, nil, nil); err == nil {
		t.Fatal("missing source accepted")
	}
	for name, mutate := range map[string]func(*GraphRunSnapshot){
		"id":            func(s *GraphRunSnapshot) { s.SnapshotID = "other" },
		"input":         func(s *GraphRunSnapshot) { s.Inputs[0].Value = json.RawMessage(`{}`) },
		"participant":   func(s *GraphRunSnapshot) { s.Participants[0] = exactRevision("other", 1, "a") },
		"loop":          func(s *GraphRunSnapshot) { s.Loops[0] = exactRevision("other", 1, "a") },
		"graph":         func(s *GraphRunSnapshot) { s.Graph = exactRevision("fake", 1, "a") },
		"nested digest": func(s *GraphRunSnapshot) { s.Execution.Digest = "sha256:bad" },
		"nested source": func(s *GraphRunSnapshot) {
			s.Execution.SourceKind = SourcePublishedGraph
			ref := exactRevision("fake", 1, "a")
			s.Execution.Graph = &ref
			s.Execution.Digest, _ = executionSnapshotDigest(*s.Execution)
		},
		"nested participant": func(s *GraphRunSnapshot) {
			s.Execution.Participants[0] = exactRevision("other", 1, "a")
			s.Execution.Digest, _ = executionSnapshotDigest(*s.Execution)
		},
		"nested loop": func(s *GraphRunSnapshot) {
			s.Execution.Topology.Nodes[0].Loop = exactRevision("other", 1, "a")
			s.Execution.Digest, _ = executionSnapshotDigest(*s.Execution)
		},
	} {
		t.Run(name, func(t *testing.T) {
			s, _ := UnmarshalRunSnapshot(wire)
			mutate(&s)
			s.Digest, _ = digestSnapshot(s)
			data, _ := json.Marshal(s)
			if _, err := UnmarshalRunSnapshot(data); err == nil {
				t.Fatal("rehashed disagreement accepted")
			}
		})
	}
	e.Inputs[0].Value[0] = '!'
	again, err := MarshalRunSnapshot(s)
	if err != nil || !bytes.Equal(wire, again) {
		t.Fatal("nested alias")
	}
}

func TestHistoricalEnvelopeRejectsNewFieldEvenNull(t *testing.T) {
	r, _, err := NewRevision(GraphRevision{GraphID: "historical", Revision: 1, Nodes: []Node{directFixture()}, Inputs: directFixture().Inputs, Outputs: directFixture().Outputs,
		InputMappings: []InputMapping{{GraphInput: "value", ToNodeID: "run-loop", ToPort: "value"}}, OutputMappings: []OutputMapping{{FromNodeID: "run-loop", FromPort: "result", GraphOutput: "result"}}})
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewRunSnapshot("old", r, executionInputs())
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := MarshalRunSnapshot(s)
	bad := append(append([]byte(nil), wire[:len(wire)-1]...), []byte(`,"execution":null}`)...)
	if _, err := UnmarshalRunSnapshot(bad); err == nil {
		t.Fatal("v1 extension accepted")
	}
	if _, err := ResolveRunTopology(s, &r, nil); err != nil {
		t.Fatal(err)
	}
	other := r
	other.GraphID = "other"
	if _, err := ResolveRunTopology(s, &other, nil); err == nil {
		t.Fatal("source substitution")
	}
}
