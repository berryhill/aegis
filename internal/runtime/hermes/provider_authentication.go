package hermes

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/berryhill/aegis/internal/core"
)

var (
	ErrProviderAuthAbsent             = errors.New("doer_provider_auth_absent")
	ErrProviderAuthInvalid            = errors.New("doer_provider_auth_invalid")
	ErrProviderAuthExpired            = errors.New("doer_provider_auth_expired")
	ErrProviderAuthUnauthorized       = errors.New("doer_provider_auth_unauthorized")
	ErrProviderAuthRuntimeUnqualified = errors.New("doer_provider_auth_runtime_unqualified")
)

// Access-only pool entries cannot refresh; the qualified selector uses 120s.
const ProviderTokenRefreshMargin = 120 * time.Second

// Exact transport sources exercised offline using disposable synthetic tokens.
// Minimum-version admission alone does not qualify the access-only schema.
var qualifiedProviderTransport = map[string]string{
	"agent/credential_persistence.py": "86826da2caa1152fd5b96c18f465e72130a155fa8ef5dc883c336dc5b5ebe60f",
	"agent/credential_pool.py":        "24ef6c3a7b1c441da09be17cf08a61230149fe17970d23aa4a45e5a4a784d077",
	"hermes_cli/auth_codex.py":        "cfac1743394306fdc529d62ba287beb68ca7a1efb28cd28c94eefeebbc4625b0",
	"hermes_cli/auth_constants.py":    "8c9ec9ccd8bf7fa4936aa1bfae1446bd8aa2162f042d78e968c57f8b7ca50d07",
	"hermes_cli/auth.py":              "b947d385b4810ddb6f262fad6c589b187726b8ad57c8fc508e75ba16783f0db8",
}

func qualifyProviderTransport(d core.RuntimeDescriptor) error {
	if d.Version != "0.21.3" || !filepath.IsAbs(d.Installation) || gatewayPython(d) == "" {
		return ErrProviderAuthRuntimeUnqualified
	}
	for name, expected := range qualifiedProviderTransport {
		data, err := os.ReadFile(filepath.Join(d.Installation, name))
		if err != nil || len(data) > 2<<20 || fmt.Sprintf("%x", sha256.Sum256(data)) != expected {
			return ErrProviderAuthRuntimeUnqualified
		}
	}
	return nil
}

func (a *Adapter) QualifyProviderTransport(ctx context.Context) error {
	if a == nil {
		return ErrProviderAuthRuntimeUnqualified
	}
	d, err := a.Discover(ctx)
	if err != nil {
		return ErrProviderAuthRuntimeUnqualified
	}
	return a.qualifyProviderDescriptor(d)
}

func (a *Adapter) qualifyProviderDescriptor(d core.RuntimeDescriptor) error {
	if a.providerTransportQualifier != nil {
		return a.providerTransportQualifier(d)
	}
	return qualifyProviderTransport(d)
}

// ProviderAccessToken is controller-only process input. It is never authority,
// persisted Aegis state, environment, argv or model input. JWT claims are checked
// for shape/expiry, NOT signature or remote entitlement; the provider authenticates.
type ProviderAccessToken struct {
	provider  string
	value     string
	expiresAt time.Time
}

func (ProviderAccessToken) String() string               { return "[redacted controller provider authentication]" }
func (p ProviderAccessToken) GoString() string           { return p.String() }
func (ProviderAccessToken) MarshalJSON() ([]byte, error) { return []byte(`"[redacted]"`), nil }

