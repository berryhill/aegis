package badger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"strconv"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/disposition"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	badgerdb "github.com/dgraph-io/badger/v4"
)

const (
	familyQueueArchive             byte = 0x70
	familyQueueArchiveHead         byte = 0x71
	familyQueuePresentationRequest byte = 0x72
	familyQueuePreferences         byte = 0x73
	familyQueuePreferencesHead     byte = 0x74
)

var _ fleet.QueuePresentationRepository = (*Store)(nil)

// queuePresentationOwner resolves immutable provenance inside the mutation
// transaction. Missing legacy provenance is denied, never inferred from a UI.
func queuePresentationOwner(txn *badgerdb.Txn, principal string, ref reference.DigestRef) (queue.Item, error) {
	item, err := loadQueueItem(txn, ref.ID)
	if errors.Is(err, fleet.ErrNotFound) {
		return item, fleet.ErrPresentationDenied
	}
	if err != nil {
		return item, err
	}
	if ref.Validate() != nil || ref.Digest != item.Digest {
		return item, fleet.ErrPresentationDenied
	}
	wire, err := get(txn, key(familySubmission, item.Submission.ID))
	if errors.Is(err, fleet.ErrNotFound) {
		return item, fleet.ErrCorrupt
	}
	if err != nil {
		return item, err
	}
	submission, err := queue.UnmarshalSubmission(wire)
	if err != nil || submission.SubmissionID != item.Submission.ID || submission.Digest != item.Submission.Digest || submission.Snapshot != item.Snapshot || submission.Authority != item.Authority {
		return item, fleet.ErrCorrupt
	}
	if submission.OwnerAgentID == "" {
		return item, fleet.ErrPresentationDenied
	}
	switch submission.AuthorityKind {
	case "registered-agent-workspace":
		if submission.OwnerID != principal {
			return item, fleet.ErrPresentationDenied
		}
	case "", "runtime-session":
		if submission.OwnerID != "" && submission.OwnerID != principal {
			return item, fleet.ErrPresentationDenied
		}
	default:
		return item, fleet.ErrPresentationDenied
	}
	snapshot, err := loadSnapshot(txn, item.Snapshot.ID)
	if errors.Is(err, fleet.ErrNotFound) {
		return item, fleet.ErrCorrupt
	}
	if err != nil {
		return item, err
	}
	if snapshot.Digest != item.Snapshot.Digest {
		return item, fleet.ErrCorrupt
	}
	for _, participant := range snapshot.Participants {
		if participant.ID != submission.OwnerAgentID {
			continue
		}
		wire, err = get(txn, key(familyAgentRevision, participant.ID, revisionPart(participant.Revision)))
		if err != nil {
			return item, err
		}
		agent, e := registry.UnmarshalAgentRevision(wire)
		if e != nil || agent.AgentID != participant.ID || agent.Revision != participant.Revision || agent.Digest != participant.Digest {
			return item, fleet.ErrCorrupt
		}
		if agent.Ownership.OwnerID != principal {
			return item, fleet.ErrPresentationDenied
		}
		return item, nil
	}
	return item, fleet.ErrPresentationDenied
}

func queuePresentationTerminal(txn *badgerdb.Txn, item queue.Item) error {
	p, err := loadQueueProjection(txn, item.ItemID)
	if err != nil {
		return err
	}
	if err = validateProjectionBasis(txn, p); err != nil {
		return err
	}
	switch p.State {
	case queue.StateSucceeded, queue.StateFailed, queue.StateDenied, queue.StateCancelled, queue.StateExpired, queue.StateRevoked:
	default:
		return fleet.ErrPresentationDenied
	}
	if p.ActiveClaimID != "" {
		return fleet.ErrPresentationDenied
	}
	id, err := get(txn, key(familyDispositionByRun, item.GraphRunID))
	if err != nil {
		return err
	}
	wire, err := get(txn, key(familyDisposition, string(id)))
	if err != nil {
		return err
	}
	d, err := disposition.Unmarshal(wire)
	if err != nil || d.DispositionID != string(id) || d.GraphRunID != item.GraphRunID || d.QueueItem != digestRef(item.ItemID, item.Digest) || string(d.State) != string(p.State) {
		return fleet.ErrCorrupt
	}
	return nil
}

