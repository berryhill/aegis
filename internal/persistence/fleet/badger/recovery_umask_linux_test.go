//go:build linux

package badger

import (
	"context"
	physical "github.com/berryhill/aegis/internal/persistence/recoveryphysical"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestFleetRecoveryPrivateUmask(t *testing.T) {
	t.Setenv("AEGIS_TEST_RECOVERY_UMASK", "0002")
	root := crashFleet(t, false)
	selected := filepath.Join(root, "store.badger")
	entries, e := os.ReadDir(selected)
	if e != nil {
		t.Fatal(e)
	}
	original := map[string]os.FileInfo{}
	permissive := false
	for _, entry := range entries {
		info, e := entry.Info()
		if e != nil {
			t.Fatal(e)
		}
		original[entry.Name()] = info
		if info.Mode().Perm() == 0664 {
			permissive = true
			t.Logf("actual engine mode: %s %04o", entry.Name(), info.Mode().Perm())
		}
	}
	if !permissive {
		t.Fatal("no actual permissive engine file")
	}
	before, e := physical.OriginalDirectory(context.Background(), selected, "")
	if e != nil {
		t.Fatal(e)
	}
	if s, e := Open(context.Background(), root); e == nil {
		s.Close()
		t.Fatal("dirty live open accepted")
	}
	r, e := RecoverInterruptedAs(context.Background(), root, "owner")
	if e != nil || !r.Recovered {
		t.Fatalf("recovery: %+v %v", r, e)
	}
	retained := filepath.Join(r.EvidencePath, "work")
	after, e := physical.OriginalDirectory(context.Background(), retained, "")
	if e != nil || after != before {
		t.Fatalf("original bytes changed: %v", e)
	}
	for name, info := range original {
		now, e := os.Lstat(filepath.Join(retained, name))
		if e != nil || !os.SameFile(info, now) || now.Mode() != info.Mode() || !physical.SameChangeTime(info, now) {
			t.Fatalf("original custody changed: %s %v", name, e)
		}
	}
	// Model a crash after exchange but before completion, using the real receipt.
	receipt, e := readFleetRecovery(root)
	if e != nil {
		t.Fatal(e)
	}
	receipt.Phase = "verified"
	if e = saveFleetRecovery(root, receipt); e != nil {
		t.Fatal(e)
	}
	if e = os.Rename(filepath.Join(root, "CLEAN"), filepath.Join(root, "DIRTY")); e != nil {
		t.Fatal(e)
	}
	if _, e = RecoverInterruptedAs(context.Background(), root, "wrong-owner"); e == nil {
		t.Fatal("wrong owner resumed")
	}
	again, e := RecoverInterruptedAs(context.Background(), root, "owner")
	if e != nil || !again.Recovered || again.EvidencePath != r.EvidencePath {
		t.Fatalf("exchange resume: %+v %v", again, e)
	}
	s, e := Open(context.Background(), root)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestFleetRecoveryCrashChild$")
	child.Env = append(os.Environ(), "AEGIS_FLEET_RECOVERY_CHILD="+root)
	if b, err := child.CombinedOutput(); err != nil {
		t.Fatalf("second crash: %v %s", err, b)
	}
	next, err := RecoverInterruptedAs(context.Background(), root, "owner")
	if err != nil || !next.Recovered || next.EvidencePath == r.EvidencePath {
		t.Fatalf("new interrupted generation: %+v %v", next, err)
	}
	if sum, err := physical.OriginalDirectory(context.Background(), retained, ""); err != nil || sum != before {
		t.Fatalf("old original history changed: %v", err)
	}
}
