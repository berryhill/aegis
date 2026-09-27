package doerbinding

import (
	"errors"

	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
)

// BindDoerGraphRun resolves only exact single-node Graph input mappings.
func BindDoerGraphRun(revision loop.LoopRevision, definition graph.GraphRevision, node graph.Node, snapshot graph.GraphRunSnapshot) (loop.DoerContract, error) {
	if graph.ValidateRevision(definition).Outcome != graph.ValidationValid {
		return loop.DoerContract{}, errors.New("invalid exact Graph revision")
	}
	rebuilt, err := graph.NewRunSnapshot(snapshot.SnapshotID, definition, snapshot.Inputs)
	if err != nil || rebuilt.Digest != snapshot.Digest {
		return loop.DoerContract{}, errors.New("Graph snapshot content is not exact")
	}
	if revision.SchemaVersion != loop.DoerReusableSchemaVersion || len(definition.Nodes) != 1 || definition.Nodes[0].ID != node.ID ||
		definition.Digest != snapshot.Graph.Digest || node.Loop.Digest != revision.Digest ||
		len(definition.InputMappings) < 4 || len(definition.InputMappings) > 5 {
		return loop.DoerContract{}, errors.New("incomplete exact Doer binding")
	}
	values := make(map[string]graph.NormalizedInput, len(snapshot.Inputs))
	for _, input := range snapshot.Inputs {
		if _, duplicate := values[input.PortID]; duplicate {
			return loop.DoerContract{}, errors.New("duplicate Graph input")
		}
		values[input.PortID] = input
	}
	inputs := make([]loop.DoerInput, 0, len(definition.InputMappings))
	used := make(map[string]bool, len(definition.InputMappings))
	for _, mapping := range definition.InputMappings {
		value, ok := values[mapping.GraphInput]
		if mapping.ToNodeID != node.ID || used[mapping.ToPort] {
			return loop.DoerContract{}, errors.New("missing or duplicate mapped Doer input")
		}
		used[mapping.ToPort] = true
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
