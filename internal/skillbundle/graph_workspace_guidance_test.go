package skillbundle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Documentation coverage only; service authorization is tested independently.
func TestGraphWorkspaceRecipeDistinguishesAuthorityPaths(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repositoryRoot(t), "skills", "aegis-graph-authoring", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, required := range []string{
		"`agent_id` selector",
		"omit `authority` and `workspace`",
		"server-derived workspace provenance",
		"`preparation-pending`",
		"legacy `awaiting-runtime` remains readable",
		"not runnable `queued`",
		"fresh controller-issued runtime binding",
		"`rejection_idempotency_key` is not a supported field",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("Graph recipe missing %q", required)
		}
	}
}
