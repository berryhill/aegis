package badger

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
)

func fleetSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out[p] = info.Mode().String()
		if info.Mode().IsRegular() {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			out[p] += string(b)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func TestFleetRecoveryCompatibility(t *testing.T) {
	ctx := context.Background()
	t.Run("absent", func(t *testing.T) {
		p := t.TempDir()
		if err := os.Chmod(p, 0700); err != nil {
			t.Fatal(err)
		}
		root := filepath.Join(p, "fleet-v1")
		r, err := RecoverInterruptedAs(ctx, root, "fixture-owner")
		if err != nil || r.Recovered {
			t.Fatalf("%+v %v", r, err)
		}
		if _, err = os.Lstat(root); !os.IsNotExist(err) {
			t.Fatalf("absent fleet created: %v", err)
		}
		if err = os.Chmod(p, 0770); err != nil {
			t.Fatal(err)
		}
		if _, err = RecoverInterruptedAs(ctx, root, "fixture-owner"); err == nil {
			t.Fatal("unsafe absent parent admitted")
		}
	})
	for _, name := range []string{"clean", "live-writer", "malformed", "conflict", "unknown", "symlink", "hardlink", "unsafe", "pending", "orphan-evidence", "ancestor-symlink", "root-symlink", "engine-hardlink", "verified-pending"} {
		t.Run(name, func(t *testing.T) {
			p := t.TempDir()
			if err := os.Chmod(p, 0700); err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(p, "fleet-v1")
			s, err := Open(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			switch name {
			case "live-writer":
				f, err := os.OpenFile(filepath.Join(root, "WRITER.lock"), os.O_RDWR, 0)
				must(err)
				defer f.Close()
				must(syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
				defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
			case "malformed":
				must(os.WriteFile(filepath.Join(root, "CLEAN"), []byte("wrong\n"), 0600))
			case "conflict":
				must(os.WriteFile(filepath.Join(root, "DIRTY"), []byte(schemaVersion+"\n"), 0600))
			case "unknown":
				must(os.WriteFile(filepath.Join(root, "foreign"), nil, 0600))
			case "symlink":
				must(os.Remove(filepath.Join(root, "CLEAN")))
				must(os.Symlink("WRITER.lock", filepath.Join(root, "CLEAN")))
			case "hardlink":
				must(os.Link(filepath.Join(root, "CLEAN"), filepath.Join(p, "alias")))
			case "unsafe":
				must(os.Chmod(root, 0750))
			case "root-symlink":
				moved := filepath.Join(p, "preserved-fleet")
				must(os.Rename(root, moved))
				must(os.Symlink(moved, root))
			case "engine-hardlink":
				entries, err := os.ReadDir(filepath.Join(root, "store.badger"))
				must(err)
				must(os.Link(filepath.Join(root, "store.badger", entries[0].Name()), filepath.Join(p, "engine-alias")))
			case "pending", "verified-pending":
				must(os.Mkdir(filepath.Join(root, "recovery-evidence"), 0700))
				phase := "preserved"
				if name == "verified-pending" {
					phase = "verified"
				}
				must(saveFleetRecovery(root, fleetRecoveryReceipt{Principal: "fixture-owner", Evidence: filepath.Join(root, "recovery-evidence", "interrupted-test"), Phase: phase}))
			case "orphan-evidence":
				must(os.Mkdir(filepath.Join(root, "recovery-evidence"), 0700))
			case "ancestor-symlink":
				moved := p + "-moved"
				must(os.Rename(p, moved))
				defer os.RemoveAll(moved)
				must(os.Symlink(moved, p))
			}
			before := fleetSnapshot(t, root)
			r, err := RecoverInterruptedAs(ctx, root, "fixture-owner")
			if name == "clean" {
				if err != nil || r.Recovered {
					t.Fatalf("clean %+v %v", r, err)
				}
			} else if err == nil {
				t.Fatal("unsafe clean accepted")
			}
			if after := fleetSnapshot(t, root); !reflect.DeepEqual(before, after) {
				t.Fatal("clean/denied state changed")
			}
		})
	}
}
func TestFleetRecoveryVerifiedCleanReceiptResume(t *testing.T) {
	root := crashFleet(t, false)
	ctx := context.Background()
	first, err := RecoverInterruptedAs(ctx, root, "fixture-owner")
	if err != nil {
		t.Fatal(err)
	}
	r, err := readFleetRecovery(root)
	if err != nil {
		t.Fatal(err)
	}
	r.Phase = "verified"
	if err = saveFleetRecovery(root, r); err != nil {
		t.Fatal(err)
	}
	if s, err := Open(ctx, root); err == nil {
		s.Close()
		t.Fatal("pending receipt admitted by normal Open")
	}
	next, err := RecoverInterruptedAs(ctx, root, "fixture-owner")
	if err != nil || !next.Recovered || next.EvidencePath != first.EvidencePath {
		t.Fatalf("%+v %v", next, err)
	}
	r, err = readFleetRecovery(root)
	if err != nil || r.Phase != "completed" {
		t.Fatalf("%+v %v", r, err)
	}
}
