package command

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/slash"
)

func TestRenderGuidedProviderAndImportContinuation(t *testing.T) {
	for _, reason := range []string{"agent_provider_setup", "local_hermes_agent_import_prepared", "local_hermes_agent_import_confirmed"} {
		wire := slash.Result{State: "completed", Reason: reason, Data: map[string]any{"message": "Separate exact-charter decision\n/agents providers exact-agent approve review sha256:exact\nExecutable: false\x1b[2J", "setup": map[string]any{"unused_private": "must-not-dump"}}}
		raw, _ := json.Marshal(wire)
		var decoded slash.Result
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		renderGatewayResult(&out, decoded)
		if !strings.Contains(out.String(), "AEGIS / authoritative\nSeparate exact-charter decision\n/agents providers exact-agent approve review sha256:exact") || strings.Contains(out.String(), "must-not-dump") || strings.Contains(out.String(), "\x1b") {
			t.Fatalf("bad renderer: %s", out.String())
		}
	}
}
