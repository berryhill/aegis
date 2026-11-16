package hermes

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
)

func TestSessionLaneAuthenticatesEffectiveProviderAndRetainsEnvelope(t *testing.T) {
	for _, kind := range []string{"selected", "missing-auth", "unqualified", "revoked", "unapproved", "tools", "credentials", "runtime-drift"} {
		t.Run(kind, func(t *testing.T) {
			a, root := attemptTestAdapter(t, `#!/bin/sh
# Discovery/tool probing happens before the credential-bearing gateway launch.
if [ -n "$HERMES_TUI_PROVIDER" ]; then
  [ -s "$HERMES_HOME/auth.json" ] || exit 90
  [ "$HERMES_TUI_PROVIDER" = codex ] || exit 91
  [ "$HERMES_MODEL" = selected-model ] || exit 92
  [ "$HERMES_TUI_MODEL" = selected-model ] || exit 93
  [ -z "$AEGIS_CODEX_ACCESS_TOKEN$OPENAI_API_KEY$CODEX_HOME$HTTP_PROXY$HTTPS_PROXY" ] || exit 94
fi
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready","payload":{}}}'
read tools
printf '%s\n' '{"jsonrpc":"2.0","id":"aegis-tools","result":{"total":0,"sections":[]}}'
while read line; do :; done
`)
			r := providerAttempt(root)
			h := core.HermesConfig{Provider: "none", Model: "proof-no-key", AllowedLanes: []core.HermesLane{
				{ID: "default", Provider: "none", Model: "proof-no-key"},
				{ID: "selected", Provider: "codex", Model: "selected-model", ProviderAuthentication: &core.ProviderAuthentication{Mode: core.ControllerCodexAccessTokenV1}},
			}}
			r.Launch.Mandate.Hermes = h
			r.Launch.AuthorityContext.Authority.Hermes = h
			if kind == "tools" {
				r.Launch.Mandate.Tools = []string{"web"}
				r.Launch.AuthorityContext.Authority.Tools = []string{"web"}
			}
			if kind == "credentials" {
				r.Launch.Mandate.Scopes.Credentials = []string{"provider:codex"}
				r.Launch.AuthorityContext.Authority.Credentials = []string{"provider:codex"}
			}
			if kind == "runtime-drift" {
				r.Launch.Mandate.Runtime.Version = "0.18.1"
				r.Launch.AuthorityContext.Runtime.Version = "0.18.1"
			}
			r.Launch.AuthorityContext.Digest = core.AuthorityContextDigest(r.Launch.AuthorityContext)
			before := core.Digest(r.Launch)
			calls := 0
			a.providerTransportQualifier = func(core.RuntimeDescriptor) error {
				if kind == "unqualified" {
					return ErrProviderAuthRuntimeUnqualified
				}
				return nil // Synthetic gateway proof only, not installed qualification.
			}
			if kind != "missing-auth" {
				a.SetProviderAuthenticationResolver(func(_ context.Context, cfg core.HermesConfig) (ProviderAccessToken, error) {
					calls++
					if cfg.Provider != "codex" || cfg.Model != "selected-model" || len(cfg.AllowedLanes) != 0 {
						t.Fatal("wrong effective transport")
					}
					return NewProviderAccessToken(cfg.Provider, syntheticProviderToken(time.Now().Add(time.Hour)), time.Now())
				})
			}
			model := "selected-model"
			if kind == "unapproved" {
				model = "unapproved"
			}
			fresh := func(context.Context) error {
				if kind == "revoked" {
					return errors.New("revoked")
				}
				return nil
			}
			id, home, _, tools, err := a.LaunchLane(context.Background(), root, r.Launch.Mandate, r.Launch.AuthorityContext, "codex", model, nil, BrokerBridge{}, fresh)
			if kind == "selected" {
				if err != nil || calls != 1 || len(tools) != 0 {
					t.Fatalf("selected transport failed: %v calls=%d", err, calls)
				}
				if _, err := os.Stat(filepath.Join(home, "auth.json")); err != nil {
					t.Fatal("missing access-only material", err)
				}
				if err := a.Terminate(context.Background(), id, true); err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				_ = a.Terminate(context.Background(), id, true)
				t.Fatal("unsafe lane admitted")
			}
			if core.Digest(r.Launch) != before {
				t.Fatal("immutable launch envelope changed")
			}
			if dirs, e := os.ReadDir(filepath.Join(root, "runtime")); e == nil && len(dirs) != 0 {
				t.Fatal("failed/ended session retained transport home")
			}
		})
	}
}

