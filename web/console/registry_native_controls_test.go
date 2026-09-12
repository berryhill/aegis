package consoleweb

import (
	"os"
	"strings"
	"testing"
)

// Presentation-only: retain native form submissions, disclosure controls and
// keyboard focus. Source agreement is not a visual acceptance verdict.
func TestRegistryNativeControlPresentation(t *testing.T) {
	data, err := os.ReadFile("app.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(data)
	for _, rule := range []string{
		`.agent-inline-detail .panel-body>details>summary{cursor:pointer;padding:9px 13px;font:600 11px var(--mono);letter-spacing:.5px;text-transform:uppercase;color:var(--muted);list-style-position:inside}`,
		`form.list-bar[action="/console/agents"] button,.agent-inline-detail .lifecycle-form button{display:inline-flex;`,
		`.agent-inline-detail .lifecycle-form label{display:flex;align-items:center;gap:8px;`,
		`@media(min-width:601px){form.list-bar[action="/console/agents"]>.search{flex:0 1 290px}}`,
		`.agent-inline-detail .panel-body>section[aria-label="Effective authority"]{margin-top:22px}`,
		`@media(max-width:600px){.registry-card .rc-facts{grid-template-columns:repeat(2,minmax(0,1fr));gap:12px 16px}.registry-card .rc-facts>div:last-child{grid-column:1/-1}}`,
	} {
		if !strings.Contains(css, rule) {
			t.Errorf("missing scoped native-control styling: %s", rule)
		}
	}
}
