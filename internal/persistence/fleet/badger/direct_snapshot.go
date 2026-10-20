package badger

import (
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	badgerdb "github.com/dgraph-io/badger/v4"
)

// verifyDirectSnapshotReferences runs inside the existing atomic acceptance
// transaction. No Graph library lookup or write occurs. This is persistence
// integrity, not runtime/mandate or host-write admission.
func verifyDirectSnapshotReferences(txn *badgerdb.Txn, s graph.GraphRunSnapshot) error {
	if _, err := graph.MarshalRunSnapshot(s); err != nil {
		return fleet.ErrConflict
	}
	node := s.Execution.Topology.Nodes[0]
	agent, err := agentRevision(txn, node.Participant.ID, node.Participant.Revision)
	if err != nil {
		return err
	}
	if agent.Digest != node.Participant.Digest || agent.Lifecycle != registry.LifecycleEnabled {
		return fleet.ErrConflict
	}
	revision, err := loopRevision(txn, node.Loop.ID, node.Loop.Revision)
	if err != nil {
		return err
	}
	if revision.Digest != node.Loop.Digest {
		return fleet.ErrConflict
	}
	if err = requireLoopValidation(txn, revision); err != nil {
		return err
	}
	// Build the interface only from exact immutable publication readback, not from
	// embedded assertions. Preserve the execution-local node ID, not a Graph ID.
	exact := graph.Node{ID: node.ID,
		Participant: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: agent.AgentID, Revision: agent.Revision, Digest: agent.Digest},
		Loop:        reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: revision.LoopID, Revision: revision.Revision, Digest: revision.Digest}}
	for _, p := range revision.Inputs {
		exact.Inputs = append(exact.Inputs, graph.Port{ID: p.ID, Type: graph.ValueType(p.Type), Required: p.Required})
	}
	for _, p := range revision.Outputs {
		exact.Outputs = append(exact.Outputs, graph.Port{ID: p.ID, Type: graph.ValueType(p.Type), Required: p.Required})
	}
	if _, err = graph.ResolveRunTopology(s, nil, &exact); err != nil {
		return fleet.ErrConflict
	}
	return nil
}
