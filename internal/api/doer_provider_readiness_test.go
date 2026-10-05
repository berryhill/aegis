package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
)

func TestDoerProviderReadinessSeparatesControllerAuthFromAgentCredentials(t *testing.T) {
	for _, test := range []struct {
		name   string
		target string
		value  string
		reason string
	}{
		{name: "unqualified-runtime", target: "AEGIS_CODEX_ACCESS_TOKEN", reason: "doer_provider_auth_runtime_unqualified"},
		{name: "absent", target: "AEGIS_CODEX_ACCESS_TOKEN", reason: "doer_provider_auth_absent"},
		{name: "mismatch", target: "OPENAI_API_KEY", reason: "doer_provider_auth_unauthorized"},
		{name: "invalid", target: "AEGIS_CODEX_ACCESS_TOKEN", value: "not-a-token", reason: "doer_provider_auth_invalid"},
		{name: "expired", target: "AEGIS_CODEX_ACCESS_TOKEN", reason: "doer_provider_auth_expired"},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, subject, input := candidateReadinessFixture(t, "gpt-6.1-sol", func(st *core.TrustStanza) {
				st.Hermes.Provider = "codex"
				st.Hermes.ProviderAuthentication = &core.ProviderAuthentication{Mode: core.ControllerCodexAccessTokenV1}
			})
			expiry := s.Now().Add(time.Hour)
			if test.name == "expired" {
				expiry = s.Now().Add(-time.Second)
			}
			payload, _ := json.Marshal(map[string]any{"exp": expiry.Unix(), "https://api.openai.com/auth": map[string]string{"chatgpt_account_id": "synthetic"}})
			value := "e30." + base64.RawURLEncoding.EncodeToString(payload) + ".synthetic"
			if test.name == "invalid" {
				value = test.value
			}
			if test.name == "absent" {
				value = ""
			}
			s.Config.Credentials.ProviderAuth["codex"] = config.EnvironmentCredentialBinding{Type: "environment", SourceEnv: "AEGIS_SYNTHETIC_AUTH", TargetEnv: test.target}
			s.LookupEnv = func(key string) (string, bool) {
				if key == "AEGIS_SYNTHETIC_AUTH" {
					return value, value != ""
				}
				t.Fatal("unexpected custody lookup")
				return "", false
			}
			ctx := context.Background()
			r, err := s.ReadDoerCandidateReadinessAs(ctx, subject, input)
			if err != nil || !r.CanAuthor || r.CanExecute || r.Reason != test.reason {
				t.Fatalf("readiness %+v %v", r, err)
			}
			if r.ToolFree.State != "ready" || r.CredentialFree.State != "ready" {
				t.Fatal("provider transport became agent authority")
			}
			data, _ := json.Marshal(r)
			if value != "" && strings.Contains(string(data), value) {
				t.Fatal("auth disclosed")
			}
			// Structural validation/publication intentionally remain legal definitions,
			// even when execution readiness is blocked.
			publication := app.PublishLoopInput{AgentID: input.Agent.ID, Revision: input.Candidate, IdempotencyKey: "definition-only"}
			if _, err := s.ValidateLoopAs(ctx, subject, publication); err != nil {
				t.Fatal(err)
			}
			if _, err := s.PublishLoopAs(ctx, subject, publication); err != nil {
				t.Fatal(err)
			}
			items, err := s.ListQueueAs(ctx, subject)
			if err != nil || len(items) != 0 {
				t.Fatal("readiness produced Queue effects", err)
			}
		})
	}
}

func TestDoerProviderReadinessKeepsLegacyProviderScopeDenial(t *testing.T) {
	s, subject, input := candidateReadinessFixture(t, "gpt-6.1-sol", func(st *core.TrustStanza) {
		st.Hermes.Provider = "codex"
		st.Scopes.Credentials = []string{"provider:codex"}
	})
	r, err := s.ReadDoerCandidateReadinessAs(context.Background(), subject, input)
	if err != nil || r.Reason != "doer_agent_credentials_denied" || r.ProviderAuthentication.State != "not_checked" {
		t.Fatalf("legacy authority silently reinterpreted: %+v %v", r, err)
	}
}
