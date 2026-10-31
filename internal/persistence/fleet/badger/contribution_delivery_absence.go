package badger

import (
	"context"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	badgerdb "github.com/dgraph-io/badger/v4"
)

// GetContributionPublicationContract resolves only the original owned root.
func (s *Store) GetContributionPublicationContract(ctx context.Context, owner, id string) (out contribution.Contract, err error) {
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		c, e := loadContribution(txn, id)
		if e != nil {
			return e
		}
		if c.OwnerID != owner {
			return delivery.ErrDenied
		}
		r := &ContributionDeliveryRepository{store: s, binding: ContributionDeliveryBinding{QueueItemID: id, OwnerID: owner, Key: c.Key, Reference: delivery.Reference{Run: c.RunID, Contract: c.Digest}}}
		out, e = r.contract(txn)
		return e
	})
	return
}

// ContributionDeliveryAuthoritativeAbsence checks the canonical intake AND all
// retained publication/history families in one snapshot. A missing root index
// alone is never absence. Corruption anywhere inspected fails closed.
func (s *Store) ContributionDeliveryAuthoritativeAbsence(ctx context.Context, owner, k string) (bool, error) {
	return s.contributionDeliveryAbsence(ctx, owner, k, true)
}

// ContributionDeliveryUnstarted is observation only: absence cannot authorize a
// release, write, new identity or budget. Every surviving slot denies absence.
func (s *Store) ContributionDeliveryUnstarted(ctx context.Context, owner, k string) (bool, error) {
	return s.contributionDeliveryAbsence(ctx, owner, k, false)
}

func (s *Store) contributionDeliveryAbsence(ctx context.Context, owner, k string, requireStop bool) (absent bool, err error) {
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		id, e := get(txn, key(familyContributionRequest, owner, k))
		if e != nil {
			return e
		}
		c, e := loadContribution(txn, string(id))
		if e != nil {
			return e
		}
		r := &ContributionDeliveryRepository{store: s, binding: ContributionDeliveryBinding{QueueItemID: c.QueueItemID, OwnerID: owner, Key: k, Reference: delivery.Reference{Run: c.RunID, Contract: c.Digest}}}
		if _, e = r.contract(txn); e != nil {
			return e
		}
		control, e := loadContributionControl(txn, c.QueueItemID)
		if e != nil {
			return e
		}
		if requireStop && !control.Stop {
			return delivery.ErrDenied
		}
		absent = true
		for _, family := range []byte{familyContributionPublication, familyContributionDelivery} {
			opts := badgerdb.DefaultIteratorOptions
			opts.Prefix = key(family)
			it := txn.NewIterator(opts)
			for it.Rewind(); it.Valid(); it.Next() {
				b, e := it.Item().ValueCopy(nil)
				if e != nil {
					it.Close()
					return e
				}
				if family == familyContributionDelivery {
					var v delivery.HistoryEnvelope
					if strictDelivery(b, &v) != nil || delivery.ValidateHistoryEnvelope(v) != nil {
						it.Close()
						return fleet.ErrCorrupt
					}
					if v.Record.Intent.Run == c.RunID || v.Record.Intent.Contract == c.Digest {
						absent = false
					}
				} else {
					var root delivery.PublicationRoot
					if strictDelivery(b, &root) == nil {
						seal := root.Digest
						root.Digest = ""
						if seal != contribution.Digest(deliveryWire(root)) {
							it.Close()
							return fleet.ErrCorrupt
						}
						root.Digest = seal
						if root.Run == c.RunID || root.Contract == c.Digest || root.OwnerID == owner && root.Key == k {
							absent = false
						}
					} else {
						var i delivery.Intent
						if strictDelivery(b, &i) != nil {
							it.Close()
							return fleet.ErrCorrupt
						}
						if i.Run == c.RunID || i.Contract == c.Digest {
							absent = false
						}
					}
				}
			}
			it.Close()
		}
		return nil
	})
	if err != nil {
		absent = false
	}
	return
}
