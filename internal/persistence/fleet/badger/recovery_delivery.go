package badger

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	badgerdb "github.com/dgraph-io/badger/v4"
)

// Decisions are historical reservations, not current approval authority. Replay
// the normal continuity reducer to bind the exact pre-reservation stage/candidate.
func recoveryDecision(tx *badgerdb.Txn, p []string, b []byte) error {
	var d contribution.Decision
	if len(p) != 2 || strictDelivery(b, &d) != nil || d.QueueItemID != p[0] || d.ID != p[1] || !contribution.ValidKey(d.ID) {
		return fleet.ErrCorrupt
	}
	c, err := loadContribution(tx, p[0])
	if err != nil {
		return err
	}
	role, ok := contribution.SealedRole(c, d.Role)
	if !ok || role != d.Binding || d.OwnerID != c.OwnerID || d.Key != c.Key || d.ContractDigest != c.Digest || d.PlanID == "" || d.PlanDigest == "" || d.ExpiresAt.After(c.Deadline) {
		return fleet.ErrCorrupt
	}
	switch d.Status {
	case "requested", "delivered", "unavailable", "timeout", "unknown", "expired", "dialog-cancelled", "applying", "denied", "unanswered", "approved":
	default:
		return fleet.ErrCorrupt
	}
	_, events, err := loadContributionContinuity(tx, p[0])
	if err != nil {
		return err
	}
	var state contribution.Continuity
	matched := 0
	for _, event := range events {
		if event.Kind == "reserve" && event.Counter == "decision-delivery" && event.OperationID == d.ID {
			if state.Stage != d.Stage || state.Candidate != d.Candidate || !event.At.Equal(d.RequestedAt) {
				return fleet.ErrCorrupt
			}
			matched++
		}
		state, err = contribution.ApplyContinuity(c, state, event)
		if err != nil {
			return err
		}
	}
	if matched != 1 {
		return fleet.ErrCorrupt
	}
	control, err := loadContributionControl(tx, p[0])
	if err != nil {
		return err
	}
	if d.ControlDigest == "" {
		return nil
	} // normal initial control has no digest
	for n := uint64(1); n <= control.Sequence; n++ {
		wire, e := get(tx, key(familyContributionControl, p[0], fmt.Sprint(n)))
		if e != nil {
			return e
		}
		v, e := decodeContributionControl(wire)
		if e != nil {
			return e
		}
		if v.Digest == d.ControlDigest && !v.Stop {
			return nil
		}
	}
	return fleet.ErrCorrupt
}

// A sibling is excluded from a legacy prefix only after canonical corroboration.
func canonicalDecisionSibling(tx *badgerdb.Txn, k, b []byte) bool {
	p, e := decodeKeyParts(k, familyContributionDecision)
	return e == nil && recoveryDecision(tx, p, b) == nil
}

func recoveryDeliveryIntent(tx *badgerdb.Txn, i delivery.Intent) error {
	run, err := loadGraphRun(tx, i.Run)
	if err != nil {
		return err
	}
	c, err := loadContribution(tx, run.QueueItem.ID)
	if err != nil {
		return err
	}
	r := &ContributionDeliveryRepository{binding: ContributionDeliveryBinding{QueueItemID: c.QueueItemID, OwnerID: c.OwnerID, Key: c.Key, Reference: i.Reference, FenceEpoch: i.FenceEpoch, RoleBinding: i.RoleBinding, Base: i.Base}}
	if i.FenceEpoch == 0 || !deliveryDigest.MatchString(i.Candidate) || !deliveryDigest.MatchString(i.RoleBinding) {
		return fleet.ErrCorrupt
	}
	if err = r.validate(tx, i, false); err != nil {
		return err
	}
	return r.historicalOwner(tx, i)
}

func recoveryDelivery(tx *badgerdb.Txn, p []string, b []byte) error {
	if len(p) != 2 {
		return fleet.ErrCorrupt
	}
	var v DeliveryObservation
	if strictDelivery(b, &v) != nil || v.Record.Intent.ID != p[0] {
		return fleet.ErrCorrupt
	}
	if p[1] != "head" && (len(p[1]) != 8 || binary.BigEndian.Uint64([]byte(p[1])) != v.Record.Revision) {
		return fleet.ErrCorrupt
	}
	h, err := loadDeliveryHistory(tx, p[0])
	if err != nil || len(h) == 0 {
		return fleet.ErrCorrupt
	}
	if err = recoveryDeliveryIntent(tx, v.Record.Intent); err != nil {
		return err
	}
	return nil
}

func recoveryPublication(tx *badgerdb.Txn, p []string, b []byte) error {
	if len(p) == 4 {
		var i delivery.Intent
		if strictDelivery(b, &i) != nil {
			return fleet.ErrCorrupt
		}
		run, err := loadGraphRun(tx, i.Run)
		if err != nil {
			return err
		}
		c, err := loadContribution(tx, run.QueueItem.ID)
		if err != nil {
			return err
		}
		local := key(familyContributionPublication, "slots", c.OwnerID, c.Key, i.ID)
		legacy := key(familyContributionPublication, fmt.Sprint(i.RepositoryID), i.Base, "slot", i.ID)
		k := key(familyContributionPublication, p...)
		if !bytes.Equal(k, local) && !bytes.Equal(k, legacy) {
			return fleet.ErrCorrupt
		}
		h, err := loadDeliveryHistory(tx, i.ID)
		if err != nil || len(h) == 0 || h[0].Record.Intent != i {
			return fleet.ErrCorrupt
		}
		return recoveryDeliveryIntent(tx, i)
	}
	var root deliveryPublication
	if strictDelivery(b, &root) != nil || !publicationRootValid(root) {
		return fleet.ErrCorrupt
	}
	if len(p) == 2 {
		if p[0] != fmt.Sprint(root.RepositoryID) || p[1] != root.Base {
			return fleet.ErrCorrupt
		}
	} else if len(p) == 3 {
		if (p[0] != "root" && p[0] != "indexed" && p[0] != "released") || p[1] != root.OwnerID || p[2] != root.Key {
			return fleet.ErrCorrupt
		}
	} else {
		return fleet.ErrCorrupt
	}
	if err := recoveryExact(tx, key(familyContributionPublication, "root", root.OwnerID, root.Key), b); err != nil {
		return err
	}
	active, err := get(tx, key(familyContributionPublication, fmt.Sprint(root.RepositoryID), root.Base))
	if err != nil {
		return err
	}
	if !bytes.Equal(active, b) || (len(p) == 3 && p[0] == "released") {
		if err = publicationHandoffReady(tx, root); err != nil {
			return err
		}
	}
	h, err := publicationHistories(tx, root)
	if err != nil || len(h) == 0 {
		return fleet.ErrCorrupt
	}
	for _, chain := range h {
		if err = recoveryDeliveryIntent(tx, chain[0].Record.Intent); err != nil {
			return err
		}
	}
	return nil
}
