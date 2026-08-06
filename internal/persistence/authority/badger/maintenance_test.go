package badger

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	badgerdb "github.com/dgraph-io/badger/v4"
)

func TestMaintenanceBackupRestoreActivateRollbackAndGarbageCollect(t *testing.T) {
	ctx := context.Background()
	root := authorityRoot(t)
	original, err := Initialize(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	mandate, _ := badgerAuthorityBinding("mandate-backup", "authority-backup", "session-backup")
	if err = store.CreateMandate(ctx, mandate); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}

	backup := filepath.Join(t.TempDir(), "exports", "authority.aegis-backup")
	manifest, err := Export(ctx, root, backup)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Format != BackupFormat || manifest.Generation != original || manifest.RecordCount < 4 || manifest.PayloadBytes == 0 || len(manifest.PayloadSHA256) != 64 {
		t.Fatalf("incomplete backup manifest: %+v", manifest)
	}
	if info, statErr := os.Lstat(backup); statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		t.Fatalf("backup publication is not a secure regular file: info=%v err=%v", info, statErr)
	}

	restored, err := Import(ctx, root, backup)
	if err != nil {
		t.Fatal(err)
	}
	if restored.GenerationID == original.GenerationID || restored.StoreID == original.StoreID || restored.Directory == original.Directory {
		t.Fatalf("restore reused source identity: source=%+v restored=%+v", original, restored)
	}
	if active, readErr := readMarker(filepath.Join(root, "ACTIVE")); readErr != nil || active != original {
		t.Fatalf("import selected the candidate: active=%+v err=%v", active, readErr)
	}
	if err = verifyStoreIdentity(filepath.Join(root, "stores", restored.Directory), restored); err != nil {
		t.Fatalf("restored candidate is not independently verified: %v", err)
	}

	activated, err := Activate(ctx, root, restored)
	if err != nil {
		t.Fatal(err)
	}
	if !activated.SelectionCommitted || !activated.PreviousRetired || activated.Previous != original || activated.GenerationID != restored.GenerationID || activated.Activation != original.Activation+1 {
		t.Fatalf("unexpected activation: %+v", activated)
	}
	assertStoredMandate(t, root, mandate.ID)

	rolledBack, err := Rollback(ctx, root, original)
	if err != nil {
		t.Fatal(err)
	}
	if !rolledBack.SelectionCommitted || !rolledBack.PreviousRetired || rolledBack.Previous.GenerationID != restored.GenerationID || rolledBack.GenerationID != original.GenerationID || rolledBack.Activation != activated.Activation+1 {
		t.Fatalf("unexpected rollback: %+v", rolledBack)
	}
	assertStoredMandate(t, root, mandate.ID)

	removed, err := GarbageCollect(ctx, root, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != restored.Directory {
		t.Fatalf("garbage collection removed=%v, want restored generation", removed)
	}
	if _, err = os.Stat(filepath.Join(root, "stores", original.Directory)); err != nil {
		t.Fatalf("garbage collection removed active generation: %v", err)
	}
}

