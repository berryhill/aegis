package hermes

import (
	"context"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/core"
)

// Until the process-owned transport is integrated, never hand a sealed local
// route to generic Hermes provider handling or discover/start a process.
func TestLocalInferenceCannotFallThroughGenericRuntime(t *testing.T) {
	r := validAttemptTurnRequest(t.TempDir())
	h := core.HermesConfig{Model: "exact:1", Provider: "ollama", LocalInference: &core.LocalInference{Kind: "ollama", Endpoint: "http://127.0.0.1:11434", ModelDigest: "sha256:" + strings.Repeat("a", 64)}}
	r.Launch.Mandate.Hermes = h
	r.Launch.Mandate.Tools = nil
	r.Launch.AuthorityContext.Authority.Hermes = h
	r.Launch.AuthorityContext.Authority.Tools = nil
	r.Launch.AuthorityContext.Digest = core.AuthorityContextDigest(r.Launch.AuthorityContext)
	r.Model = h.Model
	r.Provider = h.Provider
	if err := core.ValidateAuthorityContext(r.Launch.AuthorityContext, r.Launch.Mandate); err != nil {
		t.Fatal(err)
	}
	r.Credentials = []Credential{{Reference: "forbidden"}}
	adapter := &Adapter{}
	if _, err := adapter.AttemptTurn(context.Background(), r); err == nil || !strings.Contains(err.Error(), "local inference") {
		t.Fatalf("attempt did not fail closed: %v", err)
	}
	if _, _, _, _, err := adapter.Launch(context.Background(), r.StateRoot, r.Launch.Mandate, r.Launch.AuthorityContext, nil, BrokerBridge{}); err == nil || !strings.Contains(err.Error(), "local inference") {
		t.Fatalf("launch did not fail closed: %v", err)
	}
}
