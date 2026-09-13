package consoleweb

import (
	"os"
	"strings"
	"testing"
)

// Source contract only: these assertions do not establish visual acceptance.
func TestRegistryMobileLayoutContract(t *testing.T) {
	data, err := os.ReadFile("app.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(data)
	for _, rule := range []string{
		`@media(max-width:760px){form.list-bar[action="/console/agents"]{flex-wrap:wrap;padding:9px 13px;border:1px solid var(--border);border-radius:var(--radius);background:var(--surface2)}`,
		`form.list-bar[action="/console/agents"]>.search{flex:1 1 100%;max-width:none}`,
		`form.list-bar[action="/console/agents"]>.count{flex:0 0 auto;margin-left:auto}`,
		`@media(max-width:420px){.content:has(form[action="/console/agents"]){padding:16px 11px 44px}}`,
	} {
		if !strings.Contains(css, rule) {
			t.Errorf("missing mobile Registry layout rule: %s", rule)
		}
	}
	html := renderFoundation(t, AgentFilters(SurfaceModel{}))
	for _, control := range []string{`action="/console/agents"`, `name="q"`, `name="lifecycle"`, `type="submit"`} {
		if !strings.Contains(html, control) {
			t.Errorf("Registry filter control lost: %s", control)
		}
	}
}
