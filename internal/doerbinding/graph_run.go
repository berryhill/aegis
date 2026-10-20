package doerbinding

import (
	"errors"
	"reflect"

	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
)

// BindDoerGraphRun resolves only exact single-node Graph input mappings.
func BindDoerGraphRun(revision loop.LoopRevision, definition graph.GraphRevision, node graph.Node, snapshot graph.GraphRunSnapshot) (loop.DoerContract, error) {
	if graph.ValidateRevision(definition).Outcome != graph.ValidationValid {
		return loop.DoerContract{}, errors.New("invalid exact Graph revision")
	}
	_, err := graph.ResolveRunTopology(snapshot, &definition, nil)
	if err != nil {
		return loop.DoerContract{}, errors.New("Graph snapshot content is not exact")
	}
	if revision.SchemaVersion != loop.DoerReusableSchemaVersion || len(definition.Nodes) != 1 || !reflect.DeepEqual(definition.Nodes[0], node) ||
		definition.Digest != snapshot.Graph.Digest || node.Loop != loopReference(revision) {
		return loop.DoerContract{}, errors.New("incomplete exact Doer binding")
	}
	return bindDoerInputs(revision, node.ID, definition.InputMappings, snapshot.Inputs)
}

// BindDoerDirectRun validates immutable direct execution data against an exact
// service-resolved node and Loop revision. The caller must obtain the participant
// and Loop from authoritative readback; this binder proves neither publication,
// lifecycle nor authorization and grants no runtime or filesystem authority.
func BindDoerDirectRun(revision loop.LoopRevision, node graph.Node, snapshot graph.GraphRunSnapshot) (loop.DoerContract, error) {
	if snapshot.SchemaVersion != graph.DirectRunSnapshotSchemaVersion || node.Loop != loopReference(revision) ||
		!sameLoopPorts(node.Inputs, revision.Inputs) || !sameLoopPorts(node.Outputs, revision.Outputs) {
		return loop.DoerContract{}, errors.New("incomplete exact direct Doer binding")
	}
	topology, err := graph.ResolveRunTopology(snapshot, nil, &node)
	if err != nil {
		return loop.DoerContract{}, err
	}
	return bindDoerInputs(revision, node.ID, topology.InputMappings, snapshot.Inputs)
}

func loopReference(revision loop.LoopRevision) reference.RevisionRef {
	return reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: revision.LoopID, Revision: revision.Revision, Digest: revision.Digest}
}

func sameLoopPorts(ports []graph.Port, exact []loop.Port) bool {
	if len(ports) != len(exact) {
		return false
	}
	seen := make(map[string]bool, len(ports))
	for _, port := range ports {
		if seen[port.ID] {
			return false
		}
		seen[port.ID] = true
		found := false
		for _, expected := range exact {
			if port.ID == expected.ID && port.Type == graph.ValueType(expected.Type) && port.Required == expected.Required {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func bindDoerInputs(revision loop.LoopRevision, nodeID string, mappings []graph.InputMapping, normalized []graph.NormalizedInput) (loop.DoerContract, error) {
	if len(mappings) < 4 || len(mappings) > 5 {
		return loop.DoerContract{}, errors.New("incomplete Doer input mappings")
	}
	values := make(map[string]graph.NormalizedInput, len(normalized))
	for _, input := range normalized {
		if _, duplicate := values[input.PortID]; duplicate {
			return loop.DoerContract{}, errors.New("duplicate Graph input")
		}
		values[input.PortID] = input
	}
	inputs := make([]loop.DoerInput, 0, len(mappings))
	used := make(map[string]bool, len(mappings))
	sources := make(map[string]bool, len(mappings))
	for _, mapping := range mappings {
		value, ok := values[mapping.GraphInput]
		if mapping.ToNodeID != nodeID || used[mapping.ToPort] || sources[mapping.GraphInput] {
			return loop.DoerContract{}, errors.New("missing or duplicate mapped Doer input")
		}
		used[mapping.ToPort] = true
		sources[mapping.GraphInput] = true
		if !ok {
			if mapping.ToPort == "expected_text" {
				continue
			}
			return loop.DoerContract{}, errors.New("required mapped Doer input missing")
		}
		inputs = append(inputs, loop.DoerInput{PortID: mapping.ToPort, Type: loop.ValueType(value.Type), Value: value.Value})
	}
	if len(inputs) != len(values) {
		return loop.DoerContract{}, errors.New("unmapped Graph input in Doer run")
	}
	return loop.NormalizeDoerBinding(revision, inputs)
}
