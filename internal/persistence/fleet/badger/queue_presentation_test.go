package badger

import (
	"context"
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

func TestQueuePresentationArchiveDurableOwnedTerminal(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), schemaVersion)
	s, accepted := lifecycleFixture(t, ctx, root, "presentation")
	defer func() { _ = s.Close() }()
	// Add immutable ownership through the real constructors before admission.
	var err error
	accepted.Submission.OwnerAgentID = accepted.Snapshot.Participants[0].ID
	accepted.Submission.OwnerID = "operator-primary"
	accepted.Submission, err = queue.NewSubmission(accepted.Submission)
	if err != nil {
		t.Fatal(err)
	}
	accepted.QueueItem.Submission = lifecycleDigestRef(accepted.Submission.SubmissionID, accepted.Submission.Digest)
	accepted.QueueItem, err = queue.NewItem(accepted.QueueItem)
	if err != nil {
		t.Fatal(err)
	}
	accepted.GraphRun.QueueItem = lifecycleDigestRef(accepted.QueueItem.ItemID, accepted.QueueItem.Digest)
	accepted.GraphRun, err = execution.NewGraphRun(accepted.GraphRun)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AcceptSubmission(ctx, accepted, audit("submission.accepted", accepted.Submission.SubmissionID)); err != nil {
		t.Fatal(err)
	}
	ref := accepted.GraphRun.QueueItem
	v, err := queue.NewArchive(queue.Archive{PrincipalID: "operator-primary", SubjectID: "operator", RequestID: "archive-1", QueueItem: ref, Archived: true, Revision: 1, UpdatedAt: accepted.Submission.SubmittedAt})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.SetQueueArchive(ctx, v, 0); !errors.Is(err, fleet.ErrPresentationDenied) {
		t.Fatalf("nonterminal: %v", err)
	}
	cancellation := mustQueueCancellation(t, queue.Cancellation{CancellationID: "presentation-cancel", QueueItem: ref, Reason: "operator cancelled", OccurredAt: v.UpdatedAt})
	tr := mustQueueTransition(t, queue.QueueTransition{TransitionID: "presentation-terminal", QueueItemID: ref.ID, From: queue.StateQueued, To: queue.StateCancelled, Reason: cancellation.Reason, OccurredAt: v.UpdatedAt})
	d, err := disposition.New(disposition.Record{DispositionID: "presentation-disposition", GraphRunID: accepted.GraphRun.GraphRunID, QueueItem: ref, Authority: accepted.QueueItem.Authority, State: execution.StateCancelled, ReasonCode: cancellation.Reason, OccurredAt: v.UpdatedAt})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.CancelQueueItem(ctx, fleet.CancellationMutation{Cancellation: cancellation, Transition: tr, Disposition: d}, audit("queue.cancelled", cancellation.CancellationID)); err != nil {
		t.Fatal(err)
	}
	before, _ := s.GetQueueProjection(ctx, ref.ID)
	audits, _ := s.AuditEvents(ctx)
	got, created, err := s.SetQueueArchive(ctx, v, 0)
	if err != nil || !created || got != v {
		t.Fatalf("archive: %+v %v %v", got, created, err)
	}
	if _, created, err = s.SetQueueArchive(ctx, v, 0); err != nil || created {
		t.Fatalf("replay: %v %v", created, err)
	}
	conflict := v
	conflict.Archived = false
	conflict, err = queue.NewArchive(conflict)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.SetQueueArchive(ctx, conflict, 0); !errors.Is(err, fleet.ErrConflict) {
		t.Fatalf("conflicting replay: %v", err)
	}
	conflict.RequestID = "stale"
	conflict, _ = queue.NewArchive(conflict)
	if _, _, err = s.SetQueueArchive(ctx, conflict, 0); !errors.Is(err, fleet.ErrConflict) {
		t.Fatalf("stale revision: %v", err)
	}
	foreign := v
	foreign.PrincipalID = "foreign"
	foreign, _ = queue.NewArchive(foreign)
	if _, _, err = s.SetQueueArchive(ctx, foreign, 0); !errors.Is(err, fleet.ErrPresentationDenied) {
		t.Fatalf("foreign: %v", err)
	}
	stale := ref
	stale.Digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err = s.GetQueueArchive(ctx, v.PrincipalID, stale); !errors.Is(err, fleet.ErrPresentationDenied) {
		t.Fatalf("stale digest: %v", err)
	}
	missing := ref
	missing.ID = "missing"
	if _, err = s.GetQueueArchive(ctx, v.PrincipalID, missing); !errors.Is(err, fleet.ErrPresentationDenied) {
		t.Fatalf("missing identity: %v", err)
	}
	v.Archived = false
	v.Revision = 2
	v.RequestID = "unarchive-2"
	v, _ = queue.NewArchive(v)
	if _, created, err = s.SetQueueArchive(ctx, v, 1); err != nil || !created {
		t.Fatalf("unarchive: %v %v", created, err)
	}
	after, _ := s.GetQueueProjection(ctx, ref.ID)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("presentation changed canonical projection")
	}
	assertAcceptedReadback(t, s, accepted)
	events, _ := s.AuditEvents(ctx)
	if len(events) != len(audits)+2 {
		t.Fatalf("audit delta: %d", len(events)-len(audits))
	}
	if err = s.view(ctx, func(txn *badgerdb.Txn) error {
		wire, e := get(txn, key(familyQueueArchive, v.PrincipalID, ref.ID, revisionPart(1)))
		if e != nil {
			return e
		}
		old, e := queue.UnmarshalArchive(wire)
		if e == nil && !old.Archived {
			t.Fatal("old revision overwritten")
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if got, err = s.GetQueueArchive(ctx, v.PrincipalID, ref); err != nil || got != v {
		t.Fatalf("reopened archive: %+v %v", got, err)
	}
}

func TestQueuePresentationPreferencesReplayAndReopen(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), schemaVersion)
	s, err := Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	v, err := queue.NewPreferences(queue.Preferences{PrincipalID: "principal", SubjectID: "operator", RequestID: "prefs-1", Selection: queue.SelectionSucceeded, Visibility: queue.VisibilityUnarchived, Revision: 1, UpdatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if _, created, err := s.SetQueuePreferences(ctx, v, 0); err != nil || !created {
		t.Fatalf("set: %v %v", created, err)
	}
	if _, created, err := s.SetQueuePreferences(ctx, v, 0); err != nil || created {
		t.Fatalf("replay: %v %v", created, err)
	}
	v.Selection = queue.SelectionAll
	v, _ = queue.NewPreferences(v)
	if _, _, err = s.SetQueuePreferences(ctx, v, 0); !errors.Is(err, fleet.ErrConflict) {
		t.Fatalf("conflict: %v", err)
	}
	v.RequestID = "prefs-2"
	v.Revision = 2
	v, _ = queue.NewPreferences(v)
	if _, _, err = s.SetQueuePreferences(ctx, v, 1); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetQueuePreferences(ctx, "foreign"); !errors.Is(err, fleet.ErrNotFound) {
		t.Fatalf("foreign preferences: %v", err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.GetQueuePreferences(ctx, v.PrincipalID); err != nil || got != v {
		t.Fatalf("reopen: %+v %v", got, err)
	}
	events, err := s.AuditEvents(ctx)
	if err != nil || len(events) != 2 {
		t.Fatalf("audit: %d %v", len(events), err)
	}
}
