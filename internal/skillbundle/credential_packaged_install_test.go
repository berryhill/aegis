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

// Run against an extracted, independently verified archive executable. Source
// command constructors alone cannot establish portable packaged availability.
func TestPackagedCredentialSkillInstall(t *testing.T) {
	binary, archive := os.Getenv("AEGIS_TEST_PACKAGED_EXECUTABLE"), os.Getenv("AEGIS_TEST_SKILL_ARCHIVE")
	digest, revision := os.Getenv("AEGIS_TEST_SKILL_ARCHIVE_SHA256"), os.Getenv("AEGIS_TEST_SKILL_SOURCE_REVISION")
	if binary == "" && archive == "" && digest == "" && revision == "" {
		t.Skip("extracted executable and exact skill archive not supplied")
	}
	if !filepath.IsAbs(binary) || !filepath.IsAbs(archive) || !digestPattern.MatchString(digest) || !exactRevision(revision) {
		t.Fatal("absolute executable/archive and exact identities required")
	}
	outside := t.TempDir()
	home := filepath.Join(outside, "hermes-home")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) []byte {
		t.Helper()
		cmd := exec.Command(binary, args...)
		cmd.Dir = outside
		cmd.Env = []string{"HOME=" + outside, "HERMES_HOME=" + home, "PATH=" + os.Getenv("PATH")}
		output, err := testprocess.CombinedOutput(cmd, time.Minute)
		if err != nil {
			t.Fatalf("packaged metadata command failed: %v", err)
		}
		return output
	}
	var installed, observed InstalledInventory
	if err := json.Unmarshal(run("skills", "install", archive, digest, revision, home), &installed); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(run("skills", "inventory", home), &observed); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(installed, observed) || observed.ArchiveDigest != digest || observed.SourceRevision != revision || observed.AuthorityGranted || observed.EvidenceClass != "installed_inventory_verification" {
		t.Fatal("installed inventory identity/authority mismatch")
	}
	root := repositoryRoot(t)
	for _, name := range []string{"SKILL.md", "references/credential-fixtures.json", "references/protected-intake.v1.md", "references/protected-intake.v1.json"} {
		relative := filepath.Join("skills", "aegis-credential-authority", name)
		actual, err := os.ReadFile(filepath.Join(home, relative))
		if err != nil || !bytes.Equal(actual, mustRead(t, filepath.Join(root, relative))) {
			t.Fatalf("portable credential guidance differs: %s", name)
		}
	}
	for _, operation := range []string{"initialize", "put", "metadata", "list", "rotate", "bind", "revoke", "backup"} {
		help := run("secret", operation, "--help")
		if !bytes.Contains(help, []byte("aegis secret "+operation)) {
			t.Fatalf("packaged secret operation unavailable: %s", operation)
		}
		flags := map[string][]string{"put": {"--stdin", "--kind", "--created-by"}, "rotate": {"--stdin"}, "list": {"--limit"}, "bind": {"--agent", "--stanza", "--scope", "--destination", "--mode", "--pinned-version"}, "revoke": {"--version", "--reason"}}
		for _, flag := range flags[operation] {
			if !bytes.Contains(help, []byte(flag)) {
				t.Fatalf("packaged flag unavailable: %s %s", operation, flag)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(outside, ".aegis")); !os.IsNotExist(err) {
		t.Fatal("packaged discovery unexpectedly opened product state")
	}
	t.Log("packaged install/inventory and secret command/flag discovery verified outside checkout; no credential mutation, Hermes adherence or real-account qualification claimed")
}
