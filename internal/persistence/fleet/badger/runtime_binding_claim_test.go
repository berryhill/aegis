package badger

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	badgerdb "github.com/dgraph-io/badger/v4"
)

// A forged queued projection/transition cannot stand in for an immutable
// runtime binding. Positive binding and reclaim are covered by API TestDQHandoff.
func TestClaimEligibilityRejectsMissingRuntimeBinding(t *testing.T) {
	ctx := context.Background()
	store, accepted := lifecycleFixture(t, ctx, filepath.Join(t.TempDir(), schemaVersion), "missing-binding")
	if _, err := store.AcceptSubmission(ctx, accepted, audit("submission.accepted", accepted.Submission.SubmissionID)); err != nil {
		t.Fatal(err)
	}
	at := accepted.Submission.SubmittedAt.Add(time.Second)
	transition := mustQueueTransition(t, queue.QueueTransition{TransitionID: "forged-binding", QueueItemID: accepted.QueueItem.ItemID, From: queue.StateAwaitingRuntime, To: queue.StateQueued, Reason: "forged binding", OccurredAt: at})
	wire, err := queue.MarshalTransition(transition)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := queue.NewProjection(queue.Projection{QueueItemID: accepted.QueueItem.ItemID, State: queue.StateQueued, AvailableAt: at, UpdatedAt: at, LastTransitionID: transition.TransitionID})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.update(ctx, func(txn *badgerdb.Txn) error {
		return txn.Set(key(familyQueueTransition, accepted.QueueItem.ItemID, transition.TransitionID), wire)
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.view(ctx, func(txn *badgerdb.Txn) error {
		return validateClaimProjectionEligibility(txn, accepted.QueueItem, projection)
	}); !errors.Is(err, fleet.ErrConflict) {
		t.Fatalf("projection without canonical binding must deny: %v", err)
	}
	got, err := store.GetQueueProjection(ctx, accepted.QueueItem.ItemID)
	if err != nil || got.LastTransitionID != accepted.InitialTransition.TransitionID || got.Attempts != 0 {
		t.Fatalf("denied eligibility mutated projection: %v", err)
	}
}