func loadArchive(txn *badgerdb.Txn, principal string, ref reference.DigestRef) (queue.Archive, error) {
	wire, err := get(txn, key(familyQueueArchiveHead, principal, ref.ID))
	if err != nil {
		return queue.Archive{}, err
	}
	v, err := queue.UnmarshalArchive(wire)
	if err != nil || v.PrincipalID != principal || v.QueueItem != ref {
		return queue.Archive{}, fleet.ErrCorrupt
	}
	canonical, err := get(txn, key(familyQueueArchive, principal, ref.ID, revisionPart(v.Revision)))
	if err != nil || !bytes.Equal(wire, canonical) {
		return queue.Archive{}, fleet.ErrCorrupt
	}
	return v, nil
}
func (s *Store) GetQueueArchive(ctx context.Context, principal string, ref reference.DigestRef) (out queue.Archive, err error) {
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		if _, e := queuePresentationOwner(txn, principal, ref); e != nil {
			return e
		}
		out, err = loadArchive(txn, principal, ref)
		return err
	})
	return
}

func presentationAudit(kind, principal, subject, request, digest string, revision uint64, metadata map[string]string) fleet.AuditFact {
	metadata["request_id"], metadata["record_digest"], metadata["revision"] = request, digest, strconv.FormatUint(revision, 10)
	return fleet.AuditFact{Event: core.AuditEvent{Type: kind, SubjectID: subject, PrincipalID: principal, Outcome: "succeeded", Reason: "authenticated_queue_presentation_change", Metadata: metadata}}
}

type presentationReplay struct {
	Binding []byte          `json:"binding"`
	Record  json.RawMessage `json:"record"`
}

func replayPresentation(txn *badgerdb.Txn, k, binding []byte) ([]byte, bool, error) {
	wire, found, err := optional(txn, k)
	if err != nil || !found {
		return nil, found, err
	}
	var replay presentationReplay
	if json.Unmarshal(wire, &replay) != nil {
		return nil, true, fleet.ErrCorrupt
	}
	if !bytes.Equal(replay.Binding, binding) {
		return nil, true, fleet.ErrConflict
	}
	return replay.Record, true, nil
}
func savePresentationReplay(txn *badgerdb.Txn, k, binding, record []byte) error {
	wire, err := json.Marshal(presentationReplay{Binding: binding, Record: record})
	if err != nil {
		return err
	}
	return create(txn, k, wire)
}

func (s *Store) SetQueueArchive(ctx context.Context, v queue.Archive, expected uint64) (out queue.Archive, created bool, err error) {
	wire, err := queue.MarshalArchive(v)
	if err != nil {
		return out, false, err
	}
	if expected == math.MaxUint64 || v.Revision != expected+1 {
		return out, false, fleet.ErrConflict
	}
	binding, err := requestBinding(v.PrincipalID, v.SubjectID, v.RequestID, v.QueueItem, v.Archived, expected)
	if err != nil {
		return out, false, err
	}
	err = s.update(ctx, func(txn *badgerdb.Txn) error {
		item, e := queuePresentationOwner(txn, v.PrincipalID, v.QueueItem)
		if e != nil {
			return e
		}
		if e = queuePresentationTerminal(txn, item); e != nil {
			return e
		}
		requestKey := key(familyQueuePresentationRequest, "archive", v.PrincipalID, v.RequestID)
		if prior, found, e := replayPresentation(txn, requestKey, binding); e != nil {
			return e
		} else if found {
			out, e = queue.UnmarshalArchive(prior)
			return e
		}
		prior, e := loadArchive(txn, v.PrincipalID, v.QueueItem)
		if e != nil && !errors.Is(e, fleet.ErrNotFound) {
			return e
		}
		if prior.Revision != expected {
			return fleet.ErrConflict
		}
		if e = create(txn, key(familyQueueArchive, v.PrincipalID, v.QueueItem.ID, revisionPart(v.Revision)), wire); e != nil {
			return e
		}
		if e = txn.Set(key(familyQueueArchiveHead, v.PrincipalID, v.QueueItem.ID), wire); e != nil {
			return e
		}
		if e = savePresentationReplay(txn, requestKey, binding, wire); e != nil {
			return e
		}
		kind := "fleet.queue.unarchived"
		if v.Archived {
			kind = "fleet.queue.archived"
		}
		if e = appendAudit(txn, presentationAudit(kind, v.PrincipalID, v.SubjectID, v.RequestID, v.Digest, v.Revision, map[string]string{"queue_item_id": v.QueueItem.ID, "queue_item_digest": v.QueueItem.Digest})); e != nil {
			return e
		}
		out, created = v, true
		return nil
	})
	return
}

