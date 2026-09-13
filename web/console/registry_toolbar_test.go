package consoleweb

import (
	"os"
	"strings"
	"testing"
)

// Source-only contract: the accepted Registry filter surface applies at desktop
// and mobile widths. This does not certify browser geometry or visual acceptance.
// Preserve server-derived evidence and native GET controls; change no authority.
func TestRegistryToolbarLayoutContract(t *testing.T) {
	data, err := os.ReadFile("app.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(data)
	rule := `form.list-bar[action="/console/agents"]{padding:9px 13px;border:1px solid var(--border);border-radius:var(--radius);background:var(--surface2)}`
	index := strings.Index(css, rule)
	if index < 0 {
		t.Fatal("missing all-width Registry toolbar surface")
	}
	prefix := css[:index]
	if strings.Count(prefix, "{") != strings.Count(prefix, "}") {
		t.Fatal("Registry toolbar surface must not be limited to a media query")
	}
	html := renderFoundation(t, AgentFilters(SurfaceModel{}))
	for _, control := range []string{`action="/console/agents"`, `name="q"`, `name="lifecycle"`, `type="submit"`, `class="count"`} {
		if !strings.Contains(html, control) {
			t.Errorf("Registry filter control or result count lost: %s", control)
		}
	}
}
