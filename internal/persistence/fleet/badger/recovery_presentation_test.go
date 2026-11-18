package badger

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/disposition"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	badgerdb "github.com/dgraph-io/badger/v4"
)

func recoveryArchiveFixture(t *testing.T) (*Store, queue.Archive) {
	t.Helper()
	ctx := context.Background()
	s, a := lifecycleFixture(t, ctx, filepath.Join(t.TempDir(), schemaVersion), "recovery-presentation")
	t.Cleanup(func() { _ = s.Close() })
	var err error
	a.Submission.OwnerAgentID = a.Snapshot.Participants[0].ID
	a.Submission.OwnerID = "operator-primary"
	a.Submission, err = queue.NewSubmission(a.Submission)
	if err != nil {
		t.Fatal(err)
	}
	a.QueueItem.Submission = lifecycleDigestRef(a.Submission.SubmissionID, a.Submission.Digest)
	a.QueueItem, err = queue.NewItem(a.QueueItem)
	if err != nil {
		t.Fatal(err)
	}
	a.GraphRun.QueueItem = lifecycleDigestRef(a.QueueItem.ItemID, a.QueueItem.Digest)
	a.GraphRun, err = execution.NewGraphRun(a.GraphRun)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AcceptSubmission(ctx, a, audit("submission.accepted", a.Submission.SubmissionID)); err != nil {
		t.Fatal(err)
	}
	ref := a.GraphRun.QueueItem
	cancel := mustQueueCancellation(t, queue.Cancellation{CancellationID: "recovery-cancel", QueueItem: ref, Reason: "operator cancelled", OccurredAt: a.Submission.SubmittedAt})
	tr := mustQueueTransition(t, queue.QueueTransition{TransitionID: "recovery-terminal", QueueItemID: ref.ID, From: queue.StateQueued, To: queue.StateCancelled, Reason: cancel.Reason, OccurredAt: cancel.OccurredAt})
	d, err := disposition.New(disposition.Record{DispositionID: "recovery-disposition", GraphRunID: a.GraphRun.GraphRunID, QueueItem: ref, Authority: a.QueueItem.Authority, State: execution.StateCancelled, ReasonCode: cancel.Reason, OccurredAt: cancel.OccurredAt})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.CancelQueueItem(ctx, fleet.CancellationMutation{Cancellation: cancel, Transition: tr, Disposition: d}, audit("queue.cancelled", cancel.CancellationID)); err != nil {
		t.Fatal(err)
	}
	var v queue.Archive
	for i, id := range []string{"archive-one", "unarchive-two"} {
		v, err = queue.NewArchive(queue.Archive{PrincipalID: "operator-primary", SubjectID: "operator", RequestID: id, QueueItem: ref, Archived: i == 0, Revision: uint64(i + 1), UpdatedAt: cancel.OccurredAt})
		if err != nil {
			t.Fatal(err)
		}
		if _, fresh, e := s.SetQueueArchive(ctx, v, uint64(i)); e != nil || !fresh {
			t.Fatalf("archive writer: %v %v", fresh, e)
		}
	}
	return s, v
}

func recoveryPreferencesFixture(t *testing.T) (*Store, queue.Preferences) {
	t.Helper()
	s := newDeliveryStore(t)
	var v queue.Preferences
	for i, id := range []string{"preferences-one", "preferences-two"} {
		var err error
		v, err = queue.NewPreferences(queue.Preferences{PrincipalID: "principal", SubjectID: "operator", RequestID: id, Selection: queue.SelectionAll, Visibility: queue.VisibilityAll, Revision: uint64(i + 1), UpdatedAt: time.Now().UTC()})
		if err != nil {
			t.Fatal(err)
		}
		if _, fresh, e := s.SetQueuePreferences(context.Background(), v, uint64(i)); e != nil || !fresh {
			t.Fatalf("preferences writer: %v %v", fresh, e)
		}
	}
	return s, v
}

func checkPresentationRecovery(t *testing.T, s *Store, valid bool) {
	t.Helper()
	before := recoveryInventory(t, s)
	_, _, err := verifyFleetRecovery(context.Background(), s.db)
	if (err == nil) != valid {
		t.Fatalf("valid=%v recovery=%v", valid, err)
	}
	if !reflect.DeepEqual(before, recoveryInventory(t, s)) {
		t.Fatal("recovery verification mutated logical inventory")
	}
}

func TestRecoveryPresentationHistory(t *testing.T) {
	t.Run("archive", func(t *testing.T) { s, _ := recoveryArchiveFixture(t); checkPresentationRecovery(t, s, true) })
	t.Run("preferences", func(t *testing.T) { s, _ := recoveryPreferencesFixture(t); checkPresentationRecovery(t, s, true) })
}

