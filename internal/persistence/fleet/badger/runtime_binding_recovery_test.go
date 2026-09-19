package badger

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	badgerdb "github.com/dgraph-io/badger/v4"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRuntimeBindingRecoveryIntegrity(t *testing.T) {
	for _, mode := range []string{"replay", "projection", "attempts", "disposition"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			s, a := lifecycleFixture(t, ctx, filepath.Join(t.TempDir(), schemaVersion), mode)
			defer s.Close()
			var err error
			a.Submission.AuthorityKind = "registered-agent-workspace"
			a.Submission.OwnerAgentID = a.Snapshot.Participants[0].ID
			a.Submission.OwnerID = "owner"
			a.Submission.MandateID = "workspace"
			a.Submission, err = queue.NewSubmission(a.Submission)
			if err != nil {
				t.Fatal(err)
			}
			a.QueueItem.Submission = lifecycleDigestRef(a.Submission.SubmissionID, a.Submission.Digest)
			a.QueueItem.State = queue.StatePreparationPending
			a.QueueItem, err = queue.NewItem(a.QueueItem)
			if err != nil {
				t.Fatal(err)
			}
			a.GraphRun.QueueItem = lifecycleDigestRef(a.QueueItem.ItemID, a.QueueItem.Digest)
			a.GraphRun, err = execution.NewGraphRun(a.GraphRun)
			if err != nil {
				t.Fatal(err)
			}
			a.InitialTransition.To = queue.StatePreparationPending
			a.InitialTransition, err = queue.NewTransition(a.InitialTransition)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.AcceptSubmission(ctx, a, audit("accepted", a.Submission.SubmissionID)); err != nil {
				t.Fatal(err)
			}
			at := a.Submission.SubmittedAt.Add(time.Second)
			binding, err := queue.NewRuntimeBinding(queue.RuntimeBinding{BindingID: "binding", QueueItem: a.GraphRun.QueueItem, Submission: a.QueueItem.Submission, OwnerAgent: a.Snapshot.Participants[0], Authority: a.Submission.Authority, MandateID: "mandate", Runtime: "hermes-agent", BoundAt: at})
			if err != nil {
				t.Fatal(err)
			}
			tr := mustQueueTransition(t, queue.QueueTransition{TransitionID: "bound", QueueItemID: a.QueueItem.ItemID, From: queue.StatePreparationPending, To: queue.StateQueued, Reason: "bound", OccurredAt: at})
			if mode != "replay" {
				if err = s.update(ctx, func(txn *badgerdb.Txn) error {
					if mode == "disposition" {
						return txn.Set(key(familyDispositionByRun, a.GraphRun.GraphRunID), []byte("terminal"))
					}
					p, e := loadQueueProjection(txn, a.QueueItem.ItemID)
					if e != nil {
						return e
					}
					if mode == "projection" {
						p.LastTransitionID = "absent"
					} else {
						p.Attempts = 1
					}
					p, e = queue.NewProjection(p)
					if e != nil {
						return e
					}
					w, e := queue.MarshalProjection(p)
					if e != nil {
						return e
					}
					return txn.Set(key(familyQueueProjection, p.QueueItemID), w)
				}); err != nil {
					t.Fatal(err)
				}
				if _, err = s.BindQueueRuntime(ctx, binding, tr, audit("bound", "binding")); !errors.Is(err, fleet.ErrConflict) {
					t.Fatalf("accepted corrupt preparation: %v", err)
				}
				if _, err = s.GetQueueRuntimeBinding(ctx, a.QueueItem.ItemID); !errors.Is(err, fleet.ErrNotFound) {
					t.Fatal("persisted denied binding")
				}
				return
			}
			var winners atomic.Int32
			var wg sync.WaitGroup
			for range 8 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					created, e := s.BindQueueRuntime(ctx, binding, tr, audit("bound", "binding"))
					if e != nil {
						t.Errorf("concurrent binding: %v", e)
					}
					if created {
						winners.Add(1)
					}
				}()
			}
			wg.Wait()
			if winners.Load() != 1 {
				t.Fatalf("binding winners: %d", winners.Load())
			}
			binding.BoundAt = at.Add(time.Second)
			binding, err = queue.NewRuntimeBinding(binding)
			if err != nil {
				t.Fatal(err)
			}
			tr.OccurredAt = binding.BoundAt
			tr = mustQueueTransition(t, tr)
			if created, err := s.BindQueueRuntime(ctx, binding, tr, audit("bound", "binding")); err != nil || created {
				t.Fatalf("timestamp-independent replay %v", err)
			}
			got, err := s.GetQueueRuntimeBinding(ctx, a.QueueItem.ItemID)
			if err != nil || got.BoundAt != at {
				t.Fatal("winner changed")
			}
			tr.TransitionID = "different"
			tr = mustQueueTransition(t, tr)
			if _, err = s.BindQueueRuntime(ctx, binding, tr, audit("bound", "binding")); !errors.Is(err, fleet.ErrConflict) {
				t.Fatalf("changed transition accepted: %v", err)
			}
		})
	}
}