func assertStoredMandate(t *testing.T, root, mandateID string) {
	t.Helper()
	store, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.GetMandate(context.Background(), mandateID); err != nil {
		_ = store.Close()
		t.Fatalf("restored authority record unavailable: %v", err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
}

func secureTestPath(t *testing.T, name string) string {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "secure")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(directory, name)
}

func TestMaintenanceDeniesOpenDirtyMalformedAndCanceledAuthority(t *testing.T) {
	ctx := context.Background()
	root := authorityRoot(t)
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	store, err := Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	blocked, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if _, err = Export(blocked, root, filepath.Join(t.TempDir(), "blocked.backup")); !errors.Is(err, context.DeadlineExceeded) {
		_ = store.Close()
		t.Fatalf("maintenance while open error=%v, want context deadline", err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}

	if err = os.WriteFile(filepath.Join(root, "CLEAN"), []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Export(ctx, root, filepath.Join(t.TempDir(), "malformed.backup")); !errors.Is(err, ErrMaintenanceUnsafe) {
		t.Fatalf("malformed closed state error=%v, want ErrMaintenanceUnsafe", err)
	}

	canceled, cancelNow := context.WithCancel(ctx)
	cancelNow()
	if _, err = GarbageCollect(canceled, root, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled maintenance error=%v, want context.Canceled", err)
	}
}

func TestMaintenanceRejectsCorruptBackupWithoutPublishingGeneration(t *testing.T) {
	ctx := context.Background()
	root := authorityRoot(t)
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	backup := secureTestPath(t, "authority.backup")
	if _, err := Export(ctx, root, backup); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	content[len(content)-1] ^= 0xff
	corrupt := filepath.Join(t.TempDir(), "corrupt.backup")
	if err = os.WriteFile(corrupt, content, 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadDir(filepath.Join(root, "stores"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Import(ctx, root, corrupt); !errors.Is(err, ErrBackupCorrupt) {
		t.Fatalf("corrupt import error=%v, want ErrBackupCorrupt", err)
	}
	after, err := os.ReadDir(filepath.Join(root, "stores"))
	if err != nil {
		t.Fatal(err)
	}
	staging, err := os.ReadDir(filepath.Join(root, "staging"))
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) || len(staging) != 0 {
		t.Fatalf("corrupt import published state: stores before=%d after=%d staging=%d", len(before), len(after), len(staging))
	}
}

func TestExportDoesNotReplaceExistingDestination(t *testing.T) {
	ctx := context.Background()
	root := authorityRoot(t)
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	destination := secureTestPath(t, "existing.backup")
	const sentinel = "operator-owned"
	if err := os.WriteFile(destination, []byte(sentinel), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Export(ctx, root, destination); err == nil {
		t.Fatal("existing backup destination was replaced")
	}
	content, err := os.ReadFile(destination)
	if err != nil || string(content) != sentinel {
		t.Fatalf("existing destination changed: %q err=%v", content, err)
	}
}

func TestGarbageCollectFailsClosedOnUnknownGeneration(t *testing.T) {
	ctx := context.Background()
	root := authorityRoot(t)
	active, err := Initialize(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	unknown := filepath.Join(root, "retired", "operator-data")
	if err = os.Mkdir(unknown, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = GarbageCollect(ctx, root, 0); !errors.Is(err, ErrMaintenanceUnsafe) {
		t.Fatalf("unknown generation error=%v, want ErrMaintenanceUnsafe", err)
	}
	for _, path := range []string{unknown, filepath.Join(root, "stores", active.Directory)} {
		if _, err = os.Stat(path); err != nil {
			t.Fatalf("fail-closed GC changed %s: %v", path, err)
		}
	}
}

func TestRecoveryKindsKeepLogicalImportSeparateFromExactNativeSelection(t *testing.T) {
	ctx := context.Background()
	root := authorityRoot(t)
	original, err := Initialize(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	backup := secureTestPath(t, "recovery.backup")
	if _, err = Export(ctx, root, backup); err != nil {
		t.Fatal(err)
	}

	logical, err := RecoverLogical(ctx, root, backup)
	if err != nil {
		t.Fatal(err)
	}
	if logical.Kind != RecoveryLogical || logical.Imported == nil || logical.Activation != nil {
		t.Fatalf("logical recovery returned ambiguous outcome: %+v", logical)
	}
	active, err := readMarker(filepath.Join(root, "ACTIVE"))
	if err != nil || active != original {
		t.Fatalf("logical recovery selected imported state: active=%+v err=%v", active, err)
	}

	native, err := RecoverNative(ctx, root, *logical.Imported)
	if err != nil {
		t.Fatal(err)
	}
	if native.Kind != RecoveryNative || native.Imported != nil || native.Activation == nil || !native.Activation.SelectionCommitted || native.Activation.Previous != original {
		t.Fatalf("native recovery returned ambiguous outcome: %+v", native)
	}

	tampered := original
	tampered.StoreID = "store-substituted"
	tampered.Digest = generationDigest(tampered)
	denied, err := RecoverNative(ctx, root, tampered)
	if err == nil || denied.Kind != RecoveryNative || denied.Activation == nil || denied.Activation.SelectionCommitted {
		t.Fatalf("native recovery did not deny an inexact generation: outcome=%+v err=%v", denied, err)
	}
}

func TestDiskReserveFailsClosedForUnmeasurableAndOverflowingRequirements(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	if err := CheckDiskReserve(missing, 0); !errors.Is(err, ErrDiskReserve) {
		t.Fatalf("unmeasurable reserve error=%v, want ErrDiskReserve", err)
	}
	if err := CheckDiskReserve(t.TempDir(), ^uint64(0)); !errors.Is(err, ErrDiskReserve) {
		t.Fatalf("overflowing reserve error=%v, want ErrDiskReserve", err)
	}
}

func TestLogicalRecoveryDiscardsBackupProjectionAndRebuildsFromCanonicalState(t *testing.T) {
	ctx := context.Background()
	root := authorityRoot(t)
	original, err := Initialize(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	mandate, authority := badgerAuthorityBinding("mandate-logical", "authority-logical", "session-logical")
	if err = store.CreateMandate(ctx, mandate); err != nil {
		t.Fatal(err)
	}
	if err = store.CreateAuthorityContext(ctx, authority); err != nil {
		t.Fatal(err)
	}
	recordedAt := authority.IssuedAt.Add(10 * time.Second)
	command := badgerAuthorityCommand("command-logical", core.AuthorityCommandActivate, authority, 1, "", recordedAt.Add(-time.Second))
	if _, err = store.ProcessAuthorityCommand(ctx, command, recordedAt, "controller"); err != nil {
		t.Fatal(err)
	}
	projectionKey, _ := encodeKey(KeyAuthorityProjection, []string{authority.ID}, 0)
	if err = store.db.Update(func(txn *badgerdb.Txn) error { return txn.Set(projectionKey, []byte("{}")) }); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}

	backup := secureTestPath(t, "logical-projection.backup")
	if _, err = Export(ctx, root, backup); err != nil {
		t.Fatal(err)
	}
	recovery, err := RecoverLogical(ctx, root, backup)
	if err != nil {
		t.Fatal(err)
	}
	if recovery.Imported == nil {
		t.Fatal("logical recovery did not return imported generation")
	}
	if active, err := readMarker(filepath.Join(root, "ACTIVE")); err != nil || active != original {
		t.Fatalf("logical recovery selected candidate: active=%+v err=%v", active, err)
	}
	if _, err = RecoverNative(ctx, root, *recovery.Imported); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	projection, err := restored.CurrentAuthorityProjection(ctx, authority.ID)
	if err != nil || projection.State != core.AuthorityStateActive || projection.AuthorityContextID != authority.ID {
		t.Fatalf("logical recovery did not rebuild canonical projection: projection=%+v err=%v", projection, err)
	}
}
