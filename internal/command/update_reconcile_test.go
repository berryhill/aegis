package command

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	selfupdate "github.com/berryhill/aegis/internal/update"
	"github.com/berryhill/aegis/internal/userservice"
)

func TestUpgradeJournalReadOnlyPendingAndUnsafe(t *testing.T) {
	state := t.TempDir()
	plan := userservice.Plan{Executable: "/exact/aegis", UnitDigest: "unit"}
	id := selfupdate.CandidateIdentity{Executable: plan.Executable, Version: "0.2.30", Source: strings.Repeat("a", 40), Digest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	d := sha256.Sum256([]byte("config"))
	if _, found, err := inspectUpgradeJournal(plan, state, d); err != nil || found {
		t.Fatalf("absent: %v %v", found, err)
	}
	if _, err := os.Stat(filepath.Join(state, "upgrade")); !os.IsNotExist(err) {
		t.Fatalf("created directory: %v", err)
	}
	j, err := openUpgradeJournal(plan, state, d, id)
	if err != nil {
		t.Fatal(err)
	}
	j.receipt.Stage = "gateway_stop"
	if err := j.save(); err != nil {
		t.Fatal(err)
	}
	j.close()
	path := filepath.Join(state, "upgrade", "journal.json")
	original := j.prior
	r, found, err := inspectUpgradeJournal(plan, state, d)
	if err != nil || !found || r.Stage != "gateway_stop" {
		t.Fatalf("pending: %+v %v %v", r, found, err)
	}
	for _, unsafe := range [][]byte{[]byte("{}"), []byte(`{"schema":"aegis-upgrade-v1","stage":"unknown"}`)} {
		if err := os.WriteFile(path, unsafe, 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := inspectUpgradeJournal(plan, state, d); err == nil {
			t.Fatal("accepted malformed journal")
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/null", path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := inspectUpgradeJournal(plan, state, d); err == nil {
		t.Fatal("accepted symlink")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(path, filepath.Join(state, "alias")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := inspectUpgradeJournal(plan, state, d); err == nil {
		t.Fatal("accepted hardlink")
	}
}

func TestUpgradeJournalOldCompleteUnitMigration(t *testing.T) {
	state := t.TempDir()
	image := []byte("old-installed-image")
	exe := filepath.Join(state, "aegis")
	if err := os.WriteFile(exe, image, 0700); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(image)
	oldPlan := userservice.Plan{Executable: exe, UnitDigest: "legacy"}
	id := selfupdate.CandidateIdentity{Executable: exe, Version: "0.2.30", Source: strings.Repeat("a", 40), Digest: hex.EncodeToString(sum[:])}
	j, err := openUpgradeJournal(oldPlan, state, [32]byte{}, id)
	if err != nil {
		t.Fatal(err)
	}
	j.receipt.Stage = "complete"
	if err := j.save(); err != nil {
		t.Fatal(err)
	}
	j.close()
	plan := oldPlan
	plan.UnitDigest = "desired"
	plan.LegacyUnitDigest = "legacy"
	id.Version = "0.2.31"
	id.Digest = strings.Repeat("b", 64)
	called := false
	if err := reconcileUpgradeJournal(plan, state, [32]byte{}, id, func() error { called = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("no authentication")
	}
	r, found, err := inspectUpgradeJournal(plan, state, [32]byte{})
	if err != nil || !found || r.Stage != "prepared" {
		t.Fatalf("%+v %v", r, err)
	}
	// The entire ancestor chain must still be present and exact.
	history := filepath.Join(state, "upgrade", "superseded-"+r.SupersededDigest+".json")
	if err := os.WriteFile(history, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := inspectUpgradeJournal(plan, state, [32]byte{}); err == nil {
		t.Fatal("tampered history admitted")
	}
}

func TestPendingLaterConfigDriftDeniedBeforeAuthentication(t *testing.T) {
	state := t.TempDir()
	plan := userservice.Plan{Executable: "/exact/aegis", UnitDigest: "unit"}
	id := selfupdate.CandidateIdentity{Executable: plan.Executable, Version: "0.2.30", Source: strings.Repeat("a", 40), Digest: strings.Repeat("b", 64)}
	j, err := openUpgradeJournal(plan, state, [32]byte{}, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.save(); err != nil {
		t.Fatal(err)
	}
	j.close()
	id.Version = "0.2.31"
	called := false
	if err := reconcileUpgradeJournal(plan, state, sha256.Sum256([]byte("drift")), id, func() error { called = true; return nil }); err == nil || called {
		t.Fatal("configuration drift admitted")
	}
	// Ensure fixtures are canonical, not an unknown-field parsing bypass.
	var r upgradeReceipt
	if err := json.Unmarshal(j.prior, &r); err != nil {
		t.Fatal(err)
	}
}

func TestPendingLaterIntentRequiresReconciliation(t *testing.T) {
	state := t.TempDir()
	plan := userservice.Plan{Executable: "/exact/aegis", UnitDigest: "unit"}
	old := selfupdate.CandidateIdentity{Executable: plan.Executable, Version: "0.2.30", Source: strings.Repeat("a", 40), Digest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	next := selfupdate.CandidateIdentity{Executable: plan.Executable, Version: "0.2.31", Source: strings.Repeat("b", 40), Digest: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	j, err := openUpgradeJournal(plan, state, [32]byte{}, old)
	if err != nil {
		t.Fatal(err)
	}
	j.receipt.Stage = "gateway_stop"
	if err := j.save(); err != nil {
		t.Fatal(err)
	}
	j.close()
	original := append([]byte(nil), j.prior...)
	if other, err := openUpgradeJournal(plan, state, [32]byte{}, next); err == nil {
		other.close()
		t.Fatal("unauthenticated conflict admitted")
	}
	if err := reconcileUpgradeJournal(plan, state, [32]byte{}, next, func() error { return os.ErrPermission }); err == nil {
		t.Fatal("authentication denial admitted")
	}
	if got, err := os.ReadFile(filepath.Join(state, "upgrade", "journal.json")); err != nil || !bytes.Equal(got, original) {
		t.Fatal("denied authentication changed journal")
	}
	if err := reconcileUpgradeJournal(plan, state, [32]byte{}, next, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(state, "upgrade"))
	if err != nil {
		t.Fatal(err)
	}
	var archived bool
	for _, entry := range entries {
		if len(entry.Name()) > len("superseded-.json") && entry.Name()[:len("superseded-")] == "superseded-" {
			archived = true
		}
	}
	if !archived {
		t.Fatal("original receipt not archived")
	}
	receipt, found, err := inspectUpgradeJournal(plan, state, [32]byte{})
	if err != nil || !found || receipt.Version != next.Version || receipt.Stage != "prepared" || receipt.SupersededDigest == "" {
		t.Fatalf("new intent not durably bound: %+v %v", receipt, err)
	}
	history, err := os.ReadFile(filepath.Join(state, "upgrade", "superseded-"+receipt.SupersededDigest+".json"))
	if err != nil || !bytes.Equal(history, original) {
		t.Fatal("history changed")
	}
	if err := reconcileUpgradeJournal(plan, state, [32]byte{}, old, func() error { return nil }); err == nil {
		t.Fatal("downgrade admitted")
	}
}
