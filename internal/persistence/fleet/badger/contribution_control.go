package badger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/reference"
	badgerdb "github.com/dgraph-io/badger/v4"
)

const familyContributionControl byte = 0x73

func decodeContributionControl(b []byte) (contribution.Control, error) {
	var c contribution.Control
	if reference.RejectDuplicateObjectKeys(b) != nil {
		return c, fleet.ErrCorrupt
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil || d.Decode(new(any)) != io.EOF {
		return c, fleet.ErrCorrupt
	}
	digest := c.Digest
	c.Digest = ""
	canonical, _ := json.Marshal(c)
	c.Digest = digest
	if digest != contribution.Digest(canonical) {
		return c, fleet.ErrCorrupt
	}
	wire, _ := json.Marshal(c)
	if !bytes.Equal(wire, b) {
		return c, fleet.ErrCorrupt
	}
	return c, nil
}
func loadContributionControl(txn *badgerdb.Txn, id string) (contribution.Control, error) {
	var out contribution.Control
	contract, e := loadContribution(txn, id)
	if e != nil {
		return out, e
	}
	b, found, e := optional(txn, key(familyContributionControl, id, "head"))
	if e != nil {
		return out, e
	}
	// Enumerate history as well: a missing or rolled-back head cannot hide controls.
	prefix := key(familyContributionControl, id)
	opts := badgerdb.DefaultIteratorOptions
	opts.Prefix = prefix
	it := txn.NewIterator(opts)
	defer it.Close()
	count := uint64(0)
	for it.Rewind(); it.Valid(); it.Next() {
		if !bytes.Equal(it.Item().Key(), key(familyContributionControl, id, "head")) {
			count++
		}
	}
	if !found {
		if count != 0 {
			return out, fleet.ErrCorrupt
		}
		return out, nil
	}
	head, e := decodeContributionControl(b)
	if e != nil || head.Sequence == 0 || head.Sequence != count {
		return out, fleet.ErrCorrupt
	}
	base := contract.Effect
	for seq := uint64(1); seq <= count; seq++ {
		wire, e := get(txn, key(familyContributionControl, id, fmt.Sprint(seq)))
		if e != nil {
			return out, fleet.ErrCorrupt
		}
		c, e := decodeContributionControl(wire)
		if e != nil || c.QueueItemID != id || c.ContractDigest != contract.Digest || c.OwnerID != contract.OwnerID || c.SessionID == "" || c.Sequence != seq || c.PreviousDigest != out.Digest || out.Stop || c.At.IsZero() || !contribution.DeliveryNarrows(base, c.Delivery) {
			return out, fleet.ErrCorrupt
		}
		out = c
		base = c.Delivery
	}
	if out != head {
		return out, fleet.ErrCorrupt
	}
	return out, nil
}
func (s *Store) GetContributionControl(ctx context.Context, id string) (out contribution.Control, err error) {
	err = s.view(ctx, func(txn *badgerdb.Txn) error { var e error; out, e = loadContributionControl(txn, id); return e })
	return
}
func (s *Store) AppendContributionControl(ctx context.Context, c contribution.Control) error {
	return s.update(ctx, func(txn *badgerdb.Txn) error {
		contract, e := loadContribution(txn, c.QueueItemID)
		if e != nil {
			return e
		}
		prior, e := loadContributionControl(txn, c.QueueItemID)
		if e != nil {
			return e
		}
		base := contract.Effect
		if prior.Sequence > 0 {
			base = prior.Delivery
		}
		if c.OwnerID != contract.OwnerID || c.SessionID == "" || c.ContractDigest != contract.Digest || c.Sequence != prior.Sequence+1 || c.PreviousDigest != prior.Digest || prior.Stop || !contribution.DeliveryNarrows(base, c.Delivery) || c.At.IsZero() {
			return fleet.ErrConflict
		}
		c.Digest = ""
		b, _ := json.Marshal(c)
		c.Digest = contribution.Digest(b)
		b, _ = json.Marshal(c)
		if e = create(txn, key(familyContributionControl, c.QueueItemID, fmt.Sprint(c.Sequence)), b); e != nil {
			return e
		}
		if e = txn.Set(key(familyContributionControl, c.QueueItemID, "head"), b); e != nil {
			return e
		}
		return appendAudit(txn, fleet.AuditFact{Event: core.AuditEvent{Type: "contribution.control", SubjectID: c.OwnerID, PrincipalID: c.OwnerID, Outcome: "recorded", Reason: "authenticated_contract_bound_control", Metadata: map[string]string{"queue_item_id": c.QueueItemID, "contract_digest": c.ContractDigest, "control_digest": c.Digest, "sequence": fmt.Sprint(c.Sequence), "session_id": c.SessionID}}})
	})
}