func loadQueuePreferences(txn *badgerdb.Txn, principal string) (queue.Preferences, error) {
	wire, err := get(txn, key(familyQueuePreferencesHead, principal))
	if err != nil {
		return queue.Preferences{}, err
	}
	v, err := queue.UnmarshalPreferences(wire)
	if err != nil || v.PrincipalID != principal {
		return queue.Preferences{}, fleet.ErrCorrupt
	}
	canonical, err := get(txn, key(familyQueuePreferences, principal, revisionPart(v.Revision)))
	if err != nil || !bytes.Equal(wire, canonical) {
		return queue.Preferences{}, fleet.ErrCorrupt
	}
	return v, nil
}
func (s *Store) GetQueuePreferences(ctx context.Context, principal string) (out queue.Preferences, err error) {
	err = s.view(ctx, func(txn *badgerdb.Txn) error { out, err = loadQueuePreferences(txn, principal); return err })
	return
}
func (s *Store) SetQueuePreferences(ctx context.Context, v queue.Preferences, expected uint64) (out queue.Preferences, created bool, err error) {
	wire, err := queue.MarshalPreferences(v)
	if err != nil {
		return out, false, err
	}
	if expected == math.MaxUint64 || v.Revision != expected+1 {
		return out, false, fleet.ErrConflict
	}
	binding, err := requestBinding(v.PrincipalID, v.SubjectID, v.RequestID, v.Selection, v.Visibility, expected)
	if err != nil {
		return out, false, err
	}
	err = s.update(ctx, func(txn *badgerdb.Txn) error {
		requestKey := key(familyQueuePresentationRequest, "preferences", v.PrincipalID, v.RequestID)
		if prior, found, e := replayPresentation(txn, requestKey, binding); e != nil {
			return e
		} else if found {
			out, e = queue.UnmarshalPreferences(prior)
			return e
		}
		prior, e := loadQueuePreferences(txn, v.PrincipalID)
		if e != nil && !errors.Is(e, fleet.ErrNotFound) {
			return e
		}
		if prior.Revision != expected {
			return fleet.ErrConflict
		}
		if e = create(txn, key(familyQueuePreferences, v.PrincipalID, revisionPart(v.Revision)), wire); e != nil {
			return e
		}
		if e = txn.Set(key(familyQueuePreferencesHead, v.PrincipalID), wire); e != nil {
			return e
		}
		if e = savePresentationReplay(txn, requestKey, binding, wire); e != nil {
			return e
		}
		if e = appendAudit(txn, presentationAudit("fleet.queue.preferences.changed", v.PrincipalID, v.SubjectID, v.RequestID, v.Digest, v.Revision, map[string]string{"selection": string(v.Selection), "visibility": string(v.Visibility)})); e != nil {
			return e
		}
		out, created = v, true
		return nil
	})
	return
}
