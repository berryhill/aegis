//go:build linux

package badger

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	physical "github.com/berryhill/aegis/internal/persistence/recoveryphysical"
)

func TestInterruptedRecoveryPrivateUmask(t *testing.T) {
	for _, stage := range []string{"before-receipt", "before-exchange", "exchange", "clean"} {
		t.Run(stage, func(t *testing.T) {
			root, g := interruptedFixture(t)
			if err := os.Rename(filepath.Join(root, "DIRTY"), filepath.Join(root, "CLEAN")); err != nil {
				t.Fatal(err)
			}
			child := exec.Command(os.Args[0], "-test.run=^TestInterruptedRecoveryProcessExit$")
			child.Env = append(os.Environ(), "AEGIS_TEST_INTERRUPTED_ROOT="+root, "AEGIS_TEST_RECOVERY_UMASK=0002")
			if b, e := child.CombinedOutput(); e != nil {
				t.Fatalf("child: %v %s", e, b)
			}
			selected := filepath.Join(root, "stores", g.Directory)
			entries, e := os.ReadDir(selected)
			if e != nil {
				t.Fatal(e)
			}
			original := map[string]os.FileInfo{}
			kinds := map[string]bool{}
			for _, entry := range entries {
				info, e := entry.Info()
				if e != nil {
					t.Fatal(e)
				}
				original[entry.Name()] = info
				if info.Mode().Perm() == 0664 {
					kinds[filepath.Ext(entry.Name())] = true
					t.Logf("actual engine mode: %s %04o", entry.Name(), info.Mode().Perm())
				}
			}
			if !kinds[""] || !kinds[".mem"] || !kinds[".vlog"] {
				t.Fatalf("missing actual permissive engine files: %v", kinds)
			}
			before, e := physical.OriginalDirectory(context.Background(), selected, "")
			if e != nil {
				t.Fatal(e)
			}
			if opened, e := Open(context.Background(), root); e == nil {
				opened.Close()
				t.Fatal("dirty live open accepted")
			}
			hit := false
			ctx := context.WithValue(context.Background(), recoveryFaultKey{}, func(s string) error {
				if s == stage {
					hit = true
					return errors.New("fixture interruption")
				}
				return nil
			})
			r, e := RecoverInterruptedAs(ctx, root, "owner")
			if e == nil || !hit || r.Recovered {
				t.Fatalf("expected stage %s reached: %v %v %+v", stage, hit, e, r)
			}
			if stage != "before-receipt" {
				if _, e := RecoverInterruptedAs(context.Background(), root, "wrong-owner"); e == nil {
					t.Fatal("wrong owner resumed")
				}
			}
			r, e = RecoverInterruptedAs(context.Background(), root, "owner")
			if e != nil || !r.Recovered {
				t.Fatalf("resume: %+v %v", r, e)
			}
			retained := filepath.Join(r.EvidencePath, "work")
			after, e := physical.OriginalDirectory(context.Background(), retained, "")
			if e != nil || before != after {
				t.Fatalf("original bytes changed: %v", e)
			}
			for name, info := range original {
				now, e := os.Lstat(filepath.Join(retained, name))
				if e != nil || !os.SameFile(info, now) || info.Mode() != now.Mode() || !physical.SameChangeTime(info, now) {
					t.Fatalf("original identity/mode changed: %s %v", name, e)
				}
			}
			expected, e := os.ReadFile(filepath.Join(filepath.Dir(root), "expected.json"))
			if e != nil {
				t.Fatal(e)
			}
			store, e := Open(context.Background(), root)
			if e != nil {
				t.Fatal(e)
			}
			if !bytes.Equal(expected, canonicalBytes(t, store.db)) {
				t.Fatal("canonical bytes changed")
			}
			if e = store.Close(); e != nil {
				t.Fatal(e)
			}
		})
	}
}
