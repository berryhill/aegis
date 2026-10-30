package api

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/skillbundle"
)

// Actual installed CLI -> owning Unix service -> durable state and verification.
// Runtime/provider and authority preparation are synthetic isolated fixtures.
// This test does not claim an agent followed prose or qualify a live target.
func TestPackagedFleetRecipes(t *testing.T) {
	scheduleInstalledAPIJourney(t)
	archive := os.Getenv("AEGIS_TEST_SKILL_ARCHIVE")
	revision := os.Getenv("AEGIS_TEST_SKILL_REVISION")
	digest := os.Getenv("AEGIS_TEST_SKILL_DIGEST")
	if archive == "" {
		// Deliberately synthetic provenance for hermetic regression only. Independent
		// acceptance supplies the final exact-revision archive and frozen executable.
		revision = strings.Repeat("a", 40)
		archive = filepath.Join(t.TempDir(), "skills.tar.gz")
		var err error
		digest, err = skillbundle.BuildArchive("../..", archive, "0.2.18", revision)
		if err != nil {
			t.Fatal(err)
		}
	}
	home := t.TempDir()
	if err := os.Chmod(home, 0700); err != nil {
		t.Fatal(err)
	}
	inventory, err := skillbundle.InstallArchive(context.Background(), archive, digest, revision, home)
	if err != nil {
		t.Fatal(err)
	}
	if inventory.AuthorityGranted || len(inventory.Skills) != 15 {
		t.Fatal("installation authority/inventory drift")
	}
	for _, name := range []string{"aegis", "aegis-agent-registry", "aegis-loop-authoring", "aegis-execution-queue", "aegis-evidence-disposition"} {
		b, err := os.ReadFile(filepath.Join(home, "skills", name, "SKILL.md"))
		if err != nil || len(b) == 0 {
			t.Fatalf("installed recipe absent: %s %v", name, err)
		}
	}
	// Exercise the advertised commands, not just their help/JSON declarations.
	// Each isolated owning service retains its store lease throughout the case.
	t.Run("separate-publication-and-exact-run", func(t *testing.T) { installedOnlineDoerExactBytesSuccess(t, true, "reusable") })
	t.Run("retained-create-and-execute", func(t *testing.T) { installedOnlineDoerExactBytesSuccess(t, true, "retained") })
	t.Run("same-intent-preparation-recovery", func(t *testing.T) { installedOnlineDoerExactBytesSuccess(t, true, "failed-launch") })
	t.Run("failure-continuity-distinct-task-and-budget", func(t *testing.T) { installedOnlineDoerExactBytesSuccess(t, true, "reusable-continuity") })
	if _, err := skillbundle.InspectInstalled(home); err != nil {
		t.Fatal(err)
	}
}
