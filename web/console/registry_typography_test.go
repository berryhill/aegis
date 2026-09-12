package consoleweb

import (
	"os"
	"strings"
	"testing"
)

// Source-only acceptance: Registry names use the accepted artifact's sans
// typography, while identity metadata remains monospace. This does not certify
// visual acceptance, and changes no identity/authority data or publication gate.
// The Registry detail heading follows the digest-verified accepted source.
// Keep this scoped: other domains and authority-bearing content are unchanged.
func TestRegistryAcceptedDetailHeadingTypography(t *testing.T) {
	css, err := os.ReadFile("app.css")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(css), ".agent-inline-detail .detail-title h2{font-size:15px;font-weight:640;letter-spacing:-.01em;margin:0 0 4px}") {
		t.Fatal("Registry detail heading typography diverges from the accepted artifact")
	}
	if !strings.Contains(string(css), ".detail-title h2{margin:0;font-size:18px}") {
		t.Fatal("Shared detail heading typography must remain unchanged")
	}
}

func TestRegistryAcceptedNameTypography(t *testing.T) {
	css, err := os.ReadFile("app.css")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(css), ".registry-card .rc-top strong{font:640 13.5px/19px var(--sans);letter-spacing:-.01em;overflow-wrap:anywhere}") {
		t.Fatal("Registry agent-name typography diverges from the accepted artifact")
	}
	if !strings.Contains(string(css), ".rc-name span{font:11px var(--mono)") {
		t.Fatal("Registry identity metadata must remain monospace")
	}
}
