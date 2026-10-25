package badger

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	badgerdb "github.com/dgraph-io/badger/v4"
	"reflect"
)

const familyContributionDecision byte = 0x79

func (s *Store) GetContributionDecision(ctx context.Context, root, id string) (out contribution.Decision, err error) {
	err = s.view(ctx, func(tx *badgerdb.Txn) error {
		b, e := get(tx, key(familyContributionDecision, root, id))
		if e != nil {
			return e
		}
		return json.Unmarshal(b, &out)
	})
	return
}

// ReserveContributionDecision atomically spends the independent cumulative
// delivery budget. A stable intent cannot reserve or prompt a second time.
func (s *Store) ReserveContributionDecision(ctx context.Context, d contribution.Decision) error {
	return s.update(ctx, func(tx *badgerdb.Txn) error {
		c, e := loadContribution(tx, d.QueueItemID)
		if e != nil {
			return e
		}
		state, _, e := loadContributionContinuity(tx, c.QueueItemID)
		if e != nil {
			return e
		}
		control, e := loadContributionControl(tx, c.QueueItemID)
		if e != nil {
			return e
		}
		role, ok := contribution.SealedRole(c, d.Role)
		if !ok || role != d.Binding || d.OwnerID != c.OwnerID || d.Key != c.Key || d.ContractDigest != c.Digest || d.ControlDigest != control.Digest || control.Stop || state.CancelRequested || d.Stage != state.Stage || d.Candidate != state.Candidate || !contribution.ValidKey(d.ID) || d.Status != "requested" || d.PlanID == "" || d.PlanDigest == "" || d.ExpiresAt.After(c.Deadline) {
			return fleet.ErrConflict
		}
		if _, found, e := optional(tx, key(familyContributionDecision, c.QueueItemID, d.ID)); e != nil {
			return e
		} else if found {
			return fleet.ErrConflict
		}
		if state.Sequence >= contributionContinuityMax-contributionContinuitySafetyTail-1 {
			return contribution.ErrContinuityExhausted
		}
		// Use the existing reducer and log: this does not reset original authority.
		at := d.RequestedAt
		event, e := contribution.SealContinuityEvent(contribution.ContinuityEvent{SchemaVersion: contribution.ContinuitySchema, QueueItemID: c.QueueItemID, ContractDigest: c.Digest, OwnerID: c.OwnerID, SessionID: c.SessionID, Sequence: state.Sequence + 1, PreviousDigest: state.Digest, Kind: "reserve", Counter: "decision-delivery", OperationID: d.ID, At: at})
		if e != nil {
			return e
		}
		if _, e = contribution.ApplyContinuity(c, state, event); e != nil {
			return e
		}
		wire, _ := json.Marshal(event)
		if e = create(tx, key(familyContributionContinuity, c.QueueItemID, fmt.Sprint(event.Sequence)), wire); e != nil {
			return e
		}
		if e = tx.Set(key(familyContributionContinuity, c.QueueItemID, "head"), wire); e != nil {
			return e
		}
		b, _ := json.Marshal(d)
		return create(tx, key(familyContributionDecision, c.QueueItemID, d.ID), b)
	})
}

func (s *Store) UpdateContributionDecision(ctx context.Context, before, after contribution.Decision) error {
	return s.update(ctx, func(tx *badgerdb.Txn) error {
		b, e := get(tx, key(familyContributionDecision, before.QueueItemID, before.ID))
		if e != nil {
			return e
		}
		var current contribution.Decision
		if json.Unmarshal(b, &current) != nil {
			return fleet.ErrCorrupt
		}
		if !reflect.DeepEqual(current, before) || !contribution.DecisionTransition(before.Status, after.Status) {
			return fleet.ErrConflict
		}
		immutable := after
		immutable.Status = before.Status
		immutable.ApprovalID = before.ApprovalID
		immutable.ReceiptID = before.ReceiptID
		if (before.ApprovalID != "" && after.ApprovalID != before.ApprovalID) || (before.ReceiptID != "" && after.ReceiptID != before.ReceiptID) {
			return fleet.ErrConflict
		}
		if !reflect.DeepEqual(immutable, before) {
			return fleet.ErrConflict
		}
		b, e = json.Marshal(after)
		if e != nil {
			return e
		}
		return tx.Set(key(familyContributionDecision, before.QueueItemID, before.ID), b)
	})
}
