package badger

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSyncAndCloseAlwaysReleasesAfterSyncFailure(t *testing.T) {
	syncErr, closeErr := errors.New("sync failed"), errors.New("close failed")
	calls := 0
	err := syncAndClose(func() error { calls++; return syncErr }, func() error { calls++; return closeErr })
	if calls != 2 || !errors.Is(err, syncErr) || !errors.Is(err, closeErr) {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestCloseSyncFailureNeverPublishesClean(t *testing.T) {
	root := authorityRoot(t)
	if _, err := Initialize(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	store, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	// A closed underlying DB deterministically rejects Sync, without host faults.
	if err = store.db.Close(); err != nil {
		t.Fatal(err)
	}
	first := store.Close()
	if first == nil {
		t.Fatal("sync failure hidden")
	}
	if store.Close() != first {
		t.Fatal("repeated close lost original error")
	}
	if _, err = os.Stat(filepath.Join(root, "CLEAN")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("false CLEAN: %v", err)
	}
	if _, err = os.Stat(filepath.Join(root, "DIRTY")); err != nil {
		t.Fatal(err)
	}
	lease, err := acquireMaintenance(context.Background(), root, true)
	if err != nil {
		t.Fatal(err)
	}
	lease.release()
}
