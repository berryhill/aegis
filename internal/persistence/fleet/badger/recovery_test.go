package badger

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	badgerdb "github.com/dgraph-io/badger/v4"
)

func TestFleetRecoveryCrashChild(t *testing.T) {
	root := os.Getenv("AEGIS_FLEET_RECOVERY_CHILD")
	if root == "" {
		return
	}
	syscall.Umask(0077)
	s, e := Open(context.Background(), root)
	if e != nil {
		t.Fatal(e)
	}
	if os.Getenv("AEGIS_FLEET_RECOVERY_UNKNOWN") == "1" {
		e = s.db.Update(func(tx *badgerdb.Txn) error { return tx.Set([]byte{2, 0xfe, 0, 1, 'x'}, []byte("unknown")) })
		if e != nil {
			t.Fatal(e)
		}
	}
	if e = s.db.Sync(); e != nil {
		t.Fatal(e)
	}
	os.Exit(0)
}
func crashFleet(t *testing.T, unknown bool) string {
	t.Helper()
	parent := t.TempDir()
	if e := os.Chmod(parent, 0700); e != nil {
		t.Fatal(e)
	}
	root := filepath.Join(parent, "fleet-v1")
	c := exec.Command(os.Args[0], "-test.run=^TestFleetRecoveryCrashChild$")
	c.Env = append(os.Environ(), "AEGIS_FLEET_RECOVERY_CHILD="+root)
	if unknown {
		c.Env = append(c.Env, "AEGIS_FLEET_RECOVERY_UNKNOWN=1")
	}
	if b, e := c.CombinedOutput(); e != nil {
		t.Fatalf("child %v: %s", e, b)
	}
	return root
}
func TestFleetInterruptedRecovery(t *testing.T) {
	ctx := context.Background()
	t.Run("positive", func(t *testing.T) {
		root := crashFleet(t, false)
		operational := filepath.Join(root, "contribution-evidence")
		if e := os.Mkdir(operational, 0700); e != nil {
			t.Fatal(e)
		}
		sentinel := filepath.Join(operational, "retained-proof")
		if e := os.WriteFile(sentinel, []byte("retained operational evidence"), 0600); e != nil {
			t.Fatal(e)
		}
		if s, e := Open(ctx, root); e == nil {
			s.Close()
			t.Fatal("ordinary dirty Open admitted")
		}
		r, e := RecoverInterruptedAs(ctx, root, "fixture-owner")
		if e != nil {
			t.Fatal(e)
		}
		if !r.Recovered || r.Records != 1 {
			t.Fatalf("result %+v", r)
		}
		if b, e := os.ReadFile(sentinel); e != nil || string(b) != "retained operational evidence" {
			t.Fatalf("operational evidence changed: %v", e)
		}
		if _, e = os.Stat(filepath.Join(r.EvidencePath, "DIRTY")); e != nil {
			t.Fatal(e)
		}
		again, e := RecoverInterruptedAs(ctx, root, "fixture-owner")
		if e != nil || again.EvidencePath != r.EvidencePath {
			t.Fatalf("retry %+v %v", again, e)
		}
		s, e := Open(ctx, root)
		if e != nil {
			t.Fatal(e)
		}
		if e = s.Close(); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("unknown", func(t *testing.T) {
		root := crashFleet(t, true)
		if _, e := RecoverInterruptedAs(ctx, root, "fixture-owner"); e == nil {
			t.Fatal("unknown family admitted")
		}
		if _, e := os.Stat(filepath.Join(root, "DIRTY")); e != nil {
			t.Fatal(e)
		}
		if _, e := os.Stat(filepath.Join(root, "CLEAN")); !os.IsNotExist(e) {
			t.Fatal("clean published")
		}
	})
	t.Run("corrupt", func(t *testing.T) {
		root := crashFleet(t, false)
		entries, e := os.ReadDir(filepath.Join(root, "store.badger"))
		if e != nil {
			t.Fatal(e)
		}
		for _, entry := range entries {
			if filepath.Ext(entry.Name()) == ".mem" {
				f, e := os.OpenFile(filepath.Join(root, "store.badger", entry.Name()), os.O_WRONLY, 0)
				if e != nil {
					t.Fatal(e)
				}
				_, e = f.WriteAt([]byte{0xff}, 20)
				f.Close()
				if e != nil {
					t.Fatal(e)
				}
				break
			}
		}
		if _, e = RecoverInterruptedAs(ctx, root, "fixture-owner"); e == nil {
			t.Fatal("corruption admitted")
		}
	})
	t.Run("operational-evidence-symlink", func(t *testing.T) {
		root := crashFleet(t, false)
		if e := os.Symlink(t.TempDir(), filepath.Join(root, "contribution-evidence")); e != nil {
			t.Fatal(e)
		}
		if _, e := RecoverInterruptedAs(ctx, root, "fixture-owner"); e == nil {
			t.Fatal("operational evidence symlink admitted")
		}
	})
	t.Run("live-writer", func(t *testing.T) {
		p := t.TempDir()
		os.Chmod(p, 0700)
		root := filepath.Join(p, "fleet-v1")
		s, e := Open(ctx, root)
		if e != nil {
			t.Fatal(e)
		}
		defer s.Close()
		if _, e = RecoverInterruptedAs(ctx, root, "fixture-owner"); e == nil {
			t.Fatal("live writer admitted")
		}
	})
}
