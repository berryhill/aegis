package badger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	badgerdb "github.com/dgraph-io/badger/v4"
)

func canonicalBytes(t *testing.T, db *badgerdb.DB) []byte {
	t.Helper()
	records := map[string][]byte{}
	if err := db.View(func(txn *badgerdb.Txn) error {
		it := txn.NewIterator(badgerdb.DefaultIteratorOptions)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			v, e := it.Item().ValueCopy(nil)
			if e != nil {
				return e
			}
			records[string(it.Item().KeyCopy(nil))] = v
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	b, e := json.Marshal(records)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func interruptedFixture(t *testing.T) (string, Generation) {
	t.Helper()
	root := authorityRoot(t)
	g, err := Initialize(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(filepath.Join(root, "CLEAN"), filepath.Join(root, "DIRTY")); err != nil {
		t.Fatal(err)
	}
	return root, g
}

func TestInterruptedRecoveryPreservesExactEvidenceAndGeneration(t *testing.T) {
	root, g := interruptedFixture(t)
	dirty, _ := os.ReadFile(filepath.Join(root, "DIRTY"))
	if _, err := Open(context.Background(), root); err == nil {
		t.Fatal("direct Open bypassed dirty boundary")
	}
	result, err := RecoverInterrupted(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Recovered || result.Generation != g || result.Records != 3 {
		t.Fatalf("result: %+v", result)
	}
	saved, err := os.ReadFile(filepath.Join(result.EvidencePath, "DIRTY"))
	if err != nil || !bytes.Equal(saved, dirty) {
		t.Fatal("evidence changed", err)
	}
	if _, err = os.Stat(filepath.Join(result.EvidencePath, "SHA256.json")); err != nil {
		t.Fatal(err)
	}
	store, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if store.Generation() != g {
		t.Fatal("generation changed")
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = RecoverInterrupted(context.Background(), root); err == nil {
		t.Fatal("clean state accepted")
	}
}

func TestInterruptedRecoveryRefusesAmbiguity(t *testing.T) {
	for _, kind := range []string{"clean", "missing-dirty", "mismatch", "staging", "retired", "extra-store", "unknown-root", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			root, g := interruptedFixture(t)
			switch kind {
			case "clean":
				if err := writeMarker(root, "CLEAN", g); err != nil {
					t.Fatal(err)
				}
			case "missing-dirty":
				os.Remove(filepath.Join(root, "DIRTY"))
			case "mismatch":
				g.Activation++
				g.Digest = generationDigest(g)
				writeMarker(root, "DIRTY", g)
			case "staging", "retired":
				os.Mkdir(filepath.Join(root, kind, "unexpected"), 0700)
			case "extra-store":
				os.Mkdir(filepath.Join(root, "stores", "unexpected"), 0700)
			case "unknown-root":
				os.WriteFile(filepath.Join(root, ".CLEAN-interrupted"), []byte("evidence"), 0600)
			case "symlink":
				os.Remove(filepath.Join(root, "DIRTY"))
				os.Symlink(filepath.Join(root, "ACTIVE"), filepath.Join(root, "DIRTY"))
			}
			before, _ := os.ReadFile(filepath.Join(root, "ACTIVE"))
			result, err := RecoverInterrupted(context.Background(), root)
			if err == nil || result.Recovered {
				t.Fatal("ambiguous state accepted")
			}
			after, _ := os.ReadFile(filepath.Join(root, "ACTIVE"))
			if !bytes.Equal(before, after) {
				t.Fatal("ACTIVE changed")
			}
		})
	}
}

func TestInterruptedRecoveryRefusesOpenWriter(t *testing.T) {
	root, g := interruptedFixture(t)
	os.Rename(filepath.Join(root, "DIRTY"), filepath.Join(root, "CLEAN"))
	store, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = RecoverInterrupted(ctx, root)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if store.Generation() != g {
		t.Fatal("writer changed")
	}
}

func TestInterruptedRecoveryValidatesAllCanonicalFamilies(t *testing.T) {
	for _, family := range []KeyFamily{KeyMandate, KeyAuthorityContext, KeyContextBySession, KeyTransitionFact, KeyTransitionRoot, KeyAuthorityCommand, KeyAuthorityFact, KeyAuthorityReceipt, KeyAuthorityProjection, KeyAuthorityOutbox, KeyAuthorityAudit, 0xff} {
		t.Run(string(rune(family)), func(t *testing.T) {
			root, g := interruptedFixture(t)
			db, err := badgerdb.Open(options(filepath.Join(root, "stores", g.Directory)))
			if err != nil {
				t.Fatal(err)
			}
			key := []byte{1, 0xff}
			if family != 0xff {
				seq := uint64(0)
				if keyRegistry[family].sequence {
					seq = 1
				}
				key, _ = encodeKey(family, []string{"orphan"}, seq)
			}
			if err = db.Update(func(txn *badgerdb.Txn) error { return txn.Set(key, []byte("invalid")) }); err != nil {
				t.Fatal(err)
			}
			if err = db.Close(); err != nil {
				t.Fatal(err)
			}
			result, err := RecoverInterrupted(context.Background(), root)
			if err == nil || result.Recovered || result.EvidencePath == "" {
				t.Fatalf("accepted corruption: %+v %v", result, err)
			}
			if _, err = os.Lstat(filepath.Join(root, "CLEAN")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("published CLEAN")
			}
			if _, err = os.Stat(filepath.Join(root, "DIRTY")); err != nil {
				t.Fatal("lost DIRTY")
			}
		})
	}
}

func TestInterruptedRecoveryPreservesRevocationAndAudit(t *testing.T) {
	store, authority := openCommandStore(t)
	at := authority.IssuedAt.Add(10 * time.Second)
	activate := badgerAuthorityCommand("recover-activate", core.AuthorityCommandActivate, authority, 1, "", at.Add(-time.Second))
	receipt, err := store.ProcessAuthorityCommand(context.Background(), activate, at, "controller")
	if err != nil {
		t.Fatal(err)
	}
	revoke := badgerAuthorityCommand("recover-revoke", core.AuthorityCommandRevoke, authority, 2, receipt.FactDigest, at.Add(time.Second))
	if _, err = store.ProcessAuthorityCommand(context.Background(), revoke, at.Add(2*time.Second), "controller"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DeliverAuthorityAudit(context.Background(), authority.ID, 100); err != nil {
		t.Fatal(err)
	}
	root := store.root
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	os.Rename(filepath.Join(root, "CLEAN"), filepath.Join(root, "DIRTY"))
	if _, err = RecoverInterrupted(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	projection, err := reopened.CurrentAuthorityProjection(context.Background(), authority.ID)
	if err != nil || projection.State != core.AuthorityStateRevoked {
		t.Fatal("revocation lost", err)
	}
}

func TestInterruptedRecoveryProcessExit(t *testing.T) {
	if root := os.Getenv("AEGIS_TEST_INTERRUPTED_ROOT"); root != "" {
		syscall.Umask(0077)
		store, err := Open(context.Background(), root)
		if err != nil {
			os.Exit(3)
		}
		mandate, authority := badgerAuthorityBinding("mandate-command", "authority-command", "session-command")
		if err = store.CreateMandate(context.Background(), mandate); err != nil {
			t.Fatal(err)
		}
		if err = store.CreateAuthorityContext(context.Background(), authority); err != nil {
			t.Fatal(err)
		}
		at := authority.IssuedAt.Add(10 * time.Second)
		activate := badgerAuthorityCommand("crash-activate", core.AuthorityCommandActivate, authority, 1, "", at.Add(-time.Second))
		receipt, e := store.ProcessAuthorityCommand(context.Background(), activate, at, "controller")
		if e != nil {
			t.Fatal(e)
		}
		revoke := badgerAuthorityCommand("crash-revoke", core.AuthorityCommandRevoke, authority, 2, receipt.FactDigest, at.Add(time.Second))
		if _, e = store.ProcessAuthorityCommand(context.Background(), revoke, at.Add(2*time.Second), "controller"); e != nil {
			t.Fatal(e)
		}
		if _, e = store.DeliverAuthorityAudit(context.Background(), authority.ID, 100); e != nil {
			t.Fatal(e)
		}
		expected := canonicalBytes(t, store.db)
		if e = os.WriteFile(filepath.Join(filepath.Dir(root), "expected.json"), expected, 0600); e != nil {
			t.Fatal(e)
		}
		if err = store.db.Sync(); err != nil {
			os.Exit(4)
		}
		os.Exit(0)
	}
	root, _ := interruptedFixture(t)
	os.Rename(filepath.Join(root, "DIRTY"), filepath.Join(root, "CLEAN"))
	child := exec.Command(os.Args[0], "-test.run=^TestInterruptedRecoveryProcessExit$")
	child.Env = append(os.Environ(), "AEGIS_TEST_INTERRUPTED_ROOT="+root)
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatalf("%s %v", output, err)
	}
	result, err := RecoverInterrupted(context.Background(), root)
	if err != nil || !result.Recovered {
		t.Fatalf("crash recovery failed: %+v %v", result, err)
	}
	expected, e := os.ReadFile(filepath.Join(filepath.Dir(root), "expected.json"))
	if e != nil {
		t.Fatal(e)
	}
	reopened, e := Open(context.Background(), root)
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(expected, canonicalBytes(t, reopened.db)) {
		t.Fatal("canonical records changed")
	}
	projection, e := reopened.CurrentAuthorityProjection(context.Background(), "authority-command")
	if e != nil || projection.State != core.AuthorityStateRevoked {
		t.Fatal("revocation lost", e)
	}
	if e = reopened.Close(); e != nil {
		t.Fatal(e)
	}
}

func TestClosePreservesUnexpectedLifecycleEvidence(t *testing.T) {
	for _, marker := range []string{"CLEAN", "DIRTY", "ACTIVE"} {
		t.Run(marker, func(t *testing.T) {
			root := authorityRoot(t)
			generation, err := Initialize(context.Background(), root)
			if err != nil {
				t.Fatal(err)
			}
			store, err := Open(context.Background(), root)
			if err != nil {
				t.Fatal(err)
			}
			generation.Activation++
			generation.Digest = generationDigest(generation)
			if err = writeMarker(root, marker, generation); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(filepath.Join(root, marker))
			if err = store.Close(); err == nil {
				t.Fatal("close replaced unexpected evidence")
			}
			after, _ := os.ReadFile(filepath.Join(root, marker))
			if !bytes.Equal(before, after) {
				t.Fatal("evidence changed")
			}
			if _, err = os.Stat(filepath.Join(root, "DIRTY")); err != nil {
				t.Fatal("DIRTY lost")
			}
		})
	}
}
