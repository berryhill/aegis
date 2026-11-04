package badger

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	badgerdb "github.com/dgraph-io/badger/v4"
)

// A valid receipt for another published Loop revision is not evidence for the
// admitted pin. Exercise the exactNodeEvidence seam as well as atomic completion
// so a preceding Graph-membership guard cannot mask this regression.
func TestCompletionRejectsForeignPinnedLoopEvidenceAtomically(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), schemaVersion)
	records, completion, blobs := completionFixture(t, ctx, root)
	defer func() { _ = records.Close() }()
	attempt, err := records.GetAttempt(ctx, completion.Disposition.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	if err := records.view(ctx, func(txn *badgerdb.Txn) error {
		if !exactNodeEvidence(txn, attempt, completion) {
			t.Fatal("positive pinned evidence control rejected")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	pinned, _ := loopFixture(t)
	foreign, _ := loopFixture(t)
	foreign.Revision, foreign.PreviousDigest, foreign.Digest = 2, pinned.Digest, ""
	for i := range foreign.Steps {
		if foreign.Steps[i].ID == "echo" {
			foreign.Steps[i].EvidenceClaims[0].Claim = "foreign-output"
		}
	}
	foreign.RequiredEvidence[0].Claim = "foreign-output"
	foreign, validation, err := loop.NewRevision(foreign)
	if err != nil || len(validation.Issues) != 0 {
		t.Fatalf("foreign definition: %v %+v", err, validation)
	}
	if _, err := records.PublishLoop(ctx, loop.PublishRequest{Revision: foreign, Validation: validation, Provenance: loopPublicationProvenance(t, foreign, validation), IdempotencyKey: "foreign-loop-evidence", ExpectedPreviousDigest: pinned.Digest}, audit("loop.published", foreign.LoopID)); err != nil {
		t.Fatal(err)
	}
	completion.Receipts[0].Claim = foreign.RequiredEvidence[0].Claim
	persistReceiptInto(t, blobs, &completion.Receipts[0])
	sealCompletion(t, &completion, blobs)
	snapshot := func() map[string]string {
		t.Helper()
		out := map[string]string{}
		if err := records.view(ctx, func(txn *badgerdb.Txn) error {
			it := txn.NewIterator(badgerdb.DefaultIteratorOptions)
			defer it.Close()
			for it.Rewind(); it.Valid(); it.Next() {
				item := it.Item()
				value, err := item.ValueCopy(nil)
				if err != nil {
					return err
				}
				out[string(item.KeyCopy(nil))] = string(value)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}
	before := snapshot()
	if err := records.view(ctx, func(txn *badgerdb.Txn) error {
		if exactNodeEvidence(txn, attempt, completion) {
			t.Fatal("foreign revision receipt satisfied pinned Loop")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := records.CompleteQueueItem(ctx, completion, completionAudit(completion), blobs); !errors.Is(err, fleet.ErrConflict) {
		t.Fatalf("foreign evidence completed: %v", err)
	}
	if !reflect.DeepEqual(before, snapshot()) {
		t.Fatal("denial changed canonical records, projection or audit")
	}
	if err := records.Close(); err != nil {
		t.Fatal(err)
	}
	records, err = Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, snapshot()) {
		t.Fatal("reopen changed denied completion history")
	}
	if _, err := records.GetDisposition(ctx, completion.Disposition.DispositionID); !errors.Is(err, fleet.ErrNotFound) {
		t.Fatalf("denied disposition exists: %v", err)
	}
	if _, err := records.GetRuntimeArtifact(ctx, completion.Artifact.ID); !errors.Is(err, fleet.ErrNotFound) {
		t.Fatalf("denied artifact exists: %v", err)
	}
	if _, err := records.GetVerificationReceipt(ctx, completion.Receipts[0].ID); !errors.Is(err, fleet.ErrNotFound) {
		t.Fatalf("denied receipt exists: %v", err)
	}
}
