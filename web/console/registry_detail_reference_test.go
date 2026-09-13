package consoleweb

import (
	"os"
	"strings"
	"testing"
)

// Source-contract proof only; authenticated visual acceptance remains required.
func TestRegistryDetailAcceptedWidthAndValueTypography(t *testing.T) {
	data, err := os.ReadFile("app.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(data)
	for _, rule := range []string{
		".agent-inline-detail .spec{max-width:1000px;border-top:1px solid var(--border-subtle)}",
		".agent-inline-detail .panel-body>.inline-notice{max-width:1000px;line-height:19px}",
		".agent-inline-detail .spec dd{font:12.5px/19px var(--mono)}",
	} {
		if !strings.Contains(css, rule) {
			t.Errorf("missing Registry-scoped accepted reference rule: %s", rule)
		}
	}
	// Keep the shared 88ch default. The accepted Registry-specific cascade
	// overrides it through #agentDetail .panel-body > * at 1000px.
	if !strings.Contains(css, "max-width:88ch}") {
		t.Error("preserve accepted readiness notice width")
	}
	if !strings.Contains(css, ".spec dd{margin:0;overflow-wrap:anywhere;white-space:pre-wrap}") {
		t.Error("preserve shared value styling and evidence wrapping")
	}
}
