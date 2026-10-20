package app

import (
	"errors"
	"github.com/berryhill/aegis/internal/runtime/hermes"
)

// ProviderAuthenticationFailureCode projects only reviewed closed diagnostics.
// Adapters must never classify native error text or depend on runtime types.
func ProviderAuthenticationFailureCode(err error) string {
	if errors.Is(err, hermes.ErrProviderAuthRuntimeUnqualified) {
		return "doer_provider_auth_runtime_unqualified"
	}
	if code := hermes.NativeProviderFailureCode(err); code != "" {
		return code
	}
	switch {
	case errors.Is(err, hermes.ErrProviderAuthUnauthorized):
		return "doer_provider_auth_unauthorized"
	case errors.Is(err, hermes.ErrProviderAuthExpired):
		return "doer_provider_auth_expired"
	case errors.Is(err, hermes.ErrProviderAuthAbsent):
		return "doer_provider_auth_absent"
	case errors.Is(err, hermes.ErrProviderAuthInvalid):
		return "doer_provider_auth_invalid"
	}
	return ""
}

// ProviderFormatDiagnostic is a value-only application projection, not evidence.
type ProviderFormatDiagnostic struct {
	Parameter string `json:"parameter,omitempty"`
	Code      string `json:"code,omitempty"`
}
