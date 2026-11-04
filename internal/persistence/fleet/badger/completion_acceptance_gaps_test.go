package badger

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/berryhill/aegis/internal/disposition"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	badgerdb "github.com/dgraph-io/badger/v4"
)

// Snapshot every canonical key, including projections and audit, not just counts.
func acceptanceGapSnapshot(t *testing.T, s *Store) map[string]string {
	t.Helper()
	out := map[string]string{}
	if err := s.view(context.Background(), func(txn *badgerdb.Txn) error {
		it := txn.NewIterator(badgerdb.DefaultIteratorOptions)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			v, err := it.Item().ValueCopy(nil)
			if err != nil {
				return err
			}
			out[string(it.Item().KeyCopy(nil))] = string(v)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestCompletionAllPinnedClaimsReloadedAtomically(t *testing.T) {
	for _, mode := range []string{"missing-first", "missing-second", "duplicate", "wrong-policy", "all", "all-reversed"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			root := filepath.Join(t.TempDir(), schemaVersion)
			s, c, blobs := completionFixture(t, ctx, root, "independent-output")
			defer func() { _ = s.Close() }()
			a, err := s.GetAttempt(ctx, c.Claim.AttemptID)
			if err != nil {
				t.Fatal(err)
			}
			child, err := s.GetLoopExecution(ctx, a.LoopExecutionID)
			if err != nil {
				t.Fatal(err)
			}
			pinned, err := s.GetLoopRevision(ctx, child.Loop.ID, child.Loop.Revision)
			if err != nil || pinned.Digest != child.Loop.Digest || len(pinned.RequiredEvidence) != 2 || pinned.RequiredEvidence[0].Claim == pinned.RequiredEvidence[1].Claim {
				t.Fatalf("two distinct claims were not pinned: %+v %v", pinned, err)
			}
			second := c.Receipts[0]
			second.ID += "-second"
			second.Claim = "independent-output"
			c.Receipts = append(c.Receipts, second)
			switch mode {
			case "missing-first":
				c.Receipts = c.Receipts[1:]
			case "missing-second":
				c.Receipts = c.Receipts[:1]
			case "duplicate":
				c.Receipts[1].Claim = c.Receipts[0].Claim
			case "wrong-policy":
				c.Receipts[1].PolicyVersion = "foreign-policy"
			case "all-reversed":
				c.Receipts[0], c.Receipts[1] = c.Receipts[1], c.Receipts[0]
			}
			c.Disposition.ReceiptIDs = nil
			for i := range c.Receipts {
				persistReceiptInto(t, blobs, &c.Receipts[i])
				wire, err := blobs.GetBlob(c.Receipts[i].EvidenceRef)
				if err != nil {
					t.Fatal(err)
				}
				reloaded, err := evidence.DecodeVerificationReceipt(c.Receipts[i].EvidenceRef, wire)
				if err != nil || reloaded != c.Receipts[i] {
					t.Fatalf("content-addressed receipt reload: %v", err)
				}
				c.Receipts[i] = reloaded
				c.Disposition.ReceiptIDs = append(c.Disposition.ReceiptIDs, reloaded.ID)
			}
			c.Disposition.Digest = ""
			c.Disposition, err = disposition.New(c.Disposition)
			if err != nil {
				t.Fatal(err)
			}
			sealCompletion(t, &c, blobs)
			valid := mode == "all" || mode == "all-reversed"
			if err := s.view(ctx, func(txn *badgerdb.Txn) error {
				if exactNodeEvidence(txn, a, c) != valid {
					t.Fatalf("pinned claim qualification mismatch for %s", mode)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			before := acceptanceGapSnapshot(t, s)
			err = s.CompleteQueueItem(ctx, c, completionAudit(c), blobs)
			if valid {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				if !errors.Is(err, fleet.ErrConflict) {
					t.Fatalf("invalid claims admitted: %v", err)
				}
				if !reflect.DeepEqual(before, acceptanceGapSnapshot(t, s)) {
					t.Fatal("denial mutated canonical bytes")
				}
			}
			after := acceptanceGapSnapshot(t, s)
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			s, err = Open(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after, acceptanceGapSnapshot(t, s)) {
				t.Fatal("reopen changed canonical bytes")
			}
			if !valid {
				if _, err := s.GetDisposition(ctx, c.Disposition.DispositionID); !errors.Is(err, fleet.ErrNotFound) {
					t.Fatalf("denied disposition exists: %v", err)
				}
				return
			}
			got, err := s.GetDisposition(ctx, c.Disposition.DispositionID)
			if err != nil || !reflect.DeepEqual(got, c.Disposition) || got.State != execution.StateSucceeded {
				t.Fatalf("durable success: %+v %v", got, err)
			}
			projection, err := s.GetQueueProjection(ctx, c.Claim.QueueItem.ID)
			if err != nil || projection.State != queue.StateSucceeded {
				t.Fatalf("durable projection: %+v %v", projection, err)
			}
			for _, want := range c.Receipts {
				got, err := s.GetVerificationReceipt(ctx, want.ID)
				if err != nil || got != want {
					t.Fatalf("durable required receipt: %+v %v", got, err)
				}
			}
		})
	}
}

func TestCompletionResealedForeignLineageDeniedAtomically(t *testing.T) {
	for _, mode := range []string{"foreign-run", "foreign-attempt"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			root := filepath.Join(t.TempDir(), schemaVersion)
			s, c, blobs := completionFixture(t, ctx, root)
			defer func() { _ = s.Close() }()
			originalArtifact := *c.Artifact
			originalReceipt := c.Receipts[0]
			if mode == "foreign-run" {
				c.Artifact.RunID = "foreign-child"
				c.Receipts[0].RunID = c.Artifact.RunID
			} else {
				c.Artifact.AttemptID = "foreign-attempt"
				c.Receipts[0].AttemptID = c.Artifact.AttemptID
			}
			persistReceiptInto(t, blobs, &c.Receipts[0])
			sealCompletion(t, &c, blobs)
			a, err := s.GetAttempt(ctx, c.Claim.AttemptID)
			if err != nil {
				t.Fatal(err)
			}
			// Policy qualification still passes: this negative must reach canonical lineage,
			// rather than fail because of stale provenance, audit or foreign Loop policy.
			if err := s.view(ctx, func(txn *badgerdb.Txn) error {
				if !exactNodeEvidence(txn, a, c) {
					t.Fatal("foreign lineage unexpectedly failed policy control")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			before := acceptanceGapSnapshot(t, s)
			if err := s.CompleteQueueItem(ctx, c, completionAudit(c), blobs); !errors.Is(err, fleet.ErrConflict) {
				t.Fatalf("resealed foreign lineage admitted: %v", err)
			}
			if !reflect.DeepEqual(before, acceptanceGapSnapshot(t, s)) {
				t.Fatal("denial changed canonical records")
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			s, err = Open(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, acceptanceGapSnapshot(t, s)) {
				t.Fatal("denial changed after reopen")
			}
			// Exact positive control on the same untouched claim and store.
			*c.Artifact = originalArtifact
			c.Receipts[0] = originalReceipt
			sealCompletion(t, &c, blobs)
			if err := s.CompleteQueueItem(ctx, c, completionAudit(c), blobs); err != nil {
				t.Fatalf("exact lineage positive control failed: %v", err)
			}
		})
	}
}
