package app

import (
	"context"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/runtime/hermes"
)

// Resolve only the existing explicitly configured environment custody binding.
// This path never reads Agent credentials, profiles or host auth files. No
// binding/value is returned through product adapters; secrets stay process-local.
func (s *Service) resolveControllerProviderAuthentication(ctx context.Context, h core.HermesConfig) (hermes.ProviderAccessToken, error) {
	if ctx == nil || ctx.Err() != nil || core.ValidateProviderAuthentication(h, nil, nil) != nil || h.ProviderAuthentication == nil {
		return hermes.ProviderAccessToken{}, hermes.ErrProviderAuthUnauthorized
	}
	binding, ok := s.Config.Credentials.ProviderAuth[h.Provider]
	if !ok || binding.Type != "environment" {
		return hermes.ProviderAccessToken{}, hermes.ErrProviderAuthAbsent
	}
	// An explicit transport target prevents ordinary API keys from accidentally
	// being accepted as Codex OAuth material. It is NOT exported to Hermes.
	if binding.TargetEnv != "AEGIS_CODEX_ACCESS_TOKEN" || s.LookupEnv == nil {
		return hermes.ProviderAccessToken{}, hermes.ErrProviderAuthUnauthorized
	}
	value, ok := s.LookupEnv(binding.SourceEnv)
	if !ok || value == "" {
		return hermes.ProviderAccessToken{}, hermes.ErrProviderAuthAbsent
	}
	token, err := hermes.NewProviderAccessToken(h.Provider, value, s.Now().UTC())
	if err != nil {
		return hermes.ProviderAccessToken{}, err
	}
	if err = s.Hermes.QualifyProviderTransport(ctx); err != nil {
		return hermes.ProviderAccessToken{}, err
	}
	return token, nil
}
