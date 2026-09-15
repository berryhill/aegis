package skillbundle

import (
	"path/filepath"
	"strings"
	"testing"
)

// This protects advisory recipe distinctions; it is not service or agent proof.
func TestLoopWorkspaceRecipeDistinguishesAuthorityPaths(t *testing.T) {
	text := string(mustRead(t, filepath.Join(repositoryRoot(t), "skills/aegis-loop-authoring/SKILL.md")))
	for _, required := range []string{
		"`agent_id`", "omit `authority` and `publisher`", "server-derived workspace",
		"Workspace readback does not require a runtime mandate", "control_plane_online",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("Loop recipe missing %q", required)
		}
	}
}
