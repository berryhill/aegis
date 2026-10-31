package badger

import (
	"bytes"
	"context"
	"fmt"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/disposition"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	badgerdb "github.com/dgraph-io/badger/v4"
)

// Roots and their slots are immutable history. The repository/base record is
// only the active exclusion fence; replacing it never replaces either history.
func publicationSlotPrefix(root deliveryPublication) []byte {
	return key(familyContributionPublication, "slots", root.OwnerID, root.Key)
}
func publicationSlotKey(root deliveryPublication, id string) []byte {
	return key(familyContributionPublication, "slots", root.OwnerID, root.Key, id)
}
func publicationRootValid(root deliveryPublication) bool {
	seal := root.Digest
	root.Digest = ""
	return deliveryDigest.MatchString(seal) && seal == contribution.Digest(deliveryWire(root))
}

// GetContributionPublicationOwner reads exact active metadata, not authority.
func (s *Store) GetContributionPublicationOwner(ctx context.Context, repositoryID uint64, base string) (out delivery.PublicationRoot, err error) {
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		wire, e := get(txn, key(familyContributionPublication, fmt.Sprint(repositoryID), base))
		if e != nil {
			return e
		}
		if strictDelivery(wire, &out) != nil || !publicationRootValid(out) || out.RepositoryID != repositoryID || out.Base != base {
			return fleet.ErrCorrupt
		}
		retained, e := get(txn, key(familyContributionPublication, "root", out.OwnerID, out.Key))
		if e != nil || !bytes.Equal(retained, wire) {
			return fleet.ErrCorrupt
		}
		return nil
	})
	return
}

// Legacy roots have no root-local index. Their immutable legacy slots remain
// readable; new reservations materialize a complete index in the same txn.
func publicationHistories(txn *badgerdb.Txn, root deliveryPublication) ([][]DeliveryObservation, error) {
	marker, indexed, err := optional(txn, key(familyContributionPublication, "indexed", root.OwnerID, root.Key))
	if err != nil || indexed && !bytes.Equal(marker, deliveryWire(root)) {
		return nil, fleet.ErrCorrupt
	}
	prefix := publicationSlotPrefix(root)
	if !indexed {
		// Legacy compatibility applies only to an intact legacy active root,
		// never to a deleted new-format marker or a displaced historical root.
		local := txn.NewIterator(badgerdb.DefaultIteratorOptions)
		local.Seek(prefix)
		hasLocal := local.ValidForPrefix(prefix)
		local.Close()
		active, e := get(txn, key(familyContributionPublication, fmt.Sprint(root.RepositoryID), root.Base))
		if hasLocal || e != nil || !bytes.Equal(active, deliveryWire(root)) {
			return nil, fleet.ErrCorrupt
		}
		prefix = key(familyContributionPublication, fmt.Sprint(root.RepositoryID), root.Base, "slot")
	}
	opts := badgerdb.DefaultIteratorOptions
	opts.Prefix = prefix
	it := txn.NewIterator(opts)
	defer it.Close()
	var out [][]DeliveryObservation
	for it.Rewind(); it.Valid(); it.Next() {
		wire, e := it.Item().ValueCopy(nil)
		var i delivery.Intent
		if e != nil || strictDelivery(wire, &i) != nil {
			return nil, fleet.ErrCorrupt
		}
		if !indexed && (i.Run != root.Run || i.Contract != root.Contract) {
			continue
		}
		legacy := key(familyContributionPublication, fmt.Sprint(root.RepositoryID), root.Base, "slot", i.ID)
		want := legacy
		if indexed {
			want = publicationSlotKey(root, i.ID)
		}
		if i.Run != root.Run || i.Contract != root.Contract || i.RepositoryID != root.RepositoryID || i.Repository != root.Repository || i.Base != root.Base || i.Branch != root.Branch || !bytes.Equal(it.Item().Key(), want) {
			return nil, fleet.ErrCorrupt
		}
		// The old global key remains an immutable mirror, not an active index.
		if indexed {
			mirror, e := get(txn, legacy)
			if e != nil || !bytes.Equal(mirror, wire) {
				return nil, fleet.ErrCorrupt
			}
		}
		chain, e := loadDeliveryHistory(txn, i.ID)
		if e != nil || len(chain) == 0 || chain[0].Record.Intent != i || len(out) >= 512 {
			return nil, fleet.ErrCorrupt
		}
		for _, entry := range chain {
			if entry.Record.ProofJSON != "" || entry.Record.ProofDigest != "" {
				if _, e := delivery.ParseReadback(i, entry.Record.Observation); e != nil {
					return nil, fleet.ErrCorrupt
				}
			}
		}
		out = append(out, chain)
	}
	if indexed {
		// Both retained indexes must enumerate the same exact membership. Losing
		// a root-local slot must not silently truncate otherwise valid history.
		legacyPrefix := key(familyContributionPublication, fmt.Sprint(root.RepositoryID), root.Base, "slot")
		legacyOpts := badgerdb.DefaultIteratorOptions
		legacyOpts.Prefix = legacyPrefix
		legacy := txn.NewIterator(legacyOpts)
		defer legacy.Close()
		count := 0
		for legacy.Rewind(); legacy.Valid(); legacy.Next() {
			wire, e := legacy.Item().ValueCopy(nil)
			var intent delivery.Intent
			if e != nil || strictDelivery(wire, &intent) != nil {
				return nil, fleet.ErrCorrupt
			}
			if intent.Run != root.Run && intent.Contract != root.Contract {
				continue
			}
			local, e := get(txn, publicationSlotKey(root, intent.ID))
			if e != nil || !bytes.Equal(local, wire) || intent.Run != root.Run || intent.Contract != root.Contract {
				return nil, fleet.ErrCorrupt
			}
			count++
		}
		if count != len(out) {
			return nil, fleet.ErrCorrupt
		}
	}
	return out, nil
}

