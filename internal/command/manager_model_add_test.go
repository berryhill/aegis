package command

import (
	"bytes"
	"github.com/berryhill/aegis/internal/config"
	"io"
	"strings"
	"testing"
	"time"
)

func TestManagerModelAddDirectOperatorGrammar(t *testing.T) {
	for _, in := range []string{"Please /agents model-add silas openai-codex gpt-6.2-sol", "`/agents model-add silas openai-codex gpt-6.2-sol`", "/agents show silas"} {
		if _, handled, _ := parseManagerModelAdd(in); handled {
			t.Fatal("prose selected operator path", in)
		}
	}
	selection, handled, err := parseManagerModelAdd("/agents model-add silas openai-codex gpt-6.2-sol")
	if !handled || err != nil || selection.AgentID != "silas" || selection.Provider != "openai-codex" || selection.Model != "gpt-6.2-sol" {
		t.Fatal("literal changed", selection, err)
	}
	for _, in := range []string{"/agents model-add", "/agents model-add silas openai-codex gpt-6.2-sol extra"} {
		if _, handled, err := parseManagerModelAdd(in); !handled || err == nil {
			t.Fatal("malformed command escaped")
		}
	}
}

func TestManagerModelAddInterceptRejectsPipedApproval(t *testing.T) {
	var out bytes.Buffer
	cmd := NewRoot(Dependencies{In: strings.NewReader("/agents model-add silas openai-codex gpt-6.2-sol\nsynthetic-password\nAPPROVE\n"), Out: &out, Err: io.Discard})
	cmd.SetContext(t.Context())
	client := &gatewayManagerClient{mode: "degraded"} // No HTTP client: any dispatch to a model or gateway is a failure.
	err := runGatewayManagerLoop(cmd, config.Defaults(), client, time.Now().Add(time.Minute))
	if err == nil || !strings.Contains(err.Error(), "terminal-backed") || strings.Contains(out.String(), "synthetic-password") {
		t.Fatal("unprotected input accepted/exposed", err)
	}
}
