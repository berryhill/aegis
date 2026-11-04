package skillbundle

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/testprocess"
)

// Opt-in installed CLI acceptance. The parent supplies an independently verified
// archive/executable; absence is explicitly skipped, never model/Hermes proof.
func TestPackagedQueueSkillInstall(t *testing.T) {
	binary := os.Getenv("AEGIS_TEST_PACKAGED_EXECUTABLE")
	archive := os.Getenv("AEGIS_TEST_SKILL_ARCHIVE")
	digest := os.Getenv("AEGIS_TEST_SKILL_ARCHIVE_SHA256")
	revision := os.Getenv("AEGIS_TEST_SKILL_SOURCE_REVISION")
	if archive == "" && digest == "" && revision == "" {
		t.Skip("exact packaged skill artifact not supplied")
	}
	if !filepath.IsAbs(binary) || !filepath.IsAbs(archive) || !digestPattern.MatchString(digest) || !exactRevision(revision) {
		t.Fatal("absolute binary/archive and exact archive digest/full revision required")
	}
	outside := t.TempDir()
	home := filepath.Join(outside, "hermes-home")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) InstalledInventory {
		t.Helper()
		cmd := exec.Command(binary, args...)
		cmd.Dir = outside
		cmd.Env = []string{"HOME=" + outside, "HERMES_HOME=" + home, "PATH=" + os.Getenv("PATH")}
		out, err := testprocess.CombinedOutput(cmd, time.Minute)
		if err != nil {
			t.Fatalf("packaged installer: %v %s", err, out)
		}
		var got InstalledInventory
		if err := json.Unmarshal(out, &got); err != nil {
			t.Fatalf("inventory JSON: %v", err)
		}
		if got.ArchiveDigest != digest || got.SourceRevision != revision || got.AuthorityGranted || got.EvidenceClass != "installed_inventory_verification" {
			t.Fatalf("identity/authority drift: %+v", got)
		}
		return got
	}
	installed := run("skills", "install", archive, digest, revision, home)
	inventory := run("skills", "inventory", home)
	if !reflect.DeepEqual(installed, inventory) {
		t.Fatal("exact inventory readback differs")
	}
	found := false
	for _, slug := range inventory.Skills {
		if slug == "aegis-execution-queue" {
			found = true
		}
	}
	if !found {
		t.Fatal("Queue skill missing from installed inventory")
	}
	// No checkout is needed for these operational progressive-disclosure files.
	root := repositoryRoot(t)
	for _, name := range []string{"SKILL.md", "references/queue-contract.v1.md", "references/bind-runtime.v1.json", "references/reclaim.v1.json", "references/evaluation-ledger.v1.md"} {
		relative := filepath.Join("skills", "aegis-execution-queue", name)
		if got, err := os.ReadFile(filepath.Join(home, relative)); err != nil || !bytes.Equal(got, mustRead(t, filepath.Join(root, relative))) {
			t.Fatalf("portable Queue bytes differ: %s %v", name, err)
		}
	}
	t.Log("installed CLI/inventory bytes verified outside checkout; fresh Hermes skills_list/skill_view and model adherence remain separate")
}