// Handoff demands the authoritative terminal index AND its exact queue basis,
// closed native producers and every publication slot resolved. In particular a
// merge SHA is not a terminal observation, and UNKNOWN checks/graphs also hold.
func publicationHandoffReady(txn *badgerdb.Txn, root deliveryPublication) error {
	wire, err := get(txn, key(familyContributionPublication, "released", root.OwnerID, root.Key))
	if err != nil || !bytes.Equal(wire, deliveryWire(root)) {
		return delivery.ErrConflict
	}
	return publicationTerminalReady(txn, root)
}

func publicationTerminalReady(txn *badgerdb.Txn, root deliveryPublication) error {
	c, err := loadContribution(txn, root.QueueItemID)
	if err != nil || c.OwnerID != root.OwnerID || c.Key != root.Key || c.RunID != root.Run || c.Digest != root.Contract {
		return delivery.ErrConflict
	}
	rootWire, err := get(txn, key(familyContributionPublication, "root", root.OwnerID, root.Key))
	if err != nil || !bytes.Equal(rootWire, deliveryWire(root)) {
		return fleet.ErrCorrupt
	}
	// Legacy custody is retained conservatively until its active owner has
	// materialized a complete root-local index through normal reservation.
	marker, indexed, err := optional(txn, key(familyContributionPublication, "indexed", root.OwnerID, root.Key))
	if err != nil || !indexed || !bytes.Equal(marker, rootWire) {
		return delivery.ErrConflict
	}
	id, err := get(txn, key(familyDispositionByRun, root.Run))
	if err != nil {
		return delivery.ErrConflict
	}
	wire, err := get(txn, key(familyDisposition, string(id)))
	if err != nil {
		return fleet.ErrCorrupt
	}
	d, err := disposition.Unmarshal(wire)
	item, itemErr := loadQueueItem(txn, c.QueueItemID)
	projection, projectionErr := loadQueueProjection(txn, c.QueueItemID)
	if err != nil || itemErr != nil || projectionErr != nil || d.DispositionID != string(id) || d.GraphRunID != root.Run || d.QueueItem != digestRef(item.ItemID, item.Digest) || (string(d.State) != "succeeded" && string(d.State) != "failed" && string(d.State) != "denied" && string(d.State) != "cancelled" && string(d.State) != "expired" && string(d.State) != "revoked") || string(projection.State) != string(d.State) || d.OccurredAt != projection.UpdatedAt || validateProjectionBasis(txn, projection) != nil {
		return delivery.ErrConflict
	}
	state, _, err := loadContributionContinuity(txn, c.QueueItemID)
	if err != nil || !contribution.NativeRoleQuiescent(state, "builder") || !contribution.NativeRoleQuiescent(state, "tester") {
		return delivery.ErrConflict
	}
	controller, ok := state.RoleAuthorities["controller"]
	stop, stopped := state.RoleStops["controller"]
	if !ok || !stopped || !stop.Acknowledged || stop.Authority != controller || stop.Epoch != controller.Epoch {
		return delivery.ErrConflict
	}
	for _, effect := range state.NativeEffects {
		if effect.State != "completed" {
			return delivery.ErrConflict
		}
	}
	histories, err := publicationHistories(txn, root)
	if err != nil || len(histories) == 0 {
		return delivery.ErrConflict
	}
	for _, chain := range histories {
		if chain[len(chain)-1].Record.Outcome == delivery.Unknown {
			return delivery.ErrConflict
		}
	}
	return nil
}

// ReleaseContributionPublication seals the exact closed root without deleting
// custody or history. The owning application drains publication calls first;
// this transaction independently demands terminal state and actual Stop proofs.
func (s *Store) ReleaseContributionPublication(ctx context.Context, owner, k string) error {
	return s.update(ctx, func(txn *badgerdb.Txn) error {
		wire, err := get(txn, key(familyContributionPublication, "root", owner, k))
		var root deliveryPublication
		if err != nil || strictDelivery(wire, &root) != nil || !publicationRootValid(root) || root.OwnerID != owner || root.Key != k {
			return fleet.ErrCorrupt
		}
		active, err := get(txn, key(familyContributionPublication, fmt.Sprint(root.RepositoryID), root.Base))
		if err != nil || !bytes.Equal(active, wire) {
			return delivery.ErrConflict
		}
		if err = publicationTerminalReady(txn, root); err != nil {
			return err
		}
		marker := key(familyContributionPublication, "released", owner, k)
		prior, found, err := optional(txn, marker)
		if err != nil {
			return err
		}
		if found {
			if !bytes.Equal(prior, wire) {
				return fleet.ErrCorrupt
			}
			return nil
		}
		return create(txn, marker, wire)
	})
}
