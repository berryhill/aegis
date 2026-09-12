package consoleweb

import (
	"os"
	"strings"
	"testing"
)

// This source contract preserves server-derived readiness/authority semantics
// while matching the accepted responsive card-summary structure. It is not
// pixel approval and must not release visual or publication gates.
func TestRegistryAcceptedFactsLayout(t *testing.T) {
	css, err := os.ReadFile("app.css")
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range []string{
		".registry-card .rc-facts{display:grid;grid-template-columns:repeat(auto-fit,minmax(84px,1fr));gap:10px var(--s3);margin-top:auto;padding-top:11px;border-top:1px solid var(--border-subtle)}",
		".registry-card .rc-facts>div{display:block;min-width:0;padding:0;border-bottom:0}",
		".registry-card{height:100%;display:flex;flex-direction:column;",
	} {
		if !strings.Contains(string(css), rule) {
			t.Errorf("missing accepted Registry card-summary rule: %s", rule)
		}
	}
	markup, err := os.ReadFile("components.templ")
	if err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"Execution readiness", "Authority", "Provisioning"} {
		if !strings.Contains(string(markup), label) {
			t.Errorf("server-derived Registry fact label must remain: %s", label)
		}
	}
}
