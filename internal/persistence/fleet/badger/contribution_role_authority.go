package badger

import (
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	badgerdb "github.com/dgraph-io/badger/v4"
)

// checkContributionRoleFence runs in the existing continuity CAS transaction;
// it never calls authority services or remote processes within Badger.
func checkContributionRoleFence(txn *badgerdb.Txn, c contribution.Contract, e contribution.ContinuityEvent) error {
	if e.Kind != "role-admitted" {
		return nil
	}
	if e.RoleAuthority == nil {
		return contribution.ErrContinuityConflict
	}
	b, err := get(txn, key(familyQueueRuntimeBinding, c.QueueItemID))
	if err != nil {
		return err
	}
	original, err := queue.UnmarshalRuntimeBinding(b)
	if err != nil {
		return fleet.ErrCorrupt
	}
	r := e.RoleAuthority.OriginalRuntimeBinding
	if original.BindingID != r.ID || original.Digest != r.Digest || original.OwnerAgent != c.Controller || original.Submission.ID != c.SubmissionID {
		return contribution.ErrContinuityConflict
	}
	projection, err := loadQueueProjection(txn, c.QueueItemID)
	if err != nil {
		return err
	}
	if projection.State != queue.StateQueued && projection.State != queue.StateClaimed {
		return contribution.ErrContinuityFenced
	}
	return nil
}
