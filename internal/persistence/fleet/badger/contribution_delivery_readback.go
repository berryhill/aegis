package badger

import (
	"bytes"
	"context"
	"fmt"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	badgerdb "github.com/dgraph-io/badger/v4"
)

// ContributionDeliveryHistory is a single canonical-store snapshot. Historical
// Authority epochs are returned unchanged; reading never admits a runtime effect.
type ContributionDeliveryHistory = delivery.HistorySnapshot

func (s *Store) GetContributionDeliveryHistory(ctx context.Context, ownerID, k string) (out ContributionDeliveryHistory, err error) {
	if ownerID == "" || !contribution.ValidKey(k) {
		return out, delivery.ErrDenied
	}
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		id, e := get(txn, key(familyContributionRequest, ownerID, k))
		if e != nil {
			return e
		}
		c, e := loadContribution(txn, string(id))
		if e != nil {
			return e
		}
		if c.OwnerID != ownerID || c.Key != k {
			return delivery.ErrDenied
		}
		wire, e := get(txn, key(familyContributionPublication, "root", ownerID, k))
		if e != nil {
			return fleet.ErrCorrupt
		}
		var root deliveryPublication
		if strictDelivery(wire, &root) != nil {
			return fleet.ErrCorrupt
		}
		seal := root.Digest
		root.Digest = ""
		if seal != contribution.Digest(deliveryWire(root)) {
			return fleet.ErrCorrupt
		}
		root.Digest = seal
		if root.OwnerID != ownerID || root.Key != k || root.QueueItemID != c.QueueItemID || root.Run != c.RunID || root.Contract != c.Digest || root.RepositoryID != c.Source.RepositoryID || root.Repository != c.Repository {
			return fleet.ErrCorrupt
		}
		pub, e := get(txn, key(familyContributionPublication, fmt.Sprint(root.RepositoryID), root.Base))
		if e != nil || !bytes.Equal(pub, wire) {
			return fleet.ErrCorrupt
		}
		item, e := loadQueueItem(txn, c.QueueItemID)
		if e != nil {
			return fleet.ErrCorrupt
		}
		run, e := loadGraphRun(txn, c.RunID)
		if e != nil || item.GraphRunID != c.RunID || item.Submission.ID != c.SubmissionID || run.QueueItem.ID != item.ItemID || run.QueueItem.Digest != item.Digest || run.Snapshot != item.Snapshot || run.Authority != item.Authority {
			return fleet.ErrCorrupt
		}
		prefix := key(familyContributionPublication, fmt.Sprint(root.RepositoryID), root.Base, "slot")
		opts := badgerdb.DefaultIteratorOptions
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()
		contractBytes, e := get(txn, key(familyContribution, c.QueueItemID))
		if e != nil {
			return fleet.ErrCorrupt
		}
		out.ContractBytes = contractBytes
		out.Root = root
		out.RootBytes = append([]byte(nil), wire...)
		out.Contract = c
		out.RootRun = c.RunID
		for it.Rewind(); it.Valid(); it.Next() {
			if len(out.Histories) >= 512 {
				return fleet.ErrCorrupt
			}
			b, e := it.Item().ValueCopy(nil)
			if e != nil {
				return e
			}
			var intent delivery.Intent
			if strictDelivery(b, &intent) != nil || intent.Run != root.Run || intent.Contract != root.Contract || intent.RepositoryID != root.RepositoryID || intent.Repository != root.Repository || intent.Base != root.Base || intent.Branch != root.Branch || !bytes.Equal(it.Item().Key(), key(familyContributionPublication, fmt.Sprint(root.RepositoryID), root.Base, "slot", intent.ID)) {
				return fleet.ErrCorrupt
			}
			h, e := loadDeliveryHistory(txn, intent.ID)
			if e != nil || len(h) == 0 || h[0].Record.Intent != intent {
				return fleet.ErrCorrupt
			}
			for _, v := range h {
				if v.Record.ProofJSON != "" || v.Record.ProofDigest != "" {
					if _, e := delivery.ParseReadback(intent, v.Record.Observation); e != nil {
						return fleet.ErrCorrupt
					}
				}
			}
			out.Histories = append(out.Histories, h)
		}
		if delivery.ValidateHistorySnapshot(out) != nil {
			return fleet.ErrCorrupt
		}
		return nil
	})
	if err != nil {
		return ContributionDeliveryHistory{}, err
	}
	return
}
