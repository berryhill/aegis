package app

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/runtime/hermes"
)

type guidedProviderSource struct {
	*providerSetupSource
	count                 int
	inspections, previews int
}

func (f *guidedProviderSource) InspectNativeCodexCandidates(ctx context.Context, home string, now time.Time) (hermes.NativeCodexCandidates, error) {
	f.inspections++
	c, err := f.providerSetupSource.InspectNativeCodexCandidates(ctx, home, now)
	c.DistinctEligibleGrants = f.count
	if f.count == 2 {
		c.Candidates = append(c.Candidates, hermes.NativeCodexCandidate{CredentialID: "nc-" + strings.Repeat("b", 64), AccountFingerprint: "second", Eligible: true})
	}
	if f.count == 0 {
		c.Candidates = nil
	}
	return c, err
}
func (f *guidedProviderSource) PreviewNativeCodexLogin(ctx context.Context, home string, now time.Time, ids ...string) (hermes.NativeCodexMetadata, error) {
	f.previews++
	return f.providerSetupSource.PreviewNativeCodexLogin(ctx, home, now, ids...)
}
func TestGuidedProviderSetup(t *testing.T) {
	for _, count := range []int{1, 2} {
		t.Run(string(rune('0'+count)), func(t *testing.T) {
			s, sub, _, _, source, initial := setupProviderFixture(t, true)
			source.ambiguous = count == 2
			fixture := &guidedProviderSource{providerSetupSource: source, count: count}
			s.nativeCodexMetadata = fixture
			id := initial.Revision.AgentID
			call := func(in AgentProviderSetupInput) AgentProviderSetupResult { return providerCall(t, s, sub, id, in) }
			out := call(AgentProviderSetupInput{Action: "setup", ProfileHome: "default"})
			if count == 2 {
				if out.Connection != nil || out.Next != "" || len(out.Choices) != 2 || fixture.previews != 0 {
					t.Fatalf("ambiguity inferred: %+v", out)
				}
				args := strings.Fields(out.Choices[0].Command)[3:]
				in, err := NewAgentProviderSetupInput(args)
				if err != nil {
					t.Fatal(err)
				}
				out = call(in)
			}
			if out.Connection == nil || out.Review != nil || out.Executable {
				t.Fatal("missing separate connection review")
			}
			if binding, err := config.ReadControllerCodexBinding(s.ConfigFile); err != nil || binding != nil {
				t.Fatal("preview connected")
			}
			connect := AgentProviderSetupInput{Action: "connect", ReviewID: out.Connection.ReviewID}
			out = call(connect)
			if out.Connection.Status != "connected" || out.Review == nil || !strings.Contains(out.Next, out.Review.Charter.Digest) {
				t.Fatal("no exact automatic successor preparation")
			}
			current, _ := s.GetFleetAgentAs(context.Background(), sub, id, 0)
			if current.Revision.Digest != initial.Revision.Digest {
				t.Fatal("connection approved charter")
			}
			if _, err := s.Store.GetCharter(id, 2); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("prepare wrote charter")
			}
			if _, err := s.AgentProviderSetupAs(context.Background(), sub, id, connect); err == nil {
				t.Fatal("connect consent reused")
			}
			approve := AgentProviderSetupInput{Action: "approve", ReviewID: out.Review.ID, CharterDigest: out.Review.Charter.Digest}
			out = call(approve)
			if out.Agent.Revision.Revision != 2 || out.Models.ProviderAuthentication.Mode != core.ControllerCodexAccessTokenV1 || out.Executable {
				t.Fatal("wrong successor")
			}
			inspections, previews := fixture.inspections, fixture.previews
			out = call(approve)
			if fixture.inspections != inspections || fixture.previews != previews || out.Agent.Revision.Revision != 2 {
				t.Fatal("replay reopened source")
			}
		})
	}
}

func TestGuidedSetupNoImplicitModelReadOrFallback(t *testing.T) {
	s, sub, marker, _, source, initial := setupProviderFixture(t, false)
	fixture := &guidedProviderSource{providerSetupSource: source, count: 1}
	s.nativeCodexMetadata = fixture
	if err := os.WriteFile(marker, []byte("invalid model configuration"), 0600); err != nil {
		t.Fatal(err)
	}
	out := providerCall(t, s, sub, initial.Revision.AgentID, AgentProviderSetupInput{Action: "setup", ProfileHome: "default"})
	if out.Reason != "model_read_consent_required" || fixture.inspections != 0 || fixture.previews != 0 {
		t.Fatal("setup implicitly read config/auth")
	}
	// Explicit migration permits model config reading, not native auth reading.
	if err := os.WriteFile(marker, []byte("model: {provider: antigravity-oauth, default: gemini-3-pro}\nfallback_providers: [{provider: openai-codex, model: gpt-5.4}]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	out = providerCall(t, s, sub, initial.Revision.AgentID, AgentProviderSetupInput{Action: "models"})
	out = providerCall(t, s, sub, initial.Revision.AgentID, AgentProviderSetupInput{Action: "approve", ReviewID: out.Review.ID, CharterDigest: out.Review.Charter.Digest})
	out = providerCall(t, s, sub, initial.Revision.AgentID, AgentProviderSetupInput{Action: "setup", ProfileHome: "default"})
	if out.Models.Provider != "antigravity-oauth" || out.Reason != "antigravity_native_transport_unavailable" || fixture.inspections != 0 || fixture.previews != 0 || out.Connection != nil || out.Review != nil {
		t.Fatalf("default switched: %+v", out)
	}
}

func TestGuidedSetupInputStrict(t *testing.T) {
	for _, args := range [][]string{{"setup"}, {"setup", "other"}, {"setup", "default", "label"}, {"setup", "default", "nc-" + strings.Repeat("a", 64), "extra"}} {
		if _, err := NewAgentProviderSetupInput(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
