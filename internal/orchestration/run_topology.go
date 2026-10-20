package orchestration

import (
	"context"
	"fmt"

	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/persistence/fleet"
)

// ResolveRunTopology reconstructs immutable execution data, not authority. It
// performs no publication or lifecycle mutation and never resolves latest state.
// Historical Graphs retain their original definition and topology (including
// multi-node history which the bounded worker cannot execute).
func (service *FleetService) ResolveRunTopology(ctx context.Context, snapshot graph.GraphRunSnapshot) (graph.ExecutionTopology, error) {
	if snapshot.SchemaVersion != graph.DirectRunSnapshotSchemaVersion {
		definition, err := service.repository.GetGraphRevision(ctx, snapshot.Graph.ID, snapshot.Graph.Revision)
		if err != nil {
			return graph.ExecutionTopology{}, err
		}
		return graph.ResolveRunTopology(snapshot, &definition, nil)
	}
	// Validate before indexing embedded topology or consulting its references.
	if _, err := graph.MarshalRunSnapshot(snapshot); err != nil {
		return graph.ExecutionTopology{}, fmt.Errorf("%w: invalid direct snapshot", fleet.ErrCorrupt)
	}
	node := snapshot.Execution.Topology.Nodes[0]
	participant, err := service.repository.GetAgentRevision(ctx, node.Participant.ID, node.Participant.Revision)
	if err != nil {
		return graph.ExecutionTopology{}, err
	}
	revision, err := service.repository.GetLoopRevision(ctx, node.Loop.ID, node.Loop.Revision)
	if err != nil {
		return graph.ExecutionTopology{}, err
	}
	if participant.AgentID != node.Participant.ID || participant.Revision != node.Participant.Revision || participant.Digest != node.Participant.Digest || revision.LoopID != node.Loop.ID || revision.Revision != node.Loop.Revision || revision.Digest != node.Loop.Digest {
		return graph.ExecutionTopology{}, fleet.ErrCorrupt
	}
	// Interfaces come from immutable Loop readback, never embedded assertions.
	exact := graph.Node{ID: node.ID, Participant: node.Participant, Loop: node.Loop}
	for _, p := range revision.Inputs {
		exact.Inputs = append(exact.Inputs, graph.Port{ID: p.ID, Type: graph.ValueType(p.Type), Required: p.Required})
	}
	for _, p := range revision.Outputs {
		exact.Outputs = append(exact.Outputs, graph.Port{ID: p.ID, Type: graph.ValueType(p.Type), Required: p.Required})
	}
	return graph.ResolveRunTopology(snapshot, nil, &exact)
}
