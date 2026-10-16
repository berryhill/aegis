package badger

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestInterruptedRecoveryRejectsUnsafePersistenceParent(t *testing.T) {
	for _, kind := range []string{"symlink", "writable"} {
		t.Run(kind, func(t *testing.T) {
			root, _ := interruptedFixture(t)
			parent := filepath.Dir(root)
			dirty, err := os.ReadFile(filepath.Join(root, "DIRTY"))
			if err != nil {
				t.Fatal(err)
			}
			if kind == "symlink" {
				moved := parent + "-preserved"
				if err = os.Rename(parent, moved); err != nil {
					t.Fatal(err)
				}
				if err = os.Symlink(moved, parent); err != nil {
					t.Fatal(err)
				}
			} else if err = os.Chmod(parent, 0770); err != nil {
				t.Fatal(err)
			}
			result, err := RecoverInterruptedAs(context.Background(), root, "principal")
			if err == nil || result.Recovered {
				t.Fatalf("unsafe parent admitted: %+v %v", result, err)
			}
			after, err := os.ReadFile(filepath.Join(root, "DIRTY"))
			if err != nil || !bytes.Equal(dirty, after) {
				t.Fatal("denial changed DIRTY", err)
			}
			if _, err = os.Lstat(filepath.Join(root, "recovery-evidence")); !os.IsNotExist(err) {
				t.Fatal("denial created evidence", err)
			}
		})
	}
}
