package graph

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/berryhill/aegis/internal/reference"
)

const ExecutionSnapshotSchemaVersion = "aegis.execution.run-snapshot.v1"

type SourceKind string

const (
	SourceDirectLoop     SourceKind = "direct_loop"
	SourcePublishedGraph SourceKind = "published_graph"
)

// ExecutionTopology is frozen definition data, never a published Graph record.
// This first schema supports only single-node execution, not general scheduling.
type ExecutionTopology struct {
	Inputs         []Port          `json:"inputs"`
	Outputs        []Port          `json:"outputs"`
	Nodes          []Node          `json:"nodes"`
	InputMappings  []InputMapping  `json:"input_mappings"`
	Dependencies   []Dependency    `json:"dependencies"`
	OutputMappings []OutputMapping `json:"output_mappings"`
	AdmissionRules []AdmissionRule `json:"admission_rules"`
}

// ExecutionSnapshot carries no admission authority. Source claims must be checked
// with ResolveExecutionTopology against owning-service immutable readback.
// It deliberately does not extend/reinterpret historical GraphRunSnapshot v1.
type ExecutionSnapshot struct {
	SchemaVersion string                  `json:"schema_version"`
	SnapshotID    string                  `json:"snapshot_id"`
	SourceKind    SourceKind              `json:"source_kind"`
	Graph         *reference.RevisionRef  `json:"graph,omitempty"`
	Topology      ExecutionTopology       `json:"topology"`
	Inputs        []NormalizedInput       `json:"inputs"`
	Participants  []reference.RevisionRef `json:"participants"`
	Loops         []reference.RevisionRef `json:"loops"`
	Digest        string                  `json:"digest"`
}

// topologyDefinition is a private validation adapter only. Its local identity
// is never sealed, published, referenced, or persisted as a Graph revision.
func topologyDefinition(t ExecutionTopology) GraphRevision {
	return GraphRevision{SchemaVersion: RevisionSchemaVersion, GraphID: "execution-topology", Revision: 1,
		Validator: ValidatorSpec{ID: ValidatorID, Version: ValidatorVersion}, Inputs: t.Inputs, Outputs: t.Outputs,
		Nodes: t.Nodes, InputMappings: t.InputMappings, Dependencies: t.Dependencies,
		OutputMappings: t.OutputMappings, AdmissionRules: t.AdmissionRules}
}

func topologyOf(r GraphRevision) ExecutionTopology {
	r = canonicalRevision(r)
	return ExecutionTopology{Inputs: r.Inputs, Outputs: r.Outputs, Nodes: r.Nodes, InputMappings: r.InputMappings,
		Dependencies: r.Dependencies, OutputMappings: r.OutputMappings, AdmissionRules: r.AdmissionRules}
}

func directTopology(node Node) ExecutionTopology {
	t := ExecutionTopology{Inputs: node.Inputs, Outputs: node.Outputs, Nodes: []Node{node}}
	for _, p := range node.Inputs {
		t.InputMappings = append(t.InputMappings, InputMapping{GraphInput: p.ID, ToNodeID: node.ID, ToPort: p.ID})
	}
	for _, p := range node.Outputs {
		t.OutputMappings = append(t.OutputMappings, OutputMapping{FromNodeID: node.ID, FromPort: p.ID, GraphOutput: p.ID})
	}
	return topologyOf(topologyDefinition(t))
}

// NewDirectLoopSnapshot requires an exact service-resolved participant/Loop
// interface. It performs no publication, lifecycle lookup or authorization.
func NewDirectLoopSnapshot(id string, node Node, inputs []NormalizedInput) (ExecutionSnapshot, error) {
	return sealExecutionSnapshot(ExecutionSnapshot{SchemaVersion: ExecutionSnapshotSchemaVersion, SnapshotID: id,
		SourceKind: SourceDirectLoop, Topology: directTopology(node), Inputs: inputs,
		Participants: []reference.RevisionRef{node.Participant}, Loops: []reference.RevisionRef{node.Loop}})
}

// NewPublishedGraphSnapshot requires a service-resolved published revision;
// validating a sealed definition alone cannot prove publication or authority.
func NewPublishedGraphSnapshot(id string, revision GraphRevision, inputs []NormalizedInput) (ExecutionSnapshot, error) {
	if len(validateRevision(revision, true)) != 0 || len(revision.Nodes) != 1 {
		return ExecutionSnapshot{}, errors.New("exact single-node published Graph required")
	}
	ref := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: revision.GraphID, Revision: revision.Revision, Digest: revision.Digest}
	node := revision.Nodes[0]
	return sealExecutionSnapshot(ExecutionSnapshot{SchemaVersion: ExecutionSnapshotSchemaVersion, SnapshotID: id,
		SourceKind: SourcePublishedGraph, Graph: &ref, Topology: topologyOf(revision), Inputs: inputs,
		Participants: []reference.RevisionRef{node.Participant}, Loops: []reference.RevisionRef{node.Loop}})
}

func canonicalExecutionSnapshot(s ExecutionSnapshot) (ExecutionSnapshot, error) {
	s.Topology = topologyOf(topologyDefinition(s.Topology))
	if s.Graph != nil {
		ref := *s.Graph
		s.Graph = &ref
	}
	old, err := canonicalizeSnapshotValues(GraphRunSnapshot{Inputs: s.Inputs, Participants: s.Participants, Loops: s.Loops})
	if err != nil {
		return ExecutionSnapshot{}, err
	}
	s.Inputs, s.Participants, s.Loops = old.Inputs, old.Participants, old.Loops
	return s, nil
}

