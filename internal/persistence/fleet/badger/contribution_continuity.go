package badger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	badgerdb "github.com/dgraph-io/badger/v4"
)

// Dedicated family in the existing fleet database, not a second queue or effect log.
const familyContributionContinuity byte = 0x78

// Preserve the historical hard maximum; ordinary activity cannot consume the
// final cancellation plus request/ack slots for the three sealed roles.
const contributionContinuityMax = 4096
const contributionContinuitySafetyTail = 12

func contributionSafetyEvent(kind string) bool {
	return kind == "cancel-acknowledged" || kind == "native-boundary-closed" || kind == "native-completed" || kind == "native-unknown" || kind == "cancel-requested" || kind == "role-stop-requested" || kind == "role-stop-acknowledged"
}

var _ fleet.ContributionContinuityRepository = (*Store)(nil)

func loadContributionContinuity(txn *badgerdb.Txn, id string) (state contribution.Continuity, events []contribution.ContinuityEvent, err error) {
	c, err := loadContribution(txn, id)
	if err != nil {
		return state, nil, err
	}
	events = make([]contribution.ContinuityEvent, 0)
	head, found, err := optional(txn, key(familyContributionContinuity, id, "head"))
	if err != nil {
		return state, nil, err
	}
	opts := badgerdb.DefaultIteratorOptions
	opts.Prefix = key(familyContributionContinuity, id)
	it := txn.NewIterator(opts)
	defer it.Close()
	var count uint64
	for it.Rewind(); it.Valid(); it.Next() {
		if !bytes.Equal(it.Item().Key(), key(familyContributionContinuity, id, "head")) {
			count++
		}
	}
	if !found {
		if count != 0 {
			return state, nil, fleet.ErrCorrupt
		}
		return state, events, nil
	}
	if count == 0 || count > contributionContinuityMax {
		return state, nil, fleet.ErrCorrupt
	}
	for seq := uint64(1); seq <= count; seq++ {
		b, e := get(txn, key(familyContributionContinuity, id, fmt.Sprint(seq)))
		if e != nil {
			return state, nil, fleet.ErrCorrupt
		}
		event, e := contribution.UnmarshalContinuityEvent(b)
		if e != nil {
			return state, nil, fleet.ErrCorrupt
		}
		state, e = contribution.ApplyContinuity(c, state, event)
		if e != nil {
			return state, nil, fleet.ErrCorrupt
		}
		events = append(events, event)
	}
	expected, _ := json.Marshal(events[len(events)-1])
	if !bytes.Equal(head, expected) {
		return state, nil, fleet.ErrCorrupt
	}
	return state, events, nil
}

func (s *Store) GetContributionContinuity(ctx context.Context, id string) (state contribution.Continuity, events []contribution.ContinuityEvent, err error) {
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		var e error
		state, events, e = loadContributionContinuity(txn, id)
		return e
	})
	return
}

func (s *Store) AppendContributionContinuity(ctx context.Context, event contribution.ContinuityEvent) (out contribution.Continuity, err error) {
	err = s.update(ctx, func(txn *badgerdb.Txn) error {
		c, e := loadContribution(txn, event.QueueItemID)
		if e != nil {
			return e
		}
		prior, _, e := loadContributionContinuity(txn, event.QueueItemID)
		if e != nil {
			return e
		}
		tail := uint64(contributionContinuitySafetyTail)
		if prior.Sequence >= contributionContinuityMax || (!contributionSafetyEvent(event.Kind) && prior.Sequence >= contributionContinuityMax-tail) {
			return contribution.ErrContinuityExhausted
		}
		control, e := loadContributionControl(txn, event.QueueItemID)
		if e != nil {
			return e
		}
		if control.Stop && !contributionSafetyEvent(event.Kind) && event.Kind != "native-completed" && event.Kind != "native-unknown" && event.Kind != "cancel-requested" && event.Kind != "role-stop-requested" && event.Kind != "role-stop-acknowledged" {
			return contribution.ErrContinuityFenced
		}
		if e = checkContributionNativeFence(txn, c, prior, event); e != nil {
			return e
		}
		if e = checkContributionRoleFence(txn, c, event); e != nil {
			return e
		}
		next, e := contribution.ApplyContinuity(c, prior, event)
		if e != nil {
			return e
		}
		// Cancellation fences the existing control consumed by the execution owner
		// in this same transaction. It does NOT cancel the Queue or release custody.
		if event.Kind == "cancel-requested" && !control.Stop {
			delivery := c.Effect
			if control.Sequence > 0 {
				delivery = control.Delivery
			}
			stop := contribution.Control{QueueItemID: c.QueueItemID, ContractDigest: c.Digest, OwnerID: c.OwnerID, SessionID: event.SessionID, Sequence: control.Sequence + 1, PreviousDigest: control.Digest, Stop: true, Delivery: delivery, At: event.At}
			b, _ := json.Marshal(stop)
			stop.Digest = contribution.Digest(b)
			b, _ = json.Marshal(stop)
			if e = create(txn, key(familyContributionControl, c.QueueItemID, fmt.Sprint(stop.Sequence)), b); e != nil {
				return e
			}
			if e = txn.Set(key(familyContributionControl, c.QueueItemID, "head"), b); e != nil {
				return e
			}
			if e = appendAudit(txn, fleet.AuditFact{Event: core.AuditEvent{Type: "contribution.control", SubjectID: c.OwnerID, PrincipalID: c.OwnerID, Outcome: "recorded", Reason: "continuity_cancel_requested", Metadata: map[string]string{"queue_item_id": c.QueueItemID, "control_digest": stop.Digest}}}); e != nil {
				return e
			}
		}
		wire, e := json.Marshal(event)
		if e != nil {
			return e
		}
		if e = create(txn, key(familyContributionContinuity, event.QueueItemID, fmt.Sprint(event.Sequence)), wire); e != nil {
			return e
		}
		if e = txn.Set(key(familyContributionContinuity, event.QueueItemID, "head"), wire); e != nil {
			return e
		}
		if e = appendAudit(txn, fleet.AuditFact{Event: core.AuditEvent{Type: "contribution.continuity", SubjectID: event.OwnerID, PrincipalID: event.OwnerID, Outcome: "recorded", Reason: event.Kind, Metadata: map[string]string{"queue_item_id": event.QueueItemID, "contract_digest": event.ContractDigest, "continuity_digest": event.Digest, "sequence": fmt.Sprint(event.Sequence)}}}); e != nil {
			return e
		}
		out = next
		return nil
	})
	return
}
