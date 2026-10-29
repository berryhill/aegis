package hermes

import (
	"crypto/sha256"
	_ "embed"
	"errors"
	"fmt"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
	"os"
	"path/filepath"
	"time"
)

//go:embed provider_format_gateway.py
var providerFormatGateway string

// Additional diagnostic integration qualification does not widen or replace
// the five existing auth-source fingerprints.
func qualifyProviderFormat(d core.RuntimeDescriptor) error {
	if err := qualifyProviderTransport(d); err != nil {
		return err
	}
	for name, want := range map[string]string{
		"agent/transports/codex.py":  "1b4d48515c0ddac7db53b3446e69da92dcdc773a5e59dd80b2bec17d49c65103",
		"agent/turn_api_request.py":  "524b412e8ae4bee7760994d90386ee54f2260b03db8ce73460e861ddeaaa9542",
		"agent/error_surface.py":     "5a34474a9943185573818592e5bb1dc7d7e492c9038b7f13dabb601847b1237c",
		"tui_gateway/entry.py":       "cb972bd1774d817ea4c7b59b1488dab2dec9afb8dbe354d697f9c6ff19c62113",
		"tui_gateway/prompt_turn.py": "8d467c7002694067cca5d8a84289c49911cddccf86869be750c1ff926798ca1f",
		"tui_gateway/server.py":      "addaab48a307fc4c7924e1b8210a3fb61f02813d612045609651f79e507fff54",
	} {
		data, e := os.ReadFile(filepath.Join(d.Installation, name))
		if e != nil || len(data) > 2<<20 || fmt.Sprintf("%x", sha256.Sum256(data)) != want {
			return ErrProviderAuthRuntimeUnqualified
		}
	}
	return nil
}

const ProviderFormatPrompt = "Provider format diagnostic only. Reply with OK. Do not use tools or perform any task."

// Only closed literal metadata survives the native SDK boundary. These fields
// are diagnostic, never policy, success evidence, authority or retry advice.
type ProviderFormatDiagnostic struct {
	Parameter string `json:"parameter,omitempty"`
	Code      string `json:"code,omitempty"`
}

func validProviderFormatProbe(r AttemptTurnRequest) bool {
	a := r.Launch.AuthorityContext.Authority
	h := a.Hermes
	return r.Input == ProviderFormatPrompt && r.Bounds.Duration <= 60*time.Second && r.Bounds.OutputBytes <= 4096 &&
		len(a.Tools) == 0 && len(a.Capabilities) == 0 && len(a.Memory) == 0 && len(a.Credentials) == 0 && len(r.Credentials) == 0 &&
		len(h.Toolsets) == 0 && len(h.MCPServers) == 0 && len(h.Plugins) == 0 && h.Profile == "" && !h.PersistentHome && h.LocalInference == nil &&
		h.ProviderAuthentication != nil && (h.Provider == "codex" || h.Provider == "openai-codex")
}

func closedProviderFormatDiagnostic(payload map[string]any) ProviderFormatDiagnostic {
	s, _ := payload["error_surface"].(map[string]any)
	p, _ := s["request_parameter"].(string)
	c, _ := s["request_code"].(string)
	out := ProviderFormatDiagnostic{}
	switch p {
	case "model", "temperature", "top_p", "max_output_tokens", "max_tokens", "reasoning", "reasoning.effort", "reasoning.summary", "text.verbosity", "tools", "tool_choice", "parallel_tool_calls", "store", "stream", "input", "instructions", "prompt_cache_key", "prompt_cache_retention", "service_tier", "include", "context_management":
		out.Parameter = p
	}
	switch c {
	case "invalid_request_error", "unsupported_parameter", "unsupported_value", "invalid_value", "unknown_parameter", "missing_required_parameter", "model_not_found", "invalid_prompt":
		out.Code = c
	}
	return out
}

func (a *Adapter) finishRuntimeFailure(turn execution.Turn, payload map[string]any, probe bool) (AttemptTurnResult, error) {
	result, err := a.finishAttempt(turn, execution.StateFailed, runtimeFailureReason(payload), errors.New("Hermes turn failed"))
	if probe {
		result.Diagnostic = closedProviderFormatDiagnostic(payload)
	}
	return result, err
}
