package loop

import (
	"errors"

	"github.com/berryhill/aegis/internal/graph"
)

// BindDoerGraphRun resolves only exact single-node Graph input mappings.
func BindDoerGraphRun(revision LoopRevision, definition graph.GraphRevision, node graph.Node, snapshot graph.GraphRunSnapshot) (DoerContract, error) {
	if graph.ValidateRevision(definition).Outcome != graph.ValidationValid {
		return DoerContract{}, errors.New("invalid exact Graph revision")
	}
	rebuilt, err := graph.NewRunSnapshot(snapshot.SnapshotID, definition, snapshot.Inputs)
	if err != nil || rebuilt.Digest != snapshot.Digest {
		return DoerContract{}, errors.New("Graph snapshot content is not exact")
	}
	if revision.SchemaVersion != DoerReusableSchemaVersion || len(definition.Nodes) != 1 || definition.Nodes[0].ID != node.ID ||
		definition.Digest != snapshot.Graph.Digest || node.Loop.Digest != revision.Digest ||
		len(definition.InputMappings) < 4 || len(definition.InputMappings) > 5 {
		return DoerContract{}, errors.New("incomplete exact Doer binding")
	}
	values := make(map[string]graph.NormalizedInput, len(snapshot.Inputs))
	for _, input := range snapshot.Inputs {
		if _, duplicate := values[input.PortID]; duplicate {
			return DoerContract{}, errors.New("duplicate Graph input")
		}
		values[input.PortID] = input
	}
	inputs := make([]DoerInput, 0, len(definition.InputMappings))
	used := make(map[string]bool, len(definition.InputMappings))
	for _, mapping := range definition.InputMappings {
		value, ok := values[mapping.GraphInput]
		if mapping.ToNodeID != node.ID || used[mapping.ToPort] {
			return DoerContract{}, errors.New("missing or duplicate mapped Doer input")
		}
		used[mapping.ToPort] = true
		if !ok {
			if mapping.ToPort == "expected_text" {
				continue
			}
			return DoerContract{}, errors.New("required mapped Doer input missing")
		}
		inputs = append(inputs, DoerInput{PortID: mapping.ToPort, Type: ValueType(value.Type), Value: value.Value})
	}
	if len(inputs) != len(values) {
		return DoerContract{}, errors.New("unmapped Graph input in Doer run")
	}
	return NormalizeDoerBinding(revision, inputs)
}
