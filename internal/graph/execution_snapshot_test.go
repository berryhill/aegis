package graph

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func directFixture() Node {
	return Node{ID: "run-loop", Participant: exactRevision("agent", 1, "a"), Loop: exactRevision("loop", 2, "b"),
		Inputs: []Port{{ID: "value", Type: TypeObject, Required: true}}, Outputs: []Port{{ID: "result", Type: TypeArtifact, Required: true}}}
}

func executionInputs() []NormalizedInput {
	return []NormalizedInput{{PortID: "value", Type: TypeObject, Value: json.RawMessage(` { "z": 2, "a": 1 } `)}}
}

func TestExecutionSnapshotDirectCanonicalAndIndependent(t *testing.T) {
	node, inputs := directFixture(), executionInputs()
	snapshot, err := NewDirectLoopSnapshot("run-1", node, inputs)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := MarshalExecutionSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Graph != nil || bytes.Contains(wire, []byte(`"graph":`)) || snapshot.SourceKind != SourceDirectLoop || string(snapshot.Inputs[0].Value) != `{"a":1,"z":2}` {
		t.Fatalf("fabricated Graph source or noncanonical input: %s", wire)
	}
	decoded, err := UnmarshalExecutionSnapshot(wire)
	if err != nil || decoded.Digest != snapshot.Digest {
		t.Fatalf("roundtrip: %v", err)
	}
	canonical, err := NewDirectLoopSnapshot("run-1", node, []NormalizedInput{{PortID: "value", Type: TypeObject, Value: []byte(`{"a":1,"z":2}`)}})
	if err != nil || canonical.Digest != snapshot.Digest {
		t.Fatal("equivalent normalized inputs changed digest")
	}
	topology, err := ResolveExecutionTopology(decoded, nil, &node)
	if err != nil {
		t.Fatal(err)
	}
	topology.Nodes[0].Inputs[0].ID = "changed"
	node.Inputs[0].ID = "changed"
	inputs[0].Value[0] = '!'
	again, err := MarshalExecutionSnapshot(snapshot)
	if err != nil || !bytes.Equal(wire, again) {
		t.Fatal("snapshot aliases caller or resolver data")
	}
	if _, err := UnmarshalRunSnapshot(wire); err == nil {
		t.Fatal("legacy decoder accepted new execution schema")
	}
}

func TestExecutionSnapshotRejectsInvalidInputs(t *testing.T) {
	for name, inputs := range map[string][]NormalizedInput{
		"missing":        nil,
		"unknown":        {{PortID: "unknown", Type: TypeObject, Value: []byte(`{}`)}},
		"duplicate":      {executionInputs()[0], executionInputs()[0]},
		"wrong type":     {{PortID: "value", Type: TypeString, Value: []byte(`"x"`)}},
		"wrong value":    {{PortID: "value", Type: TypeObject, Value: []byte(`true`)}},
		"duplicate JSON": {{PortID: "value", Type: TypeObject, Value: []byte(`{"x":1,"x":2}`)}},
		"trailing JSON":  {{PortID: "value", Type: TypeObject, Value: []byte(`{} {}`)}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewDirectLoopSnapshot("run", directFixture(), inputs); err == nil {
				t.Fatal("invalid inputs accepted")
			}
		})
	}
}

func TestExecutionSnapshotRejectsTamperingAndAmbiguousSources(t *testing.T) {
	for name, mutate := range map[string]func(*ExecutionSnapshot){
		"unknown source":          func(s *ExecutionSnapshot) { s.SourceKind = "inferred" },
		"graph on direct":         func(s *ExecutionSnapshot) { ref := exactRevision("fake", 1, "c"); s.Graph = &ref },
		"missing participant":     func(s *ExecutionSnapshot) { s.Participants = nil },
		"extra participant":       func(s *ExecutionSnapshot) { s.Participants = append(s.Participants, s.Participants[0]) },
		"missing loop":            func(s *ExecutionSnapshot) { s.Loops = nil },
		"substituted loop":        func(s *ExecutionSnapshot) { s.Loops[0] = exactRevision("other", 1, "c") },
		"mutable node":            func(s *ExecutionSnapshot) { s.Topology.Nodes[0].Loop.Revision = 0 },
		"duplicate node":          func(s *ExecutionSnapshot) { s.Topology.Nodes = append(s.Topology.Nodes, s.Topology.Nodes[0]) },
		"substituted mapping":     func(s *ExecutionSnapshot) { s.Topology.InputMappings[0].ToPort = "other" },
		"graph claim without ref": func(s *ExecutionSnapshot) { s.SourceKind = SourcePublishedGraph },
	} {
		t.Run(name, func(t *testing.T) {
			s, err := NewDirectLoopSnapshot("run", directFixture(), executionInputs())
			if err != nil {
				t.Fatal(err)
			}
			mutate(&s)
			// Rehash attacker-controlled bytes: semantic checks must still reject.
			s.Digest, _ = executionSnapshotDigest(s)
			wire, _ := json.Marshal(s)
			if _, err := UnmarshalExecutionSnapshot(wire); err == nil {
				t.Fatal("tampered execution snapshot accepted")
			}
		})
	}
	node := directFixture()
	s, err := NewDirectLoopSnapshot("run", node, executionInputs())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveExecutionTopology(s, nil, nil); err == nil {
		t.Fatal("missing direct evidence accepted")
	}
	node.Loop = exactRevision("substitute", 1, "c")
	if _, err := ResolveExecutionTopology(s, nil, &node); err == nil {
		t.Fatal("substituted service reference accepted")
	}
	wire, _ := MarshalExecutionSnapshot(s)
	for name, data := range map[string][]byte{
		"unknown":         bytes.Replace(wire, []byte(`"snapshot_id":`), []byte(`"unknown":true,"snapshot_id":`), 1),
		"duplicate":       bytes.Replace(wire, []byte(`"snapshot_id":`), []byte(`"snapshot_id":"other","snapshot_id":`), 1),
		"trailing":        append(append([]byte{}, wire...), []byte(` {}`)...),
		"version":         bytes.Replace(wire, []byte(ExecutionSnapshotSchemaVersion), []byte("unknown"), 1),
		"changed content": bytes.Replace(wire, []byte(`"z":2`), []byte(`"z":3`), 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := UnmarshalExecutionSnapshot(data); err == nil {
				t.Fatal("invalid wire accepted")
			}
		})
	}
}