func executionSnapshotDigest(s ExecutionSnapshot) (string, error) {
	s.Digest = ""
	wire, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return sha256Digest(wire), nil
}

func sealExecutionSnapshot(s ExecutionSnapshot) (ExecutionSnapshot, error) {
	s, err := canonicalExecutionSnapshot(s)
	if err != nil {
		return ExecutionSnapshot{}, err
	}
	s.Digest, err = executionSnapshotDigest(s)
	if err != nil {
		return ExecutionSnapshot{}, err
	}
	if err := validateExecutionSnapshot(s); err != nil {
		return ExecutionSnapshot{}, err
	}
	return s, nil
}

func equalTopology(a, b ExecutionTopology) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return bytes.Equal(left, right)
}

func validateExecutionSnapshot(s ExecutionSnapshot) error {
	if s.SchemaVersion != ExecutionSnapshotSchemaVersion || !validID(s.SnapshotID) {
		return errors.New("invalid execution snapshot identity")
	}
	if len(s.Topology.Nodes) != 1 || len(s.Topology.Dependencies) != 0 {
		return errors.New("only single-node execution topology is supported")
	}
	r := topologyDefinition(s.Topology)
	if len(validateRevision(r, false)) != 0 {
		return errors.New("invalid execution topology")
	}
	switch s.SourceKind {
	case SourceDirectLoop:
		if s.Graph != nil || !equalTopology(s.Topology, directTopology(s.Topology.Nodes[0])) {
			return errors.New("direct Loop must not claim Graph provenance or substituted topology")
		}
	case SourcePublishedGraph:
		if s.Graph == nil || s.Graph.Validate() != nil {
			return errors.New("published Graph source requires exact reference")
		}
	default:
		return errors.New("unknown execution source kind")
	}
	node := s.Topology.Nodes[0]
	if len(s.Participants) != 1 || s.Participants[0] != node.Participant || len(s.Loops) != 1 || s.Loops[0] != node.Loop {
		return errors.New("execution references do not exactly match topology")
	}
	if len(s.Inputs) > MaxNormalizedInputs {
		return errors.New("too many execution inputs")
	}
	seen := make(map[string]bool, len(s.Inputs))
	for _, input := range s.Inputs {
		port, ok := portByID(s.Topology.Inputs, input.PortID)
		if !ok || port.Type != input.Type || seen[input.PortID] {
			return errors.New("unknown, duplicate or mismatched execution input")
		}
		seen[input.PortID] = true
		if err := validateInputValue(input); err != nil {
			return err
		}
	}
	for _, port := range s.Topology.Inputs {
		if port.Required && !seen[port.ID] {
			return errors.New("required execution input missing")
		}
	}
	digest, err := executionSnapshotDigest(s)
	if err != nil || !validDigest(s.Digest) || digest != s.Digest {
		return errors.New("execution snapshot digest mismatch")
	}
	return nil
}

func MarshalExecutionSnapshot(s ExecutionSnapshot) ([]byte, error) {
	s, err := canonicalExecutionSnapshot(s)
	if err != nil {
		return nil, err
	}
	if err := validateExecutionSnapshot(s); err != nil {
		return nil, err
	}
	return json.Marshal(s)
}

func UnmarshalExecutionSnapshot(data []byte) (ExecutionSnapshot, error) {
	s, err := decodeStrict[ExecutionSnapshot](data)
	if err != nil {
		return ExecutionSnapshot{}, err
	}
	s, err = canonicalExecutionSnapshot(s)
	if err != nil {
		return ExecutionSnapshot{}, err
	}
	if err := validateExecutionSnapshot(s); err != nil {
		return ExecutionSnapshot{}, err
	}
	return s, nil
}

// ResolveExecutionTopology validates immutable integrity and source evidence.
// Supply exactly one owning-service-resolved source: an exact published Graph,
// or a node built from the exact registered participant and published Loop.
// Callers must still enforce lifecycle, authorization and fresh effect admission.
func ResolveExecutionTopology(s ExecutionSnapshot, published *GraphRevision, direct *Node) (ExecutionTopology, error) {
	s, err := canonicalExecutionSnapshot(s)
	if err != nil {
		return ExecutionTopology{}, err
	}
	if err := validateExecutionSnapshot(s); err != nil {
		return ExecutionTopology{}, err
	}
	switch s.SourceKind {
	case SourceDirectLoop:
		if published != nil || direct == nil || !equalTopology(s.Topology, directTopology(*direct)) {
			return ExecutionTopology{}, errors.New("direct source evidence missing, ambiguous or substituted")
		}
	case SourcePublishedGraph:
		if direct != nil || published == nil || len(validateRevision(*published, true)) != 0 {
			return ExecutionTopology{}, errors.New("published Graph evidence missing or invalid")
		}
		expected := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: published.GraphID, Revision: published.Revision, Digest: published.Digest}
		if *s.Graph != expected || !equalTopology(s.Topology, topologyOf(*published)) {
			return ExecutionTopology{}, errors.New("published Graph source or topology substituted")
		}
	}
	return s.Topology, nil
}
