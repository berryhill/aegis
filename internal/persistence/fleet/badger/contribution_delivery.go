package badger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"time"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/reference"
	badgerdb "github.com/dgraph-io/badger/v4"
)

const familyContributionDelivery byte = 0x79
const familyContributionPublication byte = 0x7a

// ContributionDeliveryBinding references existing admission and accepted source
// custody. It does not create a candidate, role, epoch, lease or acceptance fact.
// Authority must independently reload the actual current #377 fence and #376
// accepted candidate; a binding alone grants no effect permission.
type ContributionDeliveryBinding struct {
	QueueItemID, OwnerID, Key string
	delivery.Reference
	FenceEpoch        uint64
	RoleBinding, Base string
}
type ContributionDeliveryRepository struct {
	store     *Store
	binding   ContributionDeliveryBinding
	authority delivery.Authority
}

var _ delivery.Repository = (*ContributionDeliveryRepository)(nil)
var deliveryDigest = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
var deliverySHA = regexp.MustCompile(`^[0-9a-f]{40}$`)
var deliveryBranch = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_/-]{0,199}$`)

// ContributionDelivery binds one existing sealed intake. No production authority
// implementation is installed here. Nil or unavailable authority fails closed.
func (s *Store) ContributionDelivery(ctx context.Context, b ContributionDeliveryBinding, a delivery.Authority) (*ContributionDeliveryRepository, error) {
	if s == nil || a == nil || b.FenceEpoch == 0 || !deliveryDigest.MatchString(b.RoleBinding) || !deliveryDigest.MatchString(b.Candidate) || !deliveryDigest.MatchString(b.Contract) || !deliveryBranch.MatchString(b.Base) {
		return nil, delivery.ErrDenied
	}
	r := &ContributionDeliveryRepository{s, b, a}
	err := s.view(ctx, func(txn *badgerdb.Txn) error { _, err := r.contract(txn); return err })
	if err != nil {
		return nil, err
	}
	return r, nil
}
func (r *ContributionDeliveryRepository) contract(txn *badgerdb.Txn) (contribution.Contract, error) {
	b := r.binding
	c, e := loadContribution(txn, b.QueueItemID)
	if e != nil {
		return c, e
	}
	// Also demand canonical intake bytes, including duplicate/unknown-key denial.
	wire, e := get(txn, key(familyContribution, b.QueueItemID))
	if e != nil {
		return c, e
	}
	canonical, e := contribution.Marshal(c)
	if e != nil || !bytes.Equal(wire, canonical) {
		return c, fleet.ErrCorrupt
	}
	if c.OwnerID != b.OwnerID || c.Key != b.Key || c.RunID != b.Run || c.Digest != b.Contract {
		return c, delivery.ErrDenied
	}
	for _, k := range [][]byte{key(familyContributionRequest, c.OwnerID, c.Key), issueKey(c)} {
		v, e := get(txn, k)
		if e != nil || string(v) != c.QueueItemID {
			return c, fleet.ErrCorrupt
		}
	}
	item, e := loadQueueItem(txn, c.QueueItemID)
	if e != nil {
		return c, e
	}
	run, e := loadGraphRun(txn, c.RunID)
	if e != nil {
		return c, e
	}
	if item.GraphRunID != c.RunID || item.Submission.ID != c.SubmissionID || item.MaxAttempts != 1 || run.QueueItem.ID != item.ItemID || run.QueueItem.Digest != item.Digest || run.Snapshot != item.Snapshot || run.Authority != item.Authority {
		return c, fleet.ErrCorrupt
	}
	if _, e = loadContributionControl(txn, c.QueueItemID); e != nil {
		return c, e
	}
	return c, nil
}
func deliveryOperation(op delivery.Operation) bool {
	switch op {
	case delivery.Commit, delivery.Publish, delivery.PullRequest, delivery.UpdatePullRequest, delivery.CheckDelivery, delivery.Merge, delivery.ReconcileIssue, delivery.TransferGraph, delivery.UploadBlob, delivery.UploadTree, delivery.UploadCommit:
		return true
	}
	return false
}
func deliveryPermits(target string, op delivery.Operation) bool {
	switch target {
	case "local-only", "no-publish":
		return op == delivery.Commit
	case "no-merge":
		return op != delivery.Merge && op != delivery.ReconcileIssue
	case "verified-protected-base-merge":
		return true
	}
	return false
}
func (r *ContributionDeliveryRepository) validate(txn *badgerdb.Txn, i delivery.Intent, writing bool) error {
	c, e := r.contract(txn)
	if e != nil {
		return e
	}
	b := r.binding
	if i.Reference != b.Reference || i.FenceEpoch != b.FenceEpoch || i.RoleBinding != b.RoleBinding || i.Actor == "" || i.Repository != c.Repository || i.RepositoryID != c.Source.RepositoryID || i.Base != b.Base || (c.Base != i.Base && c.Base != i.BaseHead) || !deliveryBranch.MatchString(i.Branch) || i.Branch == i.Base || !deliverySHA.MatchString(i.Head) || !deliverySHA.MatchString(i.BaseHead) || !deliveryDigest.MatchString(i.ID) || !deliveryDigest.MatchString(i.Payload) || i.Policy == "" || !deliveryOperation(i.Operation) {
		return delivery.ErrDenied
	}
	if len(i.Actor) > 256 || len(i.Policy) > 1024 || (i.ExpectedOld != "" && !deliverySHA.MatchString(i.ExpectedOld)) {
		return delivery.ErrDenied
	}
	// Recompute the engine's stable effect slot. Object slots are additionally
	// keyed by OID by the engine; their exact membership is checked by Authority.
	if i.Operation != delivery.UploadBlob && i.Operation != delivery.UploadTree && i.Operation != delivery.UploadCommit {
		v := struct {
			Run, Contract, Repository, Base, Branch string
			RepositoryID                            uint64
			Operation                               delivery.Operation
		}{i.Run, i.Contract, i.Repository, i.Base, i.Branch, i.RepositoryID, i.Operation}
		wire, _ := json.Marshal(v)
		if contribution.Digest(wire) != i.ID {
			return delivery.ErrConflict
		}
	}
	control, e := loadContributionControl(txn, c.QueueItemID)
	if e != nil {
		return e
	}
	target := c.Effect
	if control.Sequence > 0 {
		target = control.Delivery
	}
	if writing && (control.Stop || !time.Now().Before(c.Deadline) || !deliveryPermits(target, i.Operation)) {
		return delivery.ErrDenied
	}
	return nil
}
func (r *ContributionDeliveryRepository) external(ctx context.Context, i delivery.Intent, writing bool) error {
	if r == nil || r.store == nil || r.authority == nil || ctx.Err() != nil {
		return delivery.ErrDenied
	}
	if r.authority.Admit(ctx, i, writing) != nil {
		return delivery.ErrDenied
	}
	return nil
}

// Fence calls the external current authority outside Badger's transaction. This
// is repeated by the engine immediately at effect boundaries; it is not a claim
// of atomicity between canonical state, another store and a remote provider.
func (r *ContributionDeliveryRepository) Fence(ctx context.Context, i delivery.Intent, writing bool) error {
	if e := r.external(ctx, i, writing); e != nil {
		return e
	}
	return r.store.view(ctx, func(txn *badgerdb.Txn) error {
		if e := r.validate(txn, i, writing); e != nil {
			return e
		}
		if e := r.owner(txn, i, false); e != nil {
			return e
		}
		h, e := loadDeliveryHistory(txn, i.ID)
		if e != nil {
			return e
		}
		if len(h) == 0 || h[0].Record.Intent != i {
			return delivery.ErrConflict
		}
		return nil
	})
}

// DeliveryObservation retains the exact neutral canonical wire convention.
type DeliveryObservation = delivery.HistoryEnvelope
type deliveryPublication = delivery.PublicationRoot

func deliveryWire(v any) []byte { b, _ := json.Marshal(v); return b }
func strictDelivery(b []byte, v any) error {
	if reference.RejectDuplicateObjectKeys(b) != nil {
		return fleet.ErrCorrupt
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(new(any)) != io.EOF || !bytes.Equal(b, deliveryWire(v)) {
		return fleet.ErrCorrupt
	}
	return nil
}
func (r *ContributionDeliveryRepository) owner(txn *badgerdb.Txn, i delivery.Intent, createMissing bool) error {
	return r.publicationOwner(txn, i, createMissing, true)
}
func (r *ContributionDeliveryRepository) historicalOwner(txn *badgerdb.Txn, i delivery.Intent) error {
	return r.publicationOwner(txn, i, false, false)
}
func (r *ContributionDeliveryRepository) publicationOwner(txn *badgerdb.Txn, i delivery.Intent, createMissing, active bool) error {
	k := key(familyContributionPublication, fmt.Sprint(i.RepositoryID), i.Base)
	wire, found, e := optional(txn, k)
	if e != nil {
		return e
	}
	b := r.binding
	want := deliveryPublication{QueueItemID: b.QueueItemID, OwnerID: b.OwnerID, Key: b.Key, Run: i.Run, Contract: i.Contract, Repository: i.Repository, Base: i.Base, Branch: i.Branch, RepositoryID: i.RepositoryID}
	want.Digest = contribution.Digest(deliveryWire(want))
	if found {
		var stored deliveryPublication
		if strictDelivery(wire, &stored) != nil {
			return fleet.ErrCorrupt
		}
		digest := stored.Digest
		stored.Digest = ""
		if digest != contribution.Digest(deliveryWire(stored)) {
			return fleet.ErrCorrupt
		}
		stored.Digest = digest
		if active && stored == want {
			if _, released, err := optional(txn, key(familyContributionPublication, "released", b.OwnerID, b.Key)); err != nil {
				return err
			} else if released {
				return delivery.ErrConflict
			}
		}
		if stored != want && active {
			if !createMissing {
				return delivery.ErrConflict
			}
			// Do not resurrect an already historical root as the active owner.
			if _, exists, e := optional(txn, key(familyContributionPublication, "root", b.OwnerID, b.Key)); e != nil || exists {
				return delivery.ErrConflict
			}
			if e := publicationHandoffReady(txn, stored); e != nil {
				return e
			}
			if e := txn.Set(k, deliveryWire(want)); e != nil {
				return e
			}
		}
	} else {
		if !createMissing {
			return fleet.ErrCorrupt
		}
		// Missing exclusion state is not a free slot when any previous
		// reservation/root survives. Do not infer quiescence from its absence.
		prefix := key(familyContributionPublication, fmt.Sprint(i.RepositoryID), i.Base, "slot")
		it := txn.NewIterator(badgerdb.DefaultIteratorOptions)
		it.Seek(prefix)
		hasSlots := it.ValidForPrefix(prefix)
		it.Close()
		_, hasRoot, rootErr := optional(txn, key(familyContributionPublication, "root", b.OwnerID, b.Key))
		if hasSlots || hasRoot || rootErr != nil {
			return fleet.ErrCorrupt
		}
		if e := create(txn, k, deliveryWire(want)); e != nil {
			return e
		}
	}
	rootKey := key(familyContributionPublication, "root", b.OwnerID, b.Key)
	rootWire, rootFound, err := optional(txn, rootKey)
	if err != nil {
		return err
	}
	if rootFound {
		if !bytes.Equal(rootWire, deliveryWire(want)) {
			return fleet.ErrCorrupt
		}
	} else {
		if !createMissing {
			return fleet.ErrCorrupt
		}
		if err = create(txn, rootKey, deliveryWire(want)); err != nil {
			return err
		}
	}
	if createMissing {
		// Upgrade a legacy root without discarding its old slots.
		histories, e := publicationHistories(txn, want)
		if e != nil {
			return e
		}
		for _, chain := range histories {
			intent := chain[0].Record.Intent
			if e := txn.Set(publicationSlotKey(want, intent.ID), deliveryWire(intent)); e != nil {
				return e
			}
		}
		if e := txn.Set(key(familyContributionPublication, "indexed", b.OwnerID, b.Key), deliveryWire(want)); e != nil {
			return e
		}
		if e := create(txn, publicationSlotKey(want, i.ID), deliveryWire(i)); e != nil {
			return e
		}
	}
	// Retain the exact slot separately from its observations: deleting an entire
	// history must not manufacture a fresh effect under surviving custody.
	slotKey := key(familyContributionPublication, fmt.Sprint(i.RepositoryID), i.Base, "slot", i.ID)
	slot, exists, e := optional(txn, slotKey)
	if e != nil {
		return e
	}
	if createMissing {
		if exists {
			return fleet.ErrCorrupt
		}
		return create(txn, slotKey, deliveryWire(i))
	}
	if !exists {
		return fleet.ErrCorrupt
	}
	var intent delivery.Intent
	if strictDelivery(slot, &intent) != nil {
		return fleet.ErrCorrupt
	}
	if intent != i {
		return delivery.ErrConflict
	}
	return nil
}
func loadDeliveryHistory(txn *badgerdb.Txn, id string) ([]DeliveryObservation, error) {
	prefix := key(familyContributionDelivery, id)
	headKey := key(familyContributionDelivery, id, "head")
	head, found, e := optional(txn, headKey)
	if e != nil {
		return nil, e
	}
	count := uint64(0)
	opts := badgerdb.DefaultIteratorOptions
	opts.Prefix = prefix
	it := txn.NewIterator(opts)
	defer it.Close()
	for it.Rewind(); it.Valid(); it.Next() {
		if !bytes.Equal(it.Item().Key(), headKey) {
			count++
			if count > 2048 {
				return nil, fleet.ErrCorrupt
			}
		}
	}
	if !found {
		if count != 0 {
			return nil, fleet.ErrCorrupt
		}
		return nil, nil
	}
	var h DeliveryObservation
	if strictDelivery(head, &h) != nil || h.Record.Revision != count || count == 0 {
		return nil, fleet.ErrCorrupt
	}
	out := make([]DeliveryObservation, 0, count)
	previous := ""
	for n := uint64(1); n <= count; n++ {
		wire, e := get(txn, key(familyContributionDelivery, id, revisionPart(n)))
		if e != nil {
			return nil, fleet.ErrCorrupt
		}
		var v DeliveryObservation
		if strictDelivery(wire, &v) != nil || delivery.ValidateHistoryEnvelope(v) != nil {
			return nil, fleet.ErrCorrupt
		}
		d := v.Digest
		v.Digest = ""
		if d != contribution.Digest(deliveryWire(v)) || v.PreviousDigest != previous || v.Record.Revision != n || v.Record.Intent.ID != id || v.Record.Outcome != v.Record.Observation.Outcome || !validDeliveryOutcome(v.Record.Outcome) {
			return nil, fleet.ErrCorrupt
		}
		v.Digest = d
		if n == 1 {
			if v.Record.Outcome != delivery.Unknown {
				return nil, fleet.ErrCorrupt
			}
		} else {
			p := out[len(out)-1].Record
			if v.Record.Intent != p.Intent || deliveryTransition(p, v.Record.Observation) != nil {
				return nil, fleet.ErrCorrupt
			}
		}
		out = append(out, v)
		previous = d
	}
	if h != out[len(out)-1] {
		return nil, fleet.ErrCorrupt
	}
	return out, nil
}
func validDeliveryOutcome(o delivery.Outcome) bool { return delivery.ValidHistoryOutcome(o) }
func deliveryTransition(old delivery.Record, o delivery.Observation) error {
	return delivery.ValidateHistoryTransition(old, o)
}
func (r *ContributionDeliveryRepository) append(txn *badgerdb.Txn, record delivery.Record, previous string) error {
	v := DeliveryObservation{Record: record, PreviousDigest: previous}
	v.Digest = contribution.Digest(deliveryWire(v))
	wire := deliveryWire(v)
	if e := create(txn, key(familyContributionDelivery, record.Intent.ID, revisionPart(record.Revision)), wire); e != nil {
		return e
	}
	if e := txn.Set(key(familyContributionDelivery, record.Intent.ID, "head"), wire); e != nil {
		return e
	}
	return appendAudit(txn, fleet.AuditFact{Event: core.AuditEvent{Type: "contribution.delivery", PrincipalID: r.binding.OwnerID, SubjectID: r.binding.OwnerID, Outcome: string(record.Outcome), Reason: "sealed_delivery_observation", Metadata: map[string]string{"queue_item_id": r.binding.QueueItemID, "intent": record.Intent.ID, "observation": v.Digest, "revision": fmt.Sprint(record.Revision)}}})
}
func (r *ContributionDeliveryRepository) Reserve(ctx context.Context, i delivery.Intent) (out delivery.Record, fresh bool, err error) {
	// First inspect without allocating: replay remains reconciliation even after a
	// stop or epoch replacement. Changed immutable payloads never obtain a write.
	if err = r.external(ctx, i, false); err != nil {
		return
	}
	err = r.store.view(ctx, func(txn *badgerdb.Txn) error {
		if e := r.validate(txn, i, false); e != nil {
			return e
		}
		h, e := loadDeliveryHistory(txn, i.ID)
		if e != nil {
			return e
		}
		if len(h) > 0 {
			out = h[len(h)-1].Record
			if out.Intent != i {
				return delivery.ErrConflict
			}
			return r.historicalOwner(txn, i)
		}
		return nil
	})
	if err != nil || out.Revision != 0 {
		return
	}
	if err = r.external(ctx, i, true); err != nil {
		return
	}
	err = r.store.update(ctx, func(txn *badgerdb.Txn) error {
		if e := r.validate(txn, i, true); e != nil {
			return e
		}
		h, e := loadDeliveryHistory(txn, i.ID)
		if e != nil {
			return e
		}
		if len(h) > 0 {
			out = h[len(h)-1].Record
			if out.Intent != i {
				return delivery.ErrConflict
			}
			return r.historicalOwner(txn, i)
		}
		if e = r.owner(txn, i, true); e != nil {
			return e
		}
		out = delivery.Record{Intent: i, Outcome: delivery.Unknown, Observation: delivery.Observation{Outcome: delivery.Unknown}, Revision: 1}
		if e = r.append(txn, out, ""); e != nil {
			return e
		}
		fresh = true
		return nil
	})
	if err != nil {
		fresh = false
		return
	}
	if fresh {
		if e := r.Fence(ctx, i, true); e != nil {
			fresh = false
			err = e
		}
	} // Reservation retained on lost admission.
	return
}
func (r *ContributionDeliveryRepository) Finish(ctx context.Context, old delivery.Record, o delivery.Observation) (out delivery.Record, err error) {
	if err = r.external(ctx, old.Intent, false); err != nil {
		return
	}
	err = r.store.update(ctx, func(txn *badgerdb.Txn) error {
		if e := r.validate(txn, old.Intent, false); e != nil {
			return e
		}
		if e := r.owner(txn, old.Intent, false); e != nil {
			return e
		}
		h, e := loadDeliveryHistory(txn, old.Intent.ID)
		if e != nil {
			return e
		}
		if len(h) == 0 {
			return delivery.ErrConflict
		}
		last := h[len(h)-1]
		if last.Record != old {
			return delivery.ErrConflict
		}
		if old.BaseHead != "" && o.BaseHead != "" && old.BaseHead != o.BaseHead {
			o, e = delivery.RetainObservation(old, o)
			if e != nil {
				return e
			}
		}
		if e = deliveryTransition(old, o); e != nil {
			return e
		}
		if old.Observation == o {
			out = old
			return nil
		}
		if old.Revision == ^uint64(0) {
			return delivery.ErrConflict
		}
		out = delivery.Record{Intent: old.Intent, Outcome: o.Outcome, Observation: o, Revision: old.Revision + 1}
		return r.append(txn, out, last.Digest)
	})
	return
}

// History reads exact stored target and observations, checking all history/head
// seals. It grants no write permission and still needs current read admission.
func (r *ContributionDeliveryRepository) History(ctx context.Context, i delivery.Intent) (out []DeliveryObservation, err error) {
	if err = r.external(ctx, i, false); err != nil {
		return
	}
	err = r.store.view(ctx, func(txn *badgerdb.Txn) error {
		if e := r.validate(txn, i, false); e != nil {
			return e
		}
		var e error
		out, e = loadDeliveryHistory(txn, i.ID)
		if e != nil {
			return e
		}
		if len(out) == 0 {
			return fleet.ErrNotFound
		}
		if out[0].Record.Intent != i {
			return delivery.ErrConflict
		}
		return r.historicalOwner(txn, i)
	})
	return
}
