package app

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/registry"
	"github.com/berryhill/aegis/internal/runtime/hermes"
	"os"
	"testing"
	"time"
)

type independentDelayedSource struct {
	inner *providerSetupSource
	after func()
}

func (x *independentDelayedSource) InspectNativeCodexCandidates(c context.Context, h string, n time.Time) (hermes.NativeCodexCandidates, error) {
	return x.inner.InspectNativeCodexCandidates(c, h, n)
}
func (x *independentDelayedSource) PreviewNativeCodexLogin(c context.Context, h string, n time.Time, ids ...string) (hermes.NativeCodexMetadata, error) {
	m, e := x.inner.PreviewNativeCodexLogin(c, h, n, ids...)
	if x.after != nil {
		x.after()
	}
	return m, e
}

func TestIndependentConnectEffectBoundary(t *testing.T) {
	for _, mode := range []string{"subject-expiry", "review-expiry", "cancel", "agent-drift"} {
		t.Run(mode, func(t *testing.T) {
			s, sub, _, home, source, agent := setupProviderFixture(t, true)
			id := agent.Revision.AgentID
			now := s.Now().UTC()
			s.Now = func() time.Time { return now }
			sub.AuthenticatedAt = now
			sub.ExpiresAt = now.Add(s.Config.Principal.AuthTTL)
			p := providerCall(t, s, sub, id, AgentProviderSetupInput{Action: "review", ProfileHome: home, CredentialID: source.entry})
			if mode == "review-expiry" {
				now = now.Add(4 * time.Minute)
				sub.AuthenticatedAt = now
				sub.ExpiresAt = now.Add(s.Config.Principal.AuthTTL)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s.nativeCodexMetadata = &independentDelayedSource{inner: source, after: func() {
				switch mode {
				case "subject-expiry":
					now = sub.ExpiresAt.Add(time.Second)
				case "review-expiry":
					now = p.Connection.ExpiresAt.Add(time.Second)
				case "cancel":
					cancel()
				case "agent-drift":
					_, err := s.SetAgentLifecycleAs(context.Background(), sub, id, SetAgentLifecycleInput{Expected: agentRevisionRef(agent.Revision), Lifecycle: registry.LifecycleDisabled})
					if err != nil {
						t.Fatal(err)
					}
				}
			}}
			out, err := s.AgentProviderSetupAs(ctx, sub, id, AgentProviderSetupInput{Action: "connect", ReviewID: p.Connection.ReviewID})
			binding, readErr := config.ReadControllerCodexBinding(s.ConfigFile)
			t.Logf("mode=%s call_error=%v response_connection=%v durable_binding=%t read_error=%v", mode, err, out.Connection, binding != nil, readErr)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if binding != nil {
				t.Fatalf("UNAUTHORIZED EFFECT: connection persisted after %s crossed during native validation", mode)
			}
		})
	}
}

func TestIndependentApprovalFreshnessBoundary(t *testing.T) {
	for _, mode := range []string{"subject-expiry", "review-expiry", "cancel", "agent-drift"} {
		t.Run(mode, func(t *testing.T) {
			s, sub, _, home, source, agent := setupProviderFixture(t, true)
			id := agent.Revision.AgentID
			now := s.Now().UTC()
			s.Now = func() time.Time { return now }
			sub.AuthenticatedAt = now
			sub.ExpiresAt = now.Add(s.Config.Principal.AuthTTL)
			providerConnect(t, s, sub, id, home, source.entry)
			p := providerCall(t, s, sub, id, AgentProviderSetupInput{Action: "promote", Provider: "openai-codex", Model: "gpt-5.4"})
			if mode == "review-expiry" {
				now = now.Add(4 * time.Minute)
				sub.AuthenticatedAt = now
				sub.ExpiresAt = now.Add(s.Config.Principal.AuthTTL)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s.nativeCodexMetadata = &independentDelayedSource{inner: source, after: func() {
				switch mode {
				case "subject-expiry":
					now = sub.ExpiresAt.Add(time.Second)
				case "review-expiry":
					now = p.Review.ExpiresAt.Add(time.Second)
				case "cancel":
					cancel()
				case "agent-drift":
					_, err := s.SetAgentLifecycleAs(context.Background(), sub, id, SetAgentLifecycleInput{Expected: agentRevisionRef(agent.Revision), Lifecycle: registry.LifecycleDisabled})
					if err != nil {
						t.Fatal(err)
					}
				}
			}}
			_, err := s.AgentProviderSetupAs(ctx, sub, id, AgentProviderSetupInput{Action: "approve", ReviewID: p.Review.ID, CharterDigest: p.Review.Charter.Digest})
			_, charterErr := s.Store.GetCharter(id, 2)
			current, agentErr := s.FleetRepository.GetAgentRevision(context.Background(), id, 2)
			t.Logf("mode=%s call_error=%v charter_written=%t agent_written=%t revision=%d", mode, err, charterErr == nil, agentErr == nil, current.Revision)
			if err == nil {
				t.Fatal("invalid effect admitted")
			}
			if mode != "agent-drift" && !errors.Is(agentErr, fleet.ErrNotFound) {
				t.Fatalf("Agent effect not absent: %v", agentErr)
			}
			if mode == "agent-drift" && (agentErr != nil || current.Charter != agent.Revision.Charter || current.Lifecycle != registry.LifecycleDisabled) {
				t.Fatal("drift revision overwritten")
			}
			if !errors.Is(charterErr, os.ErrNotExist) {
				t.Fatalf("EXPIRED APPROVAL EFFECT: charter persisted after %s", mode)
			}
		})
	}
}

func TestIndependentReplayAfterLaterRevision(t *testing.T) {
	s, sub, _, home, source, agent := setupProviderFixture(t, false)
	id := agent.Revision.AgentID
	p := providerCall(t, s, sub, id, AgentProviderSetupInput{Action: "models"})
	in := AgentProviderSetupInput{Action: "approve", ReviewID: p.Review.ID, CharterDigest: p.Review.Charter.Digest}
	first := providerCall(t, s, sub, id, in)
	providerConnect(t, s, sub, id, home, source.entry)
	p2 := providerCall(t, s, sub, id, AgentProviderSetupInput{Action: "promote", Provider: "openai-codex", Model: "gpt-5.4"})
	second := providerCall(t, s, sub, id, AgentProviderSetupInput{Action: "approve", ReviewID: p2.Review.ID, CharterDigest: p2.Review.Charter.Digest})
	replay := providerCall(t, s, sub, id, in)
	if replay.Agent.Revision.Digest != first.Agent.Revision.Digest {
		t.Fatal("replay was latest not exact")
	}
	current, err := s.GetFleetAgentAs(context.Background(), sub, id, 0)
	if err != nil || current.Revision.Digest != second.Agent.Revision.Digest {
		t.Fatal("replay altered current")
	}
	t.Logf("exact_replay_revision=%d latest_revision=%d", replay.Agent.Revision.Revision, current.Revision.Revision)
}
