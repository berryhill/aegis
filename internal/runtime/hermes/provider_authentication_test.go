package hermes

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
)

func syntheticProviderToken(expiry time.Time) string {
	payload, _ := json.Marshal(map[string]any{"exp": expiry.Unix(), "https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "synthetic-account"}})
	return "e30." + base64.RawURLEncoding.EncodeToString(payload) + ".synthetic-signature"
}
func providerAttempt(root string) AttemptTurnRequest {
	r := validAttemptTurnRequest(root)
	h := core.HermesConfig{Provider: "codex", Model: "gpt-6.1-sol", ProviderAuthentication: &core.ProviderAuthentication{Mode: core.ControllerCodexAccessTokenV1}}
	r.Launch.Mandate.Hermes = h
	r.Launch.Mandate.Tools = nil
	r.Launch.AuthorityContext.Authority.Hermes = h
	r.Launch.AuthorityContext.Authority.Tools = nil
	r.Launch.AuthorityContext.Digest = core.AuthorityContextDigest(r.Launch.AuthorityContext)
	r.Provider, r.Model = h.Provider, h.Model
	return r
}

func TestProviderAuthenticationTokenShapeExpiryAndRedaction(t *testing.T) {
	now := time.Now().UTC()
	token := syntheticProviderToken(now.Add(time.Hour))
	p, err := NewProviderAccessToken("codex", token, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{fmt.Sprint(p), fmt.Sprintf("%+v", p), fmt.Sprintf("%#v", p)} {
		if strings.Contains(s, token) {
			t.Fatal("formatted token disclosed")
		}
	}
	data, _ := json.Marshal(p)
	if strings.Contains(string(data), token) {
		t.Fatal("JSON token disclosed")
	}
	for _, test := range []struct {
		provider, value string
		want            error
	}{
		{"codex", "", ErrProviderAuthAbsent}, {"codex", "malformed", ErrProviderAuthInvalid},
		{"openai", token, ErrProviderAuthUnauthorized}, {"codex", syntheticProviderToken(now), ErrProviderAuthExpired},
	} {
		if _, err := NewProviderAccessToken(test.provider, test.value, now); !errors.Is(err, test.want) {
			t.Fatalf("error %v want %v", err, test.want)
		}
	}
	home := t.TempDir()
	if err := writeProviderAuthentication(home, p); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(home, "auth.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("auth permissions")
	}
	data, err = os.ReadFile(filepath.Join(home, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), token) || strings.Contains(string(data), "refresh_token") {
		t.Fatal("wrong bounded transport")
	}
	if writeProviderAuthentication(home, p) == nil {
		t.Fatal("auth overwrite allowed")
	}
}

func TestProviderAuthenticationAttemptIsolationAndCleanup(t *testing.T) {
	for _, outcome := range []string{"success", "failure", "cancel", "disclosure"} {
		t.Run(outcome, func(t *testing.T) {
			script := `#!/bin/sh
[ -s "$HERMES_HOME/auth.json" ] || exit 70
[ "$(stat -c %a "$HERMES_HOME/auth.json")" = 600 ] || exit 71
[ -z "$AEGIS_CODEX_ACCESS_TOKEN$OPENAI_API_KEY$CODEX_HOME$HERMES_PROFILE$HTTP_PROXY$HTTPS_PROXY" ] || exit 72
[ "$HERMES_TUI_TOOLSETS" = aegis_empty ] || exit 73
`
			// Use the adapter's closed empty-toolset value, never a caller-selected grant.
			script = strings.ReplaceAll(script, "aegis_empty", emptyToolset)
			if outcome == "failure" {
				script += "exit 74\n"
			} else if outcome == "cancel" {
				script += "while read line; do :; done\n"
			} else {
				script += `printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready","payload":{}}}'
read tools
printf '%s\n' '{"jsonrpc":"2.0","id":"aegis-tools","result":{"total":0,"sections":[]}}'
read create
printf '%s\n' '{"jsonrpc":"2.0","id":"create","result":{"session_id":"provider-session"}}'
read prompt
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.start","session_id":"provider-session","payload":{}}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.complete","session_id":"provider-session","payload":{"status":"complete","text":"hello"}}}'
while read line; do :; done
`
			}
			token := syntheticProviderToken(time.Now().Add(time.Hour))
			if outcome == "disclosure" {
				script = strings.Replace(script, `"text":"hello"`, `"text":"`+token+`"`, 1)
			}
			a, root := attemptTestAdapter(t, script)
			a.providerTransportQualifier = func(core.RuntimeDescriptor) error { return nil } // Synthetic process proof only.
			r := providerAttempt(root)
			a.SetProviderAuthenticationResolver(func(ctx context.Context, h core.HermesConfig) (ProviderAccessToken, error) {
				return NewProviderAccessToken(h.Provider, token, time.Now())
			})
			t.Setenv("OPENAI_API_KEY", "ambient-not-inherited")
			t.Setenv("AEGIS_CODEX_ACCESS_TOKEN", "ambient-not-inherited")
			t.Setenv("CODEX_HOME", "/nonexistent")
			t.Setenv("HTTPS_PROXY", "https://invalid.example")
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			result, err := a.AttemptTurn(ctx, r)
			if outcome == "success" && (err != nil || result.Output != "hello") {
				t.Fatalf("turn: %+v %v", result, err)
			}
			if outcome != "success" && err == nil {
				t.Fatal("failure accepted")
			}
			if strings.Contains(result.Output, token) || (err != nil && strings.Contains(err.Error(), token)) {
				t.Fatal("provider authentication escaped the runtime boundary")
			}
			dirs, err := os.ReadDir(filepath.Join(r.StateRoot, "runtime"))
			if err != nil || len(dirs) != 0 {
				t.Fatal("disposable auth home retained", err)
			}
		})
	}
}

func TestProviderAuthenticationAttemptDeniesAbsentMismatchAndAgentAuthority(t *testing.T) {
	for _, kind := range []string{"absent", "mismatch", "expired", "tools", "credentials", "resolver-error"} {
		t.Run(kind, func(t *testing.T) {
			a, root := attemptTestAdapter(t, "#!/bin/sh\nexit 99\n")
			a.providerTransportQualifier = func(core.RuntimeDescriptor) error { return nil } // Synthetic process proof only.
			r := providerAttempt(root)
			token := syntheticProviderToken(time.Now().Add(time.Hour))
			if kind != "absent" {
				a.SetProviderAuthenticationResolver(func(ctx context.Context, h core.HermesConfig) (ProviderAccessToken, error) {
					if kind == "resolver-error" {
						return ProviderAccessToken{}, errors.New(token)
					}
					provider := h.Provider
					expiry := time.Now().Add(time.Hour)
					if kind == "mismatch" {
						provider = "openai-codex"
					}
					if kind == "expired" {
						expiry = time.Now()
					}
					return NewProviderAccessToken(provider, syntheticProviderToken(expiry), time.Now())
				})
			}
			if kind == "tools" {
				r.Launch.Mandate.Tools = []string{"web"}
				r.Launch.AuthorityContext.Authority.Tools = []string{"web"}
			}
			if kind == "credentials" {
				r.Launch.Mandate.Scopes.Credentials = []string{"provider:codex"}
				r.Launch.AuthorityContext.Authority.Credentials = []string{"provider:codex"}
			}
			r.Launch.AuthorityContext.Digest = core.AuthorityContextDigest(r.Launch.AuthorityContext)
			_, err := a.AttemptTurn(context.Background(), r)
			if err == nil || strings.Contains(err.Error(), token) {
				t.Fatal("unsafe auth accepted or disclosed")
			}
		})
	}
}
