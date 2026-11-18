package badger

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	badgerdb "github.com/dgraph-io/badger/v4"
)

// Legacy physical families are shared. Never infer meaning from a printable ID
// or turn a failed reference lookup into permission to ignore a record.
func recoveryUnique(results ...error) error {
	valid := 0
	for _, err := range results {
		if err == nil {
			valid++
		}
	}
	if valid != 1 {
		return fleet.ErrCorrupt
	}
	return nil
}

func recoveryExact(tx *badgerdb.Txn, k, want []byte) error {
	got, err := get(tx, k)
	if err != nil || !bytes.Equal(got, want) {
		return fleet.ErrCorrupt
	}
	return nil
}

func recoveryPresentationReplay(tx *badgerdb.Txn, kind, principal, request string, binding, record []byte) error {
	wire, err := json.Marshal(presentationReplay{Binding: binding, Record: record})
	if err != nil {
		return err
	}
	return recoveryExact(tx, key(familyQueuePresentationRequest, kind, principal, request), wire)
}

// Each immutable revision proves its predecessor and current head, and every
// physical record is visited by recovery. This proves contiguous history without
// trusting a head alone or repeatedly walking an unbounded history per record.
func recoveryArchive(tx *badgerdb.Txn, p []string, b []byte, head bool) error {
	v, err := queue.UnmarshalArchive(b)
	if err != nil || recoveryCanonical(b, queue.UnmarshalArchive) != nil {
		return fleet.ErrCorrupt
	}
	want := key(familyQueueArchive, v.PrincipalID, v.QueueItem.ID, revisionPart(v.Revision))
	if head {
		want = key(familyQueueArchiveHead, v.PrincipalID, v.QueueItem.ID)
	}
	family := familyQueueArchive
	if head {
		family = familyQueueArchiveHead
	}
	if !bytes.Equal(key(family, p...), want) {
		return fleet.ErrCorrupt
	}
	item, err := queuePresentationOwner(tx, v.PrincipalID, v.QueueItem)
	if err != nil {
		return err
	}
	if err = queuePresentationTerminal(tx, item); err != nil {
		return err
	}
	latest, err := loadArchive(tx, v.PrincipalID, v.QueueItem)
	if err != nil || latest.Revision < v.Revision {
		return fleet.ErrCorrupt
	}
	if err = recoveryExact(tx, key(familyQueueArchive, v.PrincipalID, v.QueueItem.ID, revisionPart(v.Revision)), b); err != nil {
		return err
	}
	if v.Revision > 1 {
		prior, e := get(tx, key(familyQueueArchive, v.PrincipalID, v.QueueItem.ID, revisionPart(v.Revision-1)))
		if e != nil {
			return fleet.ErrCorrupt
		}
		old, e := queue.UnmarshalArchive(prior)
		if e != nil || old.PrincipalID != v.PrincipalID || old.QueueItem != v.QueueItem || old.Revision != v.Revision-1 {
			return fleet.ErrCorrupt
		}
	}
	binding, err := requestBinding(v.PrincipalID, v.SubjectID, v.RequestID, v.QueueItem, v.Archived, v.Revision-1)
	if err != nil {
		return err
	}
	return recoveryPresentationReplay(tx, "archive", v.PrincipalID, v.RequestID, binding, b)
}

func recoveryPreferences(tx *badgerdb.Txn, p []string, b []byte, head bool) error {
	v, err := queue.UnmarshalPreferences(b)
	if err != nil || recoveryCanonical(b, queue.UnmarshalPreferences) != nil {
		return fleet.ErrCorrupt
	}
	want := key(familyQueuePreferences, v.PrincipalID, revisionPart(v.Revision))
	family := familyQueuePreferences
	if head {
		family = familyQueuePreferencesHead
		want = key(family, v.PrincipalID)
	}
	if !bytes.Equal(key(family, p...), want) {
		return fleet.ErrCorrupt
	}
	latest, err := loadQueuePreferences(tx, v.PrincipalID)
	if err != nil || latest.Revision < v.Revision {
		return fleet.ErrCorrupt
	}
	if err = recoveryExact(tx, key(familyQueuePreferences, v.PrincipalID, revisionPart(v.Revision)), b); err != nil {
		return err
	}
	if v.Revision > 1 {
		prior, e := get(tx, key(familyQueuePreferences, v.PrincipalID, revisionPart(v.Revision-1)))
		if e != nil {
			return fleet.ErrCorrupt
		}
		old, e := queue.UnmarshalPreferences(prior)
		if e != nil || old.PrincipalID != v.PrincipalID || old.Revision != v.Revision-1 {
			return fleet.ErrCorrupt
		}
	}
	binding, err := requestBinding(v.PrincipalID, v.SubjectID, v.RequestID, v.Selection, v.Visibility, v.Revision-1)
	if err != nil {
		return err
	}
	return recoveryPresentationReplay(tx, "preferences", v.PrincipalID, v.RequestID, binding, b)
}

