package app

import (
	"context"
	"os"

	"strconv"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/runtime/hermes"
)

// Resolve only explicit controller operational bindings, never Agent credentials.
// Native source use requires an exact authenticated connection; no ambient profile discovery. No
// binding/value is returned through product adapters; secrets stay process-local.
func (s *Service) resolveControllerProviderAuthentication(ctx context.Context, h core.HermesConfig) (hermes.ProviderAccessToken, error) {
	if ctx == nil || ctx.Err() != nil || core.ValidateProviderAuthentication(h, nil, nil) != nil || h.ProviderAuthentication == nil {
		return hermes.ProviderAccessToken{}, hermes.ErrProviderAuthUnauthorized
	}
	cfg, err := s.effectiveControllerCodexConfig()
	if err != nil {
		return hermes.ProviderAccessToken{}, err
	}
	if b := cfg.Credentials.ControllerCodexLogin; b != nil {
		// Recheck binding in memory as well as strict config decoding. Principal OS
		// ownership is necessary, not a substitute for explicit connection approval.
		if cfg.Validate() != nil || s.Current == nil || s.Hermes == nil {
			return hermes.ProviderAccessToken{}, hermes.ErrProviderAuthUnauthorized
		}
		current, err := s.Current()
		if err != nil || current == nil || current.Uid != strconv.Itoa(os.Geteuid()) || current.Uid != s.Config.Principal.UID || current.Username != s.Config.Principal.User || !nativeControllerSourceAllowed(b.ProfileHome, current.HomeDir) {
			return hermes.ProviderAccessToken{}, hermes.ErrProviderAuthUnauthorized
		}
		return s.Hermes.ResolveNativeCodexLogin(ctx, h.Provider, b.ProfileHome, b.AccountID, s.Now().UTC(), b.CredentialID)
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
