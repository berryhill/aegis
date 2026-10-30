package consoleweb

import (
	"os"
	"strings"
	"testing"
)

func TestActionReadinessNarrowNavigationAndDiagnostics(t *testing.T) {
	css, err := os.ReadFile("app.css")
	if err != nil {
		t.Fatal(err)
	}
	// Structural guard complements actual candidate-browser geometry checks.
	for _, rule := range []string{`@media(max-width:600px){#console-header{height:auto;min-height:var(--topbar-h);flex-wrap:wrap`, `#console-header .ghost{white-space:normal;overflow-wrap:anywhere;min-width:0}`, `.nav-rail{position:static}`, `[aria-labelledby="included-loop-templates"] code,.onboard code,.empty-note code,.service-state code{overflow-wrap:anywhere}`} {
		if !strings.Contains(string(css), rule) {
			t.Fatalf("missing narrow navigation/diagnostic rule %q", rule)
		}
	}
}