func TestExecutionSnapshotPublishedSourceRequiresExactReadback(t *testing.T) {
	node := directFixture()
	r := topologyDefinition(directTopology(node))
	r.GraphID = "published"
	published, _, err := NewRevision(r)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewPublishedGraphSnapshot("run", published, executionInputs())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveExecutionTopology(s, &published, nil); err != nil {
		t.Fatal(err)
	}
	wire, err := MarshalExecutionSnapshot(s)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UnmarshalExecutionSnapshot(wire); err != nil {
		t.Fatal(err)
	}
	tampered := s
	tampered.Topology = topologyOf(topologyDefinition(s.Topology))
	tampered.Topology.Nodes[0].ID = "substituted"
	tampered.Topology.InputMappings[0].ToNodeID = "substituted"
	tampered.Topology.OutputMappings[0].FromNodeID = "substituted"
	tampered.Digest, _ = executionSnapshotDigest(tampered)
	if _, err := ResolveExecutionTopology(tampered, &published, nil); err == nil {
		t.Fatal("rehashed topology substitution accepted")
	}
	if _, err := ResolveExecutionTopology(s, nil, nil); err == nil {
		t.Fatal("unproven Graph publication accepted")
	}
	if _, err := ResolveExecutionTopology(s, &published, &node); err == nil {
		t.Fatal("ambiguous source accepted")
	}
	changed := published
	changed.GraphID = "different"
	changed, _, err = NewRevision(changed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveExecutionTopology(s, &changed, nil); err == nil {
		t.Fatal("substituted Graph accepted")
	}
	direct, err := NewDirectLoopSnapshot("direct", node, executionInputs())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveExecutionTopology(direct, &published, &node); err == nil {
		t.Fatal("direct with fabricated Graph evidence accepted")
	}
	// A correctly hashed forged source claim is not authoritative publication.
	direct.SourceKind = SourcePublishedGraph
	claimed := *s.Graph
	claimed.ID = "never-published"
	direct.Graph = &claimed
	direct.Digest, _ = executionSnapshotDigest(direct)
	if _, err := ResolveExecutionTopology(direct, &published, nil); err == nil {
		t.Fatal("forged Graph publication accepted")
	}
	multi, _, err := NewRevision(graphCandidate())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewPublishedGraphSnapshot("multi", multi, nil); err == nil {
		t.Fatal("general multi-node execution accepted")
	}
}

func TestHistoricalSnapshotWireAndDigestRemainV1(t *testing.T) {
	// Independent literal legacy representation fixes field order and omission.
	ref := func(id, marker string) string {
		return `{"schema_version":"aegis.reference.revision.v1","id":"` + id + `","revision":1,"digest":"sha256:` + strings.Repeat(marker, 64) + `"}`
	}
	payload := `{"schema_version":"aegis.graph.run-snapshot.v1","snapshot_id":"history","graph":` + ref("graph", "a") + `,"inputs":[],"participants":[` + ref("agent", "b") + `],"loops":[` + ref("loop", "c") + `],"digest":""}`
	digest := sha256Digest([]byte(payload))
	wire := []byte(strings.Replace(payload, `"digest":""}`, `"digest":"`+digest+`"}`, 1))
	old, err := UnmarshalRunSnapshot(wire)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := MarshalRunSnapshot(old)
	if err != nil || old.Digest != digest || !bytes.Equal(wire, encoded) {
		t.Fatalf("historical bytes/digest changed: %v", err)
	}
	if _, err := UnmarshalExecutionSnapshot(wire); err == nil {
		t.Fatal("historical snapshot reclassified")
	}
}
