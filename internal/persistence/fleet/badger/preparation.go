package badger

import (
	"context"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	badgerdb "github.com/dgraph-io/badger/v4"
)

// RecordPreparationDiagnostic never grants authority or creates executable work.
func (s *Store) RecordPreparationDiagnostic(ctx context.Context, tr queue.QueueTransition, fact fleet.AuditFact) error {
	wire, err := queue.MarshalTransition(tr)
	if err != nil {
		return err
	}
	return s.update(ctx, func(txn *badgerdb.Txn) error {
		p, err := loadQueueProjection(txn, tr.QueueItemID)
		if err != nil {
			return err
		}
		if validateProjectionBasis(txn, p) != nil || !p.State.IsPreparation() || p.Attempts != 0 || p.ActiveClaimID != "" || tr.From != p.State || tr.To != p.State || tr.ClaimID != "" {
			return fleet.ErrConflict
		}
		if p.LastTransitionID == tr.TransitionID {
			return nil
		}
		p.LastTransitionID, p.UpdatedAt = tr.TransitionID, tr.OccurredAt
		p, err = queue.NewProjection(p)
		if err != nil {
			return err
		}
		pw, err := queue.MarshalProjection(p)
		if err != nil {
			return err
		}
		if err = create(txn, key(familyQueueTransition, tr.QueueItemID, tr.TransitionID), wire); err != nil {
			return err
		}
		if err = txn.Set(key(familyQueueProjection, tr.QueueItemID), pw); err != nil {
			return err
		}
		return appendAudit(txn, fact)
	})
}
