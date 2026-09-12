package consoleweb

import (
	"os"
	"strings"
	"testing"
)

// Presentation contracts preserve native evidence actions and server-derived
// relationship values. They are not authenticated visual acceptance.
func TestRegistryHeaderAndRelationshipGrouping(t *testing.T) {
	data, err := os.ReadFile("components.templ")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, want := range []string{
		`<section class="agent-evidence-card" aria-label="Provisioning evidence">`,
		`<section class="agent-evidence-card" aria-label="Current session and execution">`,
		`<p class="inline-notice authority-result"><strong>{ record.Agent.AuthorityState }</strong> · { record.Agent.EffectiveAuthority }</p>`,
		`<div class="detail-actions"><a class="secondary" href={ agentRevisionRoute(surface, record) }>Refresh authority evidence</a>`,
		`<span class="rc-chain"><span>{ record.Source }</span><i aria-hidden="true">→</i><span>{ record.Owner }</span></span>`,
	} {
		if !strings.Contains(source, want) {
			t.Errorf("missing scoped Registry grouping: %s", want)
		}
	}
	data, err = os.ReadFile("app.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(data)
	for _, want := range []string{
		`.agent-inline-detail .detail-actions{display:flex;flex-wrap:wrap;gap:var(--s2);margin-left:auto}`,
		`.registry-card .rc-chain{flex-wrap:wrap;align-items:center;gap:5px;margin:13px 0 11px}`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("missing scoped Registry layout: %s", want)
		}
	}
}
