package badger

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	badgerdb "github.com/dgraph-io/badger/v4"
)

func TestCombinedRecoveryVerifiesClosedAuthority(t *testing.T) {
	ctx := context.Background()
	root := authorityRoot(t)
	g, err := Initialize(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(root, "stores", g.Directory)
	before, err := recoveryDirectory(ctx, selected, "")
	if err != nil {
		t.Fatal(err)
	}
	clean, err := os.ReadFile(filepath.Join(root, "CLEAN"))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		out, err := RecoverInterruptedOrVerifyClosedAs(ctx, root, "owner")
		if err != nil || !out.VerifiedClean || out.Recovered || out.EvidencePath != "" || out.Generation != g || out.Records != 3 {
			t.Fatalf("%+v %v", out, err)
		}
	}
	after, err := recoveryDirectory(ctx, selected, "")
	if err != nil || before != after {
		t.Fatalf("engine changed: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "CLEAN"))
	if err != nil || !bytes.Equal(clean, got) {
		t.Fatal("clean changed", err)
	}
	if _, err = os.Lstat(filepath.Join(root, "recovery-evidence")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("created evidence", err)
	}
	if _, err = RecoverInterruptedAs(ctx, root, "owner"); err == nil {
		t.Fatal("authority-only accepted clean close")
	}
}

func TestCombinedRecoveryClosedAuthorityDenials(t *testing.T) {
	for _, scenario := range []string{"dirty", "missing-clean", "mismatch", "staging", "retired", "extra-store", "unknown-root", "symlink", "unsafe-engine", "unknown-record", "evidence-without-receipt", "corrupt-engine", "live-writer", "cancelled", "missing-principal"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			root := authorityRoot(t)
			g, err := Initialize(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			must := func(e error) {
				t.Helper()
				if e != nil {
					t.Fatal(e)
				}
			}
			principal := "owner"
			switch scenario {
			case "dirty":
				must(writeMarker(root, "DIRTY", g))
			case "missing-clean":
				must(os.Remove(filepath.Join(root, "CLEAN")))
			case "mismatch":
				other := g
				other.Activation++
				other.Digest = generationDigest(other)
				must(writeMarker(root, "CLEAN", other))
			case "staging", "retired":
				must(os.Mkdir(filepath.Join(root, scenario, "unexpected"), 0700))
			case "extra-store":
				must(os.Mkdir(filepath.Join(root, "stores", "unexpected"), 0700))
			case "unknown-root":
				must(os.WriteFile(filepath.Join(root, "unknown"), nil, 0600))
			case "symlink":
				must(os.Remove(filepath.Join(root, "CLEAN")))
				must(os.Symlink(filepath.Join(root, "ACTIVE"), filepath.Join(root, "CLEAN")))
			case "unsafe-engine":
				must(os.Chmod(filepath.Join(root, "stores", g.Directory, "MANIFEST"), 0666))
			case "unknown-record":
				s, e := Open(ctx, root)
				must(e)
				must(s.db.Update(func(tx *badgerdb.Txn) error { return tx.Set([]byte("unknown"), []byte("untrusted")) }))
				must(s.Close())
			case "evidence-without-receipt":
				must(os.Mkdir(filepath.Join(root, "recovery-evidence"), 0700))
			case "corrupt-engine":
				must(os.WriteFile(filepath.Join(root, "stores", g.Directory, "MANIFEST"), []byte("corrupt"), 0600))
			case "live-writer":
				s, e := Open(ctx, root)
				must(e)
				defer s.Close()
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 30*time.Millisecond)
				defer cancel()
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "missing-principal":
				principal = ""
			}
			active, e := os.ReadFile(filepath.Join(root, "ACTIVE"))
			must(e)
			out, err := RecoverInterruptedOrVerifyClosedAs(ctx, root, principal)
			if err == nil || out.VerifiedClean || out.Recovered {
				t.Fatalf("admitted: %+v %v", out, err)
			}
			if scenario == "live-writer" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
			got, e := os.ReadFile(filepath.Join(root, "ACTIVE"))
			must(e)
			if !bytes.Equal(active, got) {
				t.Fatal("denial changed ACTIVE")
			}
		})
	}
}

func TestCombinedRecoveryPreservesCompletedHistory(t *testing.T) {
	for _, scenario := range []string{"exact", "later-clean-close", "wrong-principal", "changed-evidence", "missing-history", "pending"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			root, _ := interruptedFixture(t)
			if scenario == "pending" {
				ctx = context.WithValue(ctx, recoveryFaultKey{}, func(stage string) error {
					if stage == "dirty-removed" {
						return errors.New("fixture interrupted publication")
					}
					return nil
				})
			}
			prior, err := RecoverInterruptedAs(ctx, root, "owner")
			if scenario != "pending" && err != nil {
				t.Fatal(err)
			}
			if scenario == "pending" && err == nil {
				t.Fatal("fault did not fire")
			}
			ctx = context.Background()
			must := func(e error) {
				t.Helper()
				if e != nil {
					t.Fatal(e)
				}
			}
			if scenario == "later-clean-close" || scenario == "changed-evidence" {
				s, e := Open(ctx, root)
				must(e)
				must(s.Close())
			}
			principal := "owner"
			switch scenario {
			case "wrong-principal":
				principal = "other"
			case "changed-evidence":
				must(os.WriteFile(filepath.Join(prior.EvidencePath, "store", "MANIFEST"), []byte("corrupt"), 0600))
			case "missing-history":
				must(os.Remove(filepath.Join(prior.EvidencePath, "completed.json")))
			}
			path := filepath.Join(root, "recovery-evidence", "receipt.json")
			before, e := os.ReadFile(path)
			must(e)
			out, err := RecoverInterruptedOrVerifyClosedAs(ctx, root, principal)
			switch scenario {
			case "wrong-principal", "changed-evidence", "missing-history":
				if err == nil || out.VerifiedClean || out.Recovered {
					t.Fatal("invalid history accepted")
				}
			case "exact":
				if err != nil || !reflect.DeepEqual(prior, out) {
					t.Fatalf("prior receipt not replayed: %+v %v", out, err)
				}
			case "later-clean-close":
				if err != nil || !out.VerifiedClean || out.Recovered || out.EvidencePath != prior.EvidencePath {
					t.Fatalf("%+v %v", out, err)
				}
			case "pending":
				if err != nil || !out.Recovered || out.VerifiedClean {
					t.Fatalf("pending receipt bypassed: %+v %v", out, err)
				}
			}
			after, e := os.ReadFile(path)
			must(e)
			if scenario != "pending" && !bytes.Equal(before, after) {
				t.Fatal("completed receipt changed")
			}
		})
	}
}
