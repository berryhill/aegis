package badger

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"sort"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	badgerdb "github.com/dgraph-io/badger/v4"
)

const (
	familyLoopAvailabilityEvent byte = 0x27
	familyLoopAvailabilityHead  byte = 0x28
)

var _ fleet.LoopAvailabilityRepository = (*Store)(nil)

type availabilityRecord struct {
	Event  loop.AvailabilityEvent `json:"event"`
	Audit  core.AuditEvent        `json:"audit"`
	Digest string                 `json:"digest"`
}

func decodeAvailabilityRecord(data []byte) (availabilityRecord, error) {
	var r availabilityRecord
	if err := json.Unmarshal(data, &r); err != nil {
		return r, fleet.ErrCorrupt
	}
	digest := r.Digest
	r.Digest = ""
	if digest != core.Digest(r) {
		return r, fleet.ErrCorrupt
	}
	r.Digest = digest
	wire, err := json.Marshal(r)
	if err != nil || !bytes.Equal(wire, data) {
		return r, fleet.ErrCorrupt
	}
	if _, err := loop.MarshalAvailabilityEvent(r.Event); err != nil {
		return r, fleet.ErrCorrupt
	}
	return r, nil
}

// availabilityPublication verifies the exact immutable publication bundle.
func availabilityPublication(txn *badgerdb.Txn, ref loop.ProvenanceRevision) (loop.PublicationProvenance, error) {
	revision, err := loopRevision(txn, ref.ID, ref.Revision)
	if err != nil {
		return loop.PublicationProvenance{}, err
	}
	if revision.Digest != ref.Digest {
		return loop.PublicationProvenance{}, fleet.ErrConflict
	}
	data, err := get(txn, key(familyLoopProvenance, ref.ID, revisionPart(ref.Revision)))
	if err != nil {
		return loop.PublicationProvenance{}, fleet.ErrCorrupt
	}
	p, err := loop.UnmarshalPublicationProvenance(data)
	if err != nil || p.Loop != ref {
		return p, fleet.ErrCorrupt
	}
	data, err = get(txn, key(familyLoopValidation, ref.ID, revisionPart(ref.Revision), p.ValidationDigest))
	if err != nil {
		return p, fleet.ErrCorrupt
	}
	v, err := loop.UnmarshalLoopValidationResult(data)
	if err != nil || v.LoopID != ref.ID || v.Revision != ref.Revision || v.RevisionDigest != ref.Digest || v.Digest != p.ValidationDigest || v.Outcome != loop.ValidationValid {
		return p, fleet.ErrCorrupt
	}
	return p, nil
}

func availabilityLatest(txn *badgerdb.Txn, id string) (loop.ProvenanceRevision, error) {
	data, err := get(txn, key(familyLoopLatest, id))
	if err != nil {
		return loop.ProvenanceRevision{}, err
	}
	if len(data) != 8 || binary.BigEndian.Uint64(data) == 0 {
		return loop.ProvenanceRevision{}, fleet.ErrCorrupt
	}
	revision, err := loopRevision(txn, id, binary.BigEndian.Uint64(data))
	if err != nil {
		return loop.ProvenanceRevision{}, fleet.ErrCorrupt
	}
	ref := loop.NewProvenanceRevision(id, revision.Revision, revision.Digest)
	_, err = availabilityPublication(txn, ref)
	return ref, err
}

// Stable identity comes from revision one's publisher and owner. Legacy runtime
// publications without OwnerID resolve it from that exact publisher's Registry
// revision, never from the mutation caller or a later Loop publication.
func availabilityOwner(txn *badgerdb.Txn, id string) (string, string, error) {
	first, err := loopRevision(txn, id, 1)
	if err != nil {
		return "", "", fleet.ErrCorrupt
	}
	p, err := availabilityPublication(txn, loop.NewProvenanceRevision(id, 1, first.Digest))
	if err != nil {
		return "", "", fleet.ErrCorrupt
	}
	agent, err := agentRevision(txn, p.PublisherAgent.ID, p.PublisherAgent.Revision)
	if err != nil || agent.Digest != p.PublisherAgent.Digest {
		return "", "", fleet.ErrCorrupt
	}
	owner := agent.Ownership.OwnerID
	if p.OwnerID != "" && p.OwnerID != owner {
		return "", "", fleet.ErrCorrupt
	}
	return p.PublisherAgent.ID, owner, nil
}