func recoveryReplay(tx *badgerdb.Txn, p []string, b []byte) error {
	if len(p) != 3 || recoveryJSON[presentationReplay](b) != nil {
		return fleet.ErrCorrupt
	}
	var r presentationReplay
	if json.Unmarshal(b, &r) != nil {
		return fleet.ErrCorrupt
	}
	switch p[0] {
	case "archive":
		v, err := queue.UnmarshalArchive(r.Record)
		if err != nil || v.PrincipalID != p[1] || v.RequestID != p[2] {
			return fleet.ErrCorrupt
		}
		return recoveryArchive(tx, []string{v.PrincipalID, v.QueueItem.ID, revisionPart(v.Revision)}, r.Record, false)
	case "preferences":
		v, err := queue.UnmarshalPreferences(r.Record)
		if err != nil || v.PrincipalID != p[1] || v.RequestID != p[2] {
			return fleet.ErrCorrupt
		}
		return recoveryPreferences(tx, []string{v.PrincipalID, revisionPart(v.Revision)}, r.Record, false)
	default:
		return fleet.ErrCorrupt
	}
}

func recoveryContribution(tx *badgerdb.Txn, id string) (contribution.Contract, error) {
	c, err := loadContribution(tx, id)
	if err != nil {
		return c, err
	}
	item, err := loadQueueItem(tx, c.QueueItemID)
	if err != nil {
		return c, err
	}
	wire, err := get(tx, key(familySubmission, c.SubmissionID))
	if err != nil {
		return c, err
	}
	sub, err := queue.UnmarshalSubmission(wire)
	if err != nil || sub.SubmissionID != c.SubmissionID || item.GraphRunID != c.RunID || item.Submission.ID != c.SubmissionID || item.Submission.Digest != sub.Digest || item.Snapshot != sub.Snapshot || item.Authority != sub.Authority || c.OwnerID != sub.OwnerID || c.Controller.ID != sub.OwnerAgentID || item.MaxAttempts != 1 {
		return c, fleet.ErrCorrupt
	}
	snapshot, err := loadSnapshot(tx, item.Snapshot.ID)
	if err != nil || snapshot.Digest != item.Snapshot.Digest || len(snapshot.Loops) != 1 || snapshot.Loops[0] != c.Loop {
		return c, fleet.ErrCorrupt
	}
	revision, err := loopRevision(tx, c.Loop.ID, c.Loop.Revision)
	if err != nil || revision.Digest != c.Loop.Digest {
		return c, fleet.ErrCorrupt
	}
	if _, err = contribution.BindLoop(revision, c); err != nil {
		return c, err
	}
	if err = recoveryExact(tx, key(familyContributionRequest, c.OwnerID, c.Key), []byte(c.QueueItemID)); err != nil {
		return c, err
	}
	return c, recoveryExact(tx, issueKey(c), []byte(c.QueueItemID))
}

func recoveryContributionIndex(tx *badgerdb.Txn, family byte, p []string, b []byte) error {
	if len(p) != 2 {
		return fleet.ErrCorrupt
	}
	c, err := recoveryContribution(tx, string(b))
	if err != nil {
		return err
	}
	want := issueKey(c)
	if family == familyContributionRequest {
		want = key(family, c.OwnerID, c.Key)
	}
	if !bytes.Equal(key(family, p...), want) {
		return fleet.ErrCorrupt
	}
	return nil
}

func recoveryControl(tx *badgerdb.Txn, p []string, b []byte) error {
	if len(p) != 2 {
		return fleet.ErrCorrupt
	}
	c, err := decodeContributionControl(b)
	if err != nil || c.QueueItemID != p[0] || c.Sequence == 0 || (p[1] != "head" && p[1] != fmt.Sprint(c.Sequence)) {
		return fleet.ErrCorrupt
	}
	if _, err = recoveryContribution(tx, p[0]); err != nil {
		return err
	}
	latest, err := loadContributionControl(tx, p[0])
	if err != nil || latest.Sequence < c.Sequence {
		return fleet.ErrCorrupt
	}
	return recoveryExact(tx, key(familyContributionControl, p[0], fmt.Sprint(c.Sequence)), b)
}
