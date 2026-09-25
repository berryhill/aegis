package implementation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestStepCheckpointAppendReadAndStalePredecessor(t *testing.T) {
	executor, _ := fixture(t)
	ctx := context.Background()
	store := StepCheckpointStore{DB: executor.DB, RunID: "one-run", RevisionDigest: "sha256:" + strings.Repeat("a", 64)}
	if _, err := store.Load(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("initial cursor: %v", err)
	}
	first, err := store.Append(ctx, "", json.RawMessage(`{"step":"gate","attempt":0}`))
	if err != nil || first.Sequence != 0 || first.Digest == "" {
		t.Fatalf("first checkpoint: %+v %v", first, err)
	}
	second, err := store.Append(ctx, first.Digest, json.RawMessage(`{"step":"implement","attempt":1}`))
	if err != nil || second.Sequence != 1 || second.PreviousDigest != first.Digest {
		t.Fatalf("second checkpoint: %+v %v", second, err)
	}
	loaded, err := store.Load(ctx)
	if err != nil || loaded.Digest != second.Digest {
		t.Fatalf("chain readback: %+v %v", loaded, err)
	}
	history, err := store.History(ctx)
	if err != nil || len(history) != 2 || history[0].Digest != first.Digest || history[1].Digest != second.Digest {
		t.Fatalf("ordered history: %+v %v", history, err)
	}
	if _, err := store.Append(ctx, first.Digest, json.RawMessage(`{"step":"wrong"}`)); err == nil {
		t.Fatal("stale cursor overwrote winner")
	}
	other := store
	other.RevisionDigest = "sha256:" + strings.Repeat("b", 64)
	if _, err := other.Load(ctx); err == nil {
		t.Fatal("cursor accepted substituted revision")
	}
}

func TestStepCheckpointRejectsTamperAndUnboundedInput(t *testing.T) {
	executor, _ := fixture(t)
	ctx := context.Background()
	store := StepCheckpointStore{DB: executor.DB, RunID: "one-run", RevisionDigest: "sha256:" + strings.Repeat("a", 64)}
	first, err := store.Append(ctx, "", json.RawMessage(`{"step":"gate"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(ctx, first.Digest, json.RawMessage(`{"step":`)); err == nil {
		t.Fatal("malformed cursor accepted")
	}
	if _, err := store.Append(ctx, first.Digest, json.RawMessage(`"`+strings.Repeat("x", maxStepCursorBytes)+`"`)); err == nil {
		t.Fatal("oversized cursor accepted")
	}
	if err := executor.DB.Put(store.key(0), []byte(`{"run_id":"one-run","revision_digest":"sha256:fake","sequence":0,"payload":{"step":"gate"},"digest":"sha256:fake"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(ctx); err == nil {
		t.Fatal("tampered cursor accepted")
	}

}