func TestRecoveryArchiveMalformed(t *testing.T) {
	for _, mutation := range []string{"missing-head", "rolled-back-head", "missing-history", "missing-replay", "replay-binding", "replay-key", "history-key", "foreign-owner", "missing-disposition", "orphan-head", "extra-arity"} {
		t.Run(mutation, func(t *testing.T) {
			s, v := recoveryArchiveFixture(t)
			checkPresentationRecovery(t, s, true)
			err := s.update(context.Background(), func(tx *badgerdb.Txn) error {
				history := key(familyQueueArchive, v.PrincipalID, v.QueueItem.ID, revisionPart(1))
				head := key(familyQueueArchiveHead, v.PrincipalID, v.QueueItem.ID)
				replay := key(familyQueuePresentationRequest, "archive", v.PrincipalID, v.RequestID)
				switch mutation {
				case "missing-head":
					return tx.Delete(head)
				case "missing-history":
					return tx.Delete(history)
				case "missing-replay":
					return tx.Delete(replay)
				case "missing-disposition":
					item, e := loadQueueItem(tx, v.QueueItem.ID)
					if e != nil {
						return e
					}
					return tx.Delete(key(familyDispositionByRun, item.GraphRunID))
				case "rolled-back-head":
					b, e := get(tx, history)
					if e != nil {
						return e
					}
					return tx.Set(head, b)
				case "replay-binding":
					b, e := get(tx, replay)
					if e != nil {
						return e
					}
					var r presentationReplay
					if e = json.Unmarshal(b, &r); e != nil {
						return e
					}
					r.Binding = []byte("wrong")
					b, e = json.Marshal(r)
					if e != nil {
						return e
					}
					return tx.Set(replay, b)
				case "replay-key":
					b, e := get(tx, replay)
					if e != nil {
						return e
					}
					return tx.Set(key(familyQueuePresentationRequest, "archive", v.PrincipalID, "wrong"), b)
				case "history-key":
					b, e := get(tx, history)
					if e != nil {
						return e
					}
					return tx.Set(key(familyQueueArchive, v.PrincipalID, v.QueueItem.ID, revisionPart(3)), b)
				case "foreign-owner":
					v.PrincipalID = "foreign"
					v, e := queue.NewArchive(v)
					if e != nil {
						return e
					}
					b, e := queue.MarshalArchive(v)
					if e != nil {
						return e
					}
					return tx.Set(key(familyQueueArchiveHead, v.PrincipalID, v.QueueItem.ID), b)
				case "orphan-head":
					return tx.Delete(key(familyQueueArchive, v.PrincipalID, v.QueueItem.ID, revisionPart(v.Revision)))
				case "extra-arity":
					b, e := get(tx, history)
					if e != nil {
						return e
					}
					return tx.Set(key(familyQueueArchive, v.PrincipalID, v.QueueItem.ID, revisionPart(1), "extra"), b)
				}
				return errors.New("unknown mutation")
			})
			if err != nil {
				t.Fatal(err)
			}
			checkPresentationRecovery(t, s, false)
		})
	}
}

func TestRecoveryPreferencesMalformed(t *testing.T) {
	for _, mutation := range []string{"missing-head", "rolled-back-head", "missing-history", "missing-replay", "replay-record", "replay-binding", "replay-kind", "head-owner", "history-key", "orphan-head", "extra-arity", "control-shaped-orphan"} {
		t.Run(mutation, func(t *testing.T) {
			s, v := recoveryPreferencesFixture(t)
			checkPresentationRecovery(t, s, true)
			err := s.update(context.Background(), func(tx *badgerdb.Txn) error {
				history := key(familyQueuePreferences, v.PrincipalID, revisionPart(1))
				head := key(familyQueuePreferencesHead, v.PrincipalID)
				replay := key(familyQueuePresentationRequest, "preferences", v.PrincipalID, v.RequestID)
				switch mutation {
				case "missing-head":
					return tx.Delete(head)
				case "missing-history":
					return tx.Delete(history)
				case "missing-replay":
					return tx.Delete(replay)
				case "rolled-back-head":
					b, e := get(tx, history)
					if e != nil {
						return e
					}
					return tx.Set(head, b)
				case "replay-record", "replay-binding":
					b, e := get(tx, replay)
					if e != nil {
						return e
					}
					var r presentationReplay
					if e = json.Unmarshal(b, &r); e != nil {
						return e
					}
					if mutation == "replay-binding" {
						r.Binding = []byte("wrong")
					} else {
						r.Record, e = get(tx, history)
						if e != nil {
							return e
						}
					}
					b, e = json.Marshal(r)
					if e != nil {
						return e
					}
					return tx.Set(replay, b)
				case "replay-kind":
					b, e := get(tx, replay)
					if e != nil {
						return e
					}
					return tx.Set(key(familyQueuePresentationRequest, "unknown", v.PrincipalID, v.RequestID), b)
				case "head-owner":
					b, e := get(tx, head)
					if e != nil {
						return e
					}
					return tx.Set(key(familyQueuePreferencesHead, "foreign"), b)
				case "history-key":
					b, e := get(tx, history)
					if e != nil {
						return e
					}
					return tx.Set(key(familyQueuePreferences, v.PrincipalID, revisionPart(3)), b)
				case "orphan-head":
					return tx.Delete(key(familyQueuePreferences, v.PrincipalID, revisionPart(v.Revision)))
				case "extra-arity":
					b, e := get(tx, history)
					if e != nil {
						return e
					}
					return tx.Set(key(familyQueuePreferences, v.PrincipalID, revisionPart(1), "extra"), b)
				case "control-shaped-orphan":
					b, e := get(tx, history)
					if e != nil {
						return e
					}
					return tx.Set(key(familyContributionControl, v.PrincipalID, "1"), b)
				}
				return errors.New("unknown mutation")
			})
			if err != nil {
				t.Fatal(err)
			}
			checkPresentationRecovery(t, s, false)
		})
	}
}

func TestRecoveryUniqueInterpretation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		results []error
		valid   bool
	}{
		{"none", []error{fleet.ErrNotFound, fleet.ErrCorrupt}, false},
		{"first", []error{nil, fleet.ErrCorrupt}, true},
		{"second", []error{fleet.ErrNotFound, nil}, true},
		{"ambiguous", []error{nil, nil}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if (recoveryUnique(tc.results...) == nil) != tc.valid {
				t.Fatal("interpretation cardinality not enforced")
			}
		})
	}
}
