package badger

import (
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	badgerdb "github.com/dgraph-io/badger/v4"
)

// validateGraphNodeFamilies qualifies both canonical families before a reopened
// store serves reads. They are facts, not projections that may be rebuilt or
// silently discarded. Physical Badger snapshots retain both key namespaces.
func validateGraphNodeFamilies(txn *badgerdb.Txn) error {
	for _, family := range []byte{familyGraphNode, familyGraphNodeCheckpoint} {
		if err := scan(txn, family, func(k, _ []byte) error {
			parts, err := decodeKeyParts(k, family)
			if err != nil || len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				return fleet.ErrCorrupt
			}
			if family == familyGraphNode {
				var a fleet.GraphNodeAdmission
				a, err = loadNode(txn, parts[0], parts[1])
				if err == nil {
					p, e := loadQueueProjection(txn, a.Claim.QueueItem.ID)
					if e != nil {
						return fleet.ErrCorrupt
					}
					if p.State == queue.StateSucceeded {
						c, e := loadNodeCheckpoint(txn, parts[0], parts[1])
						if e != nil || c.Completion.Disposition.State != execution.StateSucceeded {
							return fleet.ErrCorrupt
						}
					}
				}
			} else {
				_, err = loadNodeCheckpoint(txn, parts[0], parts[1])
			}
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}