func availabilityHistory(txn *badgerdb.Txn, id string) ([]loop.AvailabilityEvent, error) {
	latest, err := availabilityLatest(txn, id)
	if err != nil {
		return nil, err
	}
	publisher, owner, err := availabilityOwner(txn, id)
	if err != nil {
		return nil, err
	}
	events := []loop.AvailabilityEvent{}
	err = scan(txn, familyLoopAvailabilityEvent, func(k, data []byte) error {
		r, err := decodeAvailabilityRecord(data)
		if err != nil {
			return err
		}
		parts, err := decodeKeyParts(k, familyLoopAvailabilityEvent)
		if err != nil || len(parts) != 1 || parts[0] != r.Event.EventID {
			return fleet.ErrCorrupt
		}
		if r.Event.LoopID == id {
			events = append(events, r.Event)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Sequence < events[j].Sequence })
	state, digest := loop.AvailabilityPublished, ""
	for i, e := range events {
		if e.Sequence != uint64(i+1) || e.ExpectedState != state || e.PreviousDigest != digest || e.PublisherAgent.ID != publisher || e.OwnerID != owner || e.Head.Revision > latest.Revision {
			return nil, fleet.ErrCorrupt
		}
		if _, err := availabilityPublication(txn, e.Head); err != nil {
			return nil, fleet.ErrCorrupt
		}
		actor, err := agentRevision(txn, e.PublisherAgent.ID, e.PublisherAgent.Revision)
		if err != nil || actor.Digest != e.PublisherAgent.Digest || actor.Ownership.OwnerID != owner {
			return nil, fleet.ErrCorrupt
		}
		state, digest = e.State, e.Digest
	}
	head, found, err := optional(txn, key(familyLoopAvailabilityHead, id))
	if err != nil {
		return nil, err
	}
	if (len(events) == 0 && found) || (len(events) > 0 && (!found || string(head) != digest)) {
		return nil, fleet.ErrCorrupt
	}
	return events, nil
}

func (s *Store) ListLoopAvailabilityEvents(ctx context.Context, id string) (out []loop.AvailabilityEvent, err error) {
	err = s.view(ctx, func(txn *badgerdb.Txn) error { out, err = availabilityHistory(txn, id); return err })
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) GetLoopAvailability(ctx context.Context, id string) (out loop.AvailabilityStatus, err error) {
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		events, err := availabilityHistory(txn, id)
		if err != nil {
			return err
		}
		out = loop.AvailabilityStatus{LoopID: id, State: loop.AvailabilityPublished}
		if len(events) > 0 {
			last := events[len(events)-1]
			out.State, out.Sequence, out.Digest = last.State, last.Sequence, last.Digest
		}
		return nil
	})
	return
}

// AppendLoopAvailability atomically CASes availability and publication head,
// persists immutable intent history, and appends one authoritative audit fact.
// Authentication/expiry admission is the application's responsibility, including
// on replay. No availability decision grants execution authority.
func (s *Store) AppendLoopAvailability(ctx context.Context, event loop.AvailabilityEvent, fact fleet.AuditFact) (out loop.AvailabilityEvent, idempotent bool, err error) {
	if _, err = loop.MarshalAvailabilityEvent(event); err != nil {
		return
	}
	wantType := "loop.archived"
	if event.State == loop.AvailabilityPublished {
		wantType = "loop.unarchived"
	}
	if fact.Event.Type != wantType || fact.Event.SubjectID != event.LoopID || fact.Event.PrincipalID != event.PrincipalID || fact.Event.Outcome != "succeeded" {
		return out, false, errors.New("availability audit does not bind mutation")
	}
	err = s.update(ctx, func(txn *badgerdb.Txn) error {
		events, err := availabilityHistory(txn, event.LoopID)
		if err != nil {
			return err
		}
		publisher, owner, err := availabilityOwner(txn, event.LoopID)
		if err != nil {
			return err
		}
		actor, err := agentRevision(txn, event.PublisherAgent.ID, event.PublisherAgent.Revision)
		if err != nil || actor.Digest != event.PublisherAgent.Digest || actor.Ownership.OwnerID != owner || event.OwnerID != owner || event.PublisherAgent.ID != publisher {
			return fleet.ErrConflict
		}
		recordKey := key(familyLoopAvailabilityEvent, event.EventID)
		if data, found, err := optional(txn, recordKey); err != nil {
			return err
		} else if found {
			stored, err := decodeAvailabilityRecord(data)
			if err != nil {
				return err
			}
			if !loop.SameAvailabilityIntent(stored.Event, event) || core.Digest(stored.Audit) != core.Digest(fact.Event) {
				return fleet.ErrConflict
			}
			out, idempotent = stored.Event, true
			return nil
		}
		head, err := availabilityLatest(txn, event.LoopID)
		if err != nil {
			return err
		}
		state, digest := loop.AvailabilityPublished, ""
		if len(events) > 0 {
			state, digest = events[len(events)-1].State, events[len(events)-1].Digest
		}
		if event.Head != head || event.ExpectedState != state || event.PreviousDigest != digest || event.Sequence != uint64(len(events))+1 {
			return fleet.ErrConflict
		}
		r := availabilityRecord{Event: event, Audit: fact.Event}
		r.Digest = core.Digest(r)
		wire, err := json.Marshal(r)
		if err != nil {
			return err
		}
		if err = create(txn, recordKey, wire); err != nil {
			return err
		}
		if err = txn.Set(key(familyLoopAvailabilityHead, event.LoopID), []byte(event.Digest)); err != nil {
			return err
		}
		// Copy metadata: callers retain ownership of their map.
		metadata := make(map[string]string, len(fact.Event.Metadata)+2)
		for k, v := range fact.Event.Metadata {
			metadata[k] = v
		}
		metadata["availability_event_id"], metadata["availability_digest"] = event.EventID, event.Digest
		fact.Event.Metadata = metadata
		if err = appendAudit(txn, fact); err != nil {
			return err
		}
		out = event
		return nil
	})
	if err != nil {
		return loop.AvailabilityEvent{}, false, err
	}
	return
}
