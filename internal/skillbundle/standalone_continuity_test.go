package skillbundle

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestStandaloneArchiveContinuityDiscovery(t *testing.T) {
	root := repositoryRoot(t)
	archive := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if _, err := BuildArchive(root, archive, "1.2.3", testRevision); err != nil {
		t.Fatal(err)
	}
	standalone := t.TempDir()
	if _, err := verifyArchiveInto(archive, testRevision, standalone); err != nil {
		t.Fatal(err)
	}
	links := regexp.MustCompile(`\[[^\]]*\]\(([^)]+)\)`)
	for _, slug := range []string{"aegis-loop-authoring", "aegis-execution-queue", "aegis-evidence-disposition", "aegis-session-operations", "aegis-approval-provisioning"} {
		t.Run(slug, func(t *testing.T) {
			name := path.Join("skills", slug, "SKILL.md")
			b, err := os.ReadFile(filepath.Join(standalone, name))
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, m := range links.FindAllStringSubmatch(string(b), -1) {
				if !strings.Contains(m[1], "contribution-continuity.md") {
					continue
				}
				found = true
				target := path.Clean(path.Join(path.Dir(name), m[1]))
				if !strings.HasPrefix(target, "skills/") {
					t.Fatalf("continuity prerequisite escapes standalone inventory: %s", target)
				}
				content, err := os.ReadFile(filepath.Join(standalone, target))
				if err != nil {
					t.Fatal(err)
				}
				text := string(content)
				for _, required := range []string{"installed help", "operation-specific", "/contribute resume", "queue result", "unavailable", "real-provider"} {
					if !strings.Contains(text, required) {
						t.Errorf("continuity discovery lacks %q", required)
					}
				}
			}
			if !found {
				t.Fatal("missing bundled continuity discovery")
			}
		})
	}
}
