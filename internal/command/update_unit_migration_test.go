package command

import (
	"crypto/sha256"
	selfupdate "github.com/berryhill/aegis/internal/update"
	"github.com/berryhill/aegis/internal/userservice"
	"testing"
)

func TestUpgradeJournalBindsKnownUnitMigrationAcrossCrash(t *testing.T) {
	state := t.TempDir()
	plan := userservice.Plan{UnitDigest: "desired", LegacyUnitDigest: "known-legacy"}
	id := selfupdate.CandidateIdentity{Executable: "/exact/aegis", Version: "1", Source: "source", Digest: "image"}
	d := sha256.Sum256([]byte("config"))
	j, err := openUpgradeJournal(plan, state, d, id)
	if err != nil {
		t.Fatal(err)
	}
	j.receipt.PreviousUnitDigest = plan.LegacyUnitDigest
	j.receipt.Stage = "unit_migration"
	if err = j.save(); err != nil {
		t.Fatal(err)
	}
	j.close()
	// An atomic unit replacement does not change the desired plan. Reopening
	// before any post-write journal update retains exactly the old/new pair.
	j, err = openUpgradeJournal(plan, state, d, id)
	if err != nil {
		t.Fatal(err)
	}
	if j.receipt.UnitDigest != plan.UnitDigest || j.receipt.PreviousUnitDigest != plan.LegacyUnitDigest {
		t.Fatal(j.receipt)
	}
	j.close()
	plan.LegacyUnitDigest = "other-legacy"
	if j, err = openUpgradeJournal(plan, state, d, id); err == nil {
		j.close()
		t.Fatal("unknown migration source accepted")
	}
	plan.LegacyUnitDigest = "known-legacy"
	plan.UnitDigest = "other-desired"
	if j, err = openUpgradeJournal(plan, state, d, id); err == nil {
		j.close()
		t.Fatal("different desired unit accepted")
	}
}
