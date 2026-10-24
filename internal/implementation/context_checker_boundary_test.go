package implementation

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContextExecutorCheckerRejectsInvalidCandidate(t *testing.T) {
	for _, phase := range []string{"test_drift", "module_drift", "source_drift", "budget", "missing_module", "local_replace"} {
		t.Run(phase, func(t *testing.T) {
			_, c := addContextFixture(t)
			if phase == "local_replace" {
				text := "module synthetic\n\ngo 1.24\nreplace example.org/hidden => ../hidden\n"
				if err := os.WriteFile(filepath.Join(c.Workspace, "go.mod"), []byte(text), 0600); err != nil {
					t.Fatal(err)
				}
				c.RepositoryContext.Files[2].Bytes = int64(len(text))
				c.RepositoryContext.Files[2].Digest = digest([]byte(text))
				c.RepositoryContext.SourceDigest = RepositorySourceDigest(c.RepositoryContext)
			}
			if phase == "missing_module" {
				c.RepositoryContext.Files = c.RepositoryContext.Files[:2]
				c.RepositoryContext.SourceDigest = RepositorySourceDigest(c.RepositoryContext)
			}
			expected, err := CollectRepositoryContext(context.Background(), c, RepositorySourceDigest(c.RepositoryContext))
			if err != nil {
				t.Fatal(err)
			}
			switch phase {
			case "test_drift", "module_drift", "source_drift":
				name := map[string]string{"test_drift": "add_test.go", "module_drift": "go.mod", "source_drift": "add.go"}[phase]
				if err := os.WriteFile(filepath.Join(c.Workspace, name), []byte("package synthetic\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "budget":
				expected.Files[0].Text = strings.Repeat("x", 4097)
				expected.Files[0].Digest = digest([]byte(expected.Files[0].Text))
			}
			if err := materializeCheckerContext(context.Background(), c, expected, filepath.Join(t.TempDir(), "checker")); err == nil {
				t.Fatal("invalid candidate accepted")
			}
		})
	}
}
