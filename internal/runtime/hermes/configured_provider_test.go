package hermes

import (
	"context"
	"errors"
	"testing"

	"github.com/berryhill/aegis/internal/core"
)

func TestConfiguredProviderNeverResolvesCredentials(t *testing.T) {
	for _, provider := range []string{"codex", "openai-codex", "antigravity-oauth"} {
		t.Run(provider, func(t *testing.T) {
			a := &Adapter{}
			a.SetProviderAuthenticationResolver(func(context.Context, core.HermesConfig) (ProviderAccessToken, error) {
				t.Fatal("metadata snapshot consulted credentials")
				return ProviderAccessToken{}, nil
			})
			h := core.HermesConfig{Provider: provider, Model: "gpt-5.4", ProviderAuthentication: &core.ProviderAuthentication{Mode: core.ConfiguredProviderV1}}
			if _, err := a.ResolveProviderAuthentication(context.Background(), h); !errors.Is(err, ErrProviderAuthUnauthorized) {
				t.Fatalf("configuration treated as authentication: %v", err)
			}
		})
	}
}
