package badger

import (
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/queue"
	badgerdb "github.com/dgraph-io/badger/v4"
)

// exactSingleNodeCompletion binds the successful child to the immutable admitted
// topology. Checking its Loop evidence alone cannot establish Graph membership
// or aggregate success. Until a separate multi-node checkpoint/aggregate protocol
// exists, a valid producer must not terminalize a multi-node Graph successfully.
// Historical/direct codecs and all non-success terminal outcomes are unchanged.
func exactSingleNodeCompletion(txn *badgerdb.Txn, attempt execution.Attempt, child execution.LoopExecution) bool {
	itemWire, err := get(txn, key(familyQueueItem, attempt.QueueItem.ID))
	if err != nil {
		return false
	}
	item, err := queue.UnmarshalItem(itemWire)
	if err != nil || item.Digest != attempt.QueueItem.Digest || item.GraphRunID != attempt.GraphRunID || child.GraphRunID != item.GraphRunID {
		return false
	}
	snapshotWire, err := get(txn, key(familySnapshot, item.Snapshot.ID))
	if err != nil {
		return false
	}
	snapshot, err := graph.UnmarshalRunSnapshot(snapshotWire)
	if err != nil || snapshot.SnapshotID != item.Snapshot.ID || snapshot.Digest != item.Snapshot.Digest {
		return false
	}
	var topology graph.ExecutionTopology
	if snapshot.SchemaVersion == graph.DirectRunSnapshotSchemaVersion {
		if verifyDirectSnapshotReferences(txn, snapshot) != nil {
			return false
		}
		node := snapshot.Execution.Topology.Nodes[0]
		topology, err = graph.ResolveRunTopology(snapshot, nil, &node)
	} else {
		wire, loadErr := get(txn, key(familyGraphRevision, snapshot.Graph.ID, revisionPart(snapshot.Graph.Revision)))
		if loadErr != nil {
			return false
		}
		definition, loadErr := graph.UnmarshalRevision(wire)
		if loadErr != nil {
			return false
		}
		topology, err = graph.ResolveRunTopology(snapshot, &definition, nil)
	}
	if err != nil || len(topology.Nodes) != 1 {
		return false
	}
	node := topology.Nodes[0]
	return node.ID == child.GraphNodeID && node.Loop == child.Loop && node.Participant == child.Participant
}
