package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os/user"
	"path/filepath"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/runtime/hermes"
)

func TestControllerNativeCodexBindingDeniesBeforeSourceUse(t *testing.T) {
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Principal.UID = current.Uid
	cfg.Principal.User = current.Username
	h := core.HermesConfig{Provider: "codex", Model: "gpt-6.1-sol", ProviderAuthentication: &core.ProviderAuthentication{Mode: core.ControllerCodexAccessTokenV1}}
	s := &Service{Config: cfg, Now: time.Now, Current: func() (*user.User, error) { return current, nil }, Hermes: hermes.New("/nonexistent/no-native-source-read", slog.New(slog.NewTextHandler(io.Discard, nil)))}
	s.LookupEnv = func(string) (string, bool) { t.Fatal("native path consulted environment fallback"); return "", false }
	if _, err = s.resolveControllerProviderAuthentication(context.Background(), h); !errors.Is(err, hermes.ErrProviderAuthAbsent) {
		t.Fatal(err)
	}
	cases := []string{"no-approval", "wrong-home", "wrong-owner", "competing-binding", "wrong-provider", "cancelled"}
	for _, kind := range cases {
		t.Run(kind, func(t *testing.T) {
			c := cfg
			c.Credentials.ProviderAuth = map[string]config.EnvironmentCredentialBinding{}
			c.Credentials.ControllerCodexLogin = &config.ControllerCodexLogin{ProfileHome: filepath.Join(current.HomeDir, ".hermes", "profiles", "synthetic"), AccountID: "synthetic-account", ApprovedBy: c.Principal.ID}
			hc := h
			ctx := context.Background()
			switch kind {
			case "no-approval":
				c.Credentials.ControllerCodexLogin.ApprovedBy = ""
			case "wrong-home":
				c.Credentials.ControllerCodexLogin.ProfileHome = "/another-owner/.hermes/profiles/synthetic"
			case "wrong-owner":
				c.Principal.UID = "not-the-controller-uid"
			case "competing-binding":
				c.Credentials.ProviderAuth["openai-codex"] = config.EnvironmentCredentialBinding{Type: "environment", SourceEnv: "CANARY_SOURCE", TargetEnv: "AEGIS_CODEX_ACCESS_TOKEN"}
			case "wrong-provider":
				hc.Provider = "openrouter"
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			s.Config = c
			if _, err = s.resolveControllerProviderAuthentication(ctx, hc); !errors.Is(err, hermes.ErrProviderAuthUnauthorized) {
				t.Fatalf("source not denied before runtime discovery: %v", err)
			}
		})
	}
}

func TestControllerProviderEnvironmentBindingStillExplicit(t *testing.T) {
	s := &Service{Config: config.Defaults(), Now: time.Now}
	h := core.HermesConfig{Provider: "codex", Model: "gpt-6.1-sol", ProviderAuthentication: &core.ProviderAuthentication{Mode: core.ControllerCodexAccessTokenV1}}
	s.Config.Credentials.ProviderAuth["codex"] = config.EnvironmentCredentialBinding{Type: "environment", SourceEnv: "EXACT_SOURCE", TargetEnv: "WRONG_TARGET"}
	s.LookupEnv = func(string) (string, bool) { t.Fatal("unauthorized env read"); return "", false }
	if _, err := s.resolveControllerProviderAuthentication(context.Background(), h); !errors.Is(err, hermes.ErrProviderAuthUnauthorized) {
		t.Fatal(err)
	}
	b := s.Config.Credentials.ProviderAuth["codex"]
	b.TargetEnv = "AEGIS_CODEX_ACCESS_TOKEN"
	s.Config.Credentials.ProviderAuth["codex"] = b
	s.LookupEnv = func(name string) (string, bool) {
		if name != "EXACT_SOURCE" {
			t.Fatal("ambient source read")
		}
		return "", false
	}
	if _, err := s.resolveControllerProviderAuthentication(context.Background(), h); !errors.Is(err, hermes.ErrProviderAuthAbsent) {
		t.Fatal(err)
	}
}
