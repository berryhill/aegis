package app

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/core"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/registry"
)

func TestAgentProviderSetupOnlySelectedLane(t *testing.T) {
	s, sub, marker, home, source, agent := setupProviderFixture(t, false)
	id := agent.Revision.AgentID
	data := strings.Replace(modelImportConfig, "terminal:", "  - provider: codex\n    model: gpt-5.3\nterminal:", 1)
	if err := os.WriteFile(marker, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	migrate := providerCall(t, s, sub, id, AgentProviderSetupInput{Action: "models"})
	providerCall(t, s, sub, id, AgentProviderSetupInput{Action: "approve", ReviewID: migrate.Review.ID, CharterDigest: migrate.Review.Charter.Digest})
	providerConnect(t, s, sub, id, home, source.entry)
	promote := providerCall(t, s, sub, id, AgentProviderSetupInput{Action: "promote", Provider: "codex", Model: "gpt-5.3"})
	applied := providerCall(t, s, sub, id, AgentProviderSetupInput{Action: "approve", ReviewID: promote.Review.ID, CharterDigest: promote.Review.Charter.Digest})
	if applied.Models.Provider != "openai-codex" || applied.Models.Model != "gpt-5.4" || applied.Models.ProviderAuthentication.Mode != core.ConfiguredProviderV1 {
		t.Fatal("unreviewed default promoted")
	}
	for _, lane := range applied.Models.AllowedLanes {
		want := core.ConfiguredProviderV1
		if lane.Provider == "codex" && lane.Model == "gpt-5.3" {
			want = core.ControllerCodexAccessTokenV1
		}
		if lane.ProviderAuthentication == nil || lane.ProviderAuthentication.Mode != want {
			t.Fatal("unreviewed lane changed")
		}
	}
}

func TestAgentProviderSetupConnectionConsent(t *testing.T) {
	s, sub, _, home, source, agent := setupProviderFixture(t, true)
	id := agent.Revision.AgentID
	ctx := context.Background()
	prepare := func() AgentProviderSetupResult {
		return providerCall(t, s, sub, id, AgentProviderSetupInput{Action: "review", ProfileHome: home, CredentialID: source.entry})
	}
	p := prepare()
	other := sub
	other.Claims = map[string]string{"uid": s.Config.Principal.UID, "other": "subject"}
	if _, err := s.AgentProviderSetupAs(ctx, other, id, AgentProviderSetupInput{Action: "connect", ReviewID: p.Connection.ReviewID}); !errors.Is(err, ErrDenied) {
		t.Fatal("foreign review subject admitted", err)
	}
	source.account = "changed"
	if _, err := s.AgentProviderSetupAs(ctx, sub, id, AgentProviderSetupInput{Action: "connect", ReviewID: p.Connection.ReviewID}); !errors.Is(err, ErrConflict) {
		t.Fatal("account drift admitted", err)
	}
	source.account = "synthetic-account"
	p = prepare()
	now := s.Now().Add(6 * time.Minute)
	s.Now = func() time.Time { return now }
	sub.AuthenticatedAt = now
	sub.ExpiresAt = now.Add(time.Minute)
	if _, err := s.AgentProviderSetupAs(ctx, sub, id, AgentProviderSetupInput{Action: "connect", ReviewID: p.Connection.ReviewID}); err == nil {
		t.Fatal("expired review admitted")
	}
	p = prepare()
	changed, err := s.SetAgentLifecycleAs(ctx, sub, id, SetAgentLifecycleInput{Expected: agentRevisionRef(agent.Revision), Lifecycle: registry.LifecycleDisabled})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AgentProviderSetupAs(ctx, sub, id, AgentProviderSetupInput{Action: "connect", ReviewID: p.Connection.ReviewID}); !errors.Is(err, ErrConflict) {
		t.Fatal("stale Agent admitted", err)
	}
	if binding, err := config.ReadControllerCodexBinding(s.ConfigFile); err != nil || binding != nil {
		t.Fatal("denied connection persisted")
	}
	p = prepare()
	input := AgentProviderSetupInput{Action: "connect", ReviewID: p.Connection.ReviewID}
	errs := make(chan error, 4)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() { _, err := s.AgentProviderSetupAs(ctx, sub, id, input); errs <- err })
	}
	wg.Wait()
	close(errs)
	success := 0
	for err := range errs {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("duplicate connect effects: %d", success)
	}
	read := providerCall(t, s, sub, id, AgentProviderSetupInput{Action: "status"})
	if read.Connection.Status != "connected" || read.Agent.Revision.Digest != changed.Revision.Digest {
		t.Fatal("connection changed disabled Agent")
	}
}