func TestCredentialFreeSessionLaneStillRequiresFreshLifecycle(t *testing.T) {
	for _, kind := range []string{"cancel", "expiry", "revoked-after-probe"} {
		t.Run(kind, func(t *testing.T) {
			a, root := attemptTestAdapter(t, `#!/bin/sh
[ ! -e "$HERMES_HOME/auth.json" ] || exit 90
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready","payload":{}}}'
read tools
printf '%s\n' '{"jsonrpc":"2.0","id":"aegis-tools","result":{"total":0,"sections":[]}}'
while read line; do :; done
`)
			a.providerTransportQualifier = func(core.RuntimeDescriptor) error { t.Fatal("unused Codex transport qualified"); return nil }
			a.SetProviderAuthenticationResolver(func(context.Context, core.HermesConfig) (ProviderAccessToken, error) {
				t.Fatal("unused Codex login read")
				return ProviderAccessToken{}, ErrProviderAuthAbsent
			})
			r := providerAttempt(root)
			h := r.Launch.Mandate.Hermes
			h.AllowedLanes = []core.HermesLane{{ID: "default", Provider: h.Provider, Model: h.Model}, {ID: "fixture", Provider: "none", Model: "proof-no-key"}}
			r.Launch.Mandate.Hermes = h
			r.Launch.AuthorityContext.Authority.Hermes = h
			if kind == "expiry" {
				r.Launch.Mandate.ExpiresAt = time.Now().Add(500 * time.Millisecond)
				r.Launch.AuthorityContext.ExpiresAt = r.Launch.Mandate.ExpiresAt
			}
			r.Launch.AuthorityContext.Digest = core.AuthorityContextDigest(r.Launch.AuthorityContext)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			fresh := func(context.Context) error {
				calls++
				if kind == "revoked-after-probe" && calls > 1 {
					return errors.New("revoked")
				}
				return nil
			}
			id, home, _, _, err := a.LaunchLane(ctx, root, r.Launch.Mandate, r.Launch.AuthorityContext, "none", "proof-no-key", nil, BrokerBridge{}, fresh)
			if kind == "revoked-after-probe" {
				if !errors.Is(err, ErrProviderAuthUnauthorized) || calls != 2 {
					t.Fatalf("fresh denial: %v calls=%d", err, calls)
				}
				if dirs, e := os.ReadDir(filepath.Join(root, "runtime")); e != nil || len(dirs) != 0 {
					t.Fatal("denial retained home", e)
				}
				return
			}
			if err != nil || calls != 2 {
				t.Fatalf("launch: %v calls=%d", err, calls)
			}
			t.Cleanup(func() { _ = a.Terminate(context.Background(), id, true) })
			if kind == "cancel" {
				cancel()
			}
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				_, e := os.Stat(home)
				if !a.Alive(id) && os.IsNotExist(e) {
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Fatal("explicit credential-free lane survived cancellation/deadline")
		})
	}
}

func TestSessionLocalLaneCannotFallThroughDefaultCodex(t *testing.T) {
	r := providerAttempt(t.TempDir())
	h := r.Launch.Mandate.Hermes
	h.AllowedLanes = []core.HermesLane{
		{ID: "default", Provider: h.Provider, Model: h.Model},
		{ID: "local", Provider: "ollama", Model: "exact:1", LocalInference: &core.LocalInference{Kind: "ollama", Endpoint: "http://127.0.0.1:11434", ModelDigest: "sha256:" + strings.Repeat("a", 64)}},
	}
	r.Launch.Mandate.Hermes = h
	r.Launch.AuthorityContext.Authority.Hermes = h
	r.Launch.AuthorityContext.Digest = core.AuthorityContextDigest(r.Launch.AuthorityContext)
	// No listener or runtime may be created before the local fresh-admission gate.
	a := &Adapter{}
	_, _, _, _, err := a.LaunchLane(context.Background(), r.StateRoot, r.Launch.Mandate, r.Launch.AuthorityContext, "ollama", "exact:1", nil, BrokerBridge{})
	if err == nil || !strings.Contains(err.Error(), "local inference transport requires fresh authority") {
		t.Fatalf("wrong effective gate: %v", err)
	}
	if _, err := os.Stat(filepath.Join(r.StateRoot, "runtime")); !os.IsNotExist(err) {
		t.Fatal("denial created runtime state")
	}
}