func NewProviderAccessToken(provider, value string, now time.Time) (ProviderAccessToken, error) {
	if provider != "codex" && provider != "openai-codex" {
		return ProviderAccessToken{}, ErrProviderAuthUnauthorized
	}
	if value == "" {
		return ProviderAccessToken{}, ErrProviderAuthAbsent
	}
	if len(value) > 32<<10 || strings.TrimSpace(value) != value {
		return ProviderAccessToken{}, ErrProviderAuthInvalid
	}
	parts := strings.Split(value, ".")
	if len(parts) != 3 || parts[0] == "" || parts[2] == "" {
		return ProviderAccessToken{}, ErrProviderAuthInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ProviderAccessToken{}, ErrProviderAuthInvalid
	}
	var claims struct {
		Exp  int64 `json:"exp"`
		Auth struct {
			Account string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Exp <= 0 || strings.TrimSpace(claims.Auth.Account) == "" {
		return ProviderAccessToken{}, ErrProviderAuthInvalid
	}
	expiry := time.Unix(claims.Exp, 0)
	if !now.Add(ProviderTokenRefreshMargin).Before(expiry) {
		return ProviderAccessToken{}, ErrProviderAuthExpired
	}
	return ProviderAccessToken{provider: provider, value: value, expiresAt: expiry}, nil
}

type ProviderAuthenticationResolver func(context.Context, core.HermesConfig) (ProviderAccessToken, error)

// Startup-only wiring. The resolver must resolve only the explicit approved
// provider, never ambient profiles, fallback accounts or caller-selected paths.
func (a *Adapter) SetProviderAuthenticationResolver(resolve ProviderAuthenticationResolver) {
	a.providerAuthentication = resolve
}

func (a *Adapter) ResolveProviderAuthentication(ctx context.Context, h core.HermesConfig) (ProviderAccessToken, error) {
	if ctx == nil || ctx.Err() != nil {
		return ProviderAccessToken{}, ErrProviderAuthUnauthorized
	}
	if h.ProviderAuthentication == nil || core.ValidateProviderAuthentication(h, nil, nil) != nil {
		return ProviderAccessToken{}, ErrProviderAuthUnauthorized
	}
	if a == nil || a.providerAuthentication == nil {
		return ProviderAccessToken{}, ErrProviderAuthAbsent
	}
	if err := a.QualifyProviderTransport(ctx); err != nil {
		return ProviderAccessToken{}, err
	}
	p, err := a.providerAuthentication(ctx, h)
	if err != nil {
		// Do not expose resolver errors: a custody implementation may embed values.
		for _, allowed := range []error{ErrProviderAuthAbsent, ErrProviderAuthInvalid, ErrProviderAuthExpired, ErrProviderAuthUnauthorized} {
			if errors.Is(err, allowed) {
				return ProviderAccessToken{}, allowed
			}
		}
		return ProviderAccessToken{}, ErrProviderAuthInvalid
	}
	if p.provider != h.Provider {
		return ProviderAccessToken{}, ErrProviderAuthUnauthorized
	}
	return NewProviderAccessToken(p.provider, p.value, time.Now().UTC())
}

func writeProviderAuthentication(home string, p ProviderAccessToken) error {
	if _, err := NewProviderAccessToken(p.provider, p.value, time.Now().UTC()); err != nil {
		return err
	}
	// Hermes's OAuth pool consumes a bounded access token without a refresh token.
	// No singleton, refresh token, inherited pool, Codex CLI home or fallback account.
	data, err := json.Marshal(map[string]any{"version": 1, "providers": map[string]any{}, "credential_pool": map[string]any{"openai-codex": []any{map[string]any{"id": "aegis-controller", "auth_type": "oauth", "source": "manual:aegis-controller", "access_token": p.value}}}})
	if err != nil {
		return ErrProviderAuthInvalid
	}
	f, err := os.OpenFile(filepath.Join(home, "auth.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return ErrProviderAuthInvalid
	}
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return ErrProviderAuthInvalid
	}
	return nil
}

func providerAuthenticationEnv(env []string) []string {
	// Transport credentials must not traverse inherited proxy destinations. HOME
	// and HERMES_HOME already point to the disposable home; no ambient auth paths.
	out := make([]string, 0, len(env))
	for _, s := range env {
		key, _, _ := strings.Cut(s, "=")
		switch key {
		case "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "SSL_CERT_FILE", "SSL_CERT_DIR":
			continue
		}
		out = append(out, s)
	}
	return out
}

func (a *Adapter) prepareProviderAuthentication(ctx context.Context, home string, h core.HermesConfig, fresh func(context.Context) error, cutoff time.Time) error {
	if fresh == nil {
		return ErrProviderAuthUnauthorized
	}
	if err := fresh(ctx); err != nil {
		return ErrProviderAuthUnauthorized
	}
	p, err := a.ResolveProviderAuthentication(ctx, h)
	if err != nil {
		return err
	}
	if cutoff.IsZero() || !cutoff.Before(p.expiresAt.Add(-ProviderTokenRefreshMargin)) {
		return ErrProviderAuthExpired
	}
	if err = writeProviderAuthentication(home, p); err != nil {
		return err
	}
	return nil
}

var _ fmt.Stringer = ProviderAccessToken{}
