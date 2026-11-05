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

// Text coverage complements, but does not replace, public decoder/adapter tests.
func TestPortableGraphContractMatchesDeliveredTransportAndPendingState(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repositoryRoot(t), "skills/aegis-graph-authoring/references/submission-contract.v1.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, required := range []string{
		"--target CONSOLE_URL graphs submit REVIEWED_FILE",
		"--target CONSOLE_URL queue preparations",
		"--target CONSOLE_URL queue show RETURNED_ITEM",
		"`preparation-pending`", "legacy `awaiting-runtime`",
		"no standalone CLI submission-history", "never rotate IDs",
		"not complete wire examples", "submission-check-result.v1.json",
		"workspace-publish.v1.json", "runtime-effect or credential authority",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("portable contract missing %q", required)
		}
	}
	for _, obsolete := range []string{"report the unavailable product adapter", "`awaiting_runtime`", "Graph with no inputs"} {
		if strings.Contains(text, obsolete) {
			t.Errorf("stale portable contract %q", obsolete)
		}
	}
}
