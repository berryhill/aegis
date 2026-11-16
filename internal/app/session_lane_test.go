package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/runtime/hermes"
)

func TestQueuedSessionUsesRequestedLaneWithoutRewritingAuthority(t *testing.T) {
	s := testService(t)
	now := time.Now().UTC()
	s.Now = func() time.Time { return now }
	ctx := context.Background()
	sub, err := s.Authenticate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c := testCharter(s.Now())
	c.Stanzas = c.Stanzas[:1]
	c.Stanzas[0].Grant = core.Grant{}
	c.Stanzas[0].Scopes = core.Scopes{}
	c.Stanzas[0].Hermes = core.HermesConfig{
		Provider: "openai-codex", Model: "default-codex",
		ProviderAuthentication: &core.ProviderAuthentication{Mode: core.ControllerCodexAccessTokenV1},
		AllowedLanes: []core.HermesLane{
			{ID: "default", Provider: "openai-codex", Model: "default-codex"},
			{ID: "fixture", Provider: "none", Model: "proof-no-key"},
		},
	}
	raw, _ := json.Marshal(c)
	canonical, err := s.ImportCharterAs(ctx, sub, raw)
	if err != nil {
		t.Fatal(err)
	}
	m, _, err := s.previewSessionAs(ctx, sub, c.AgentID, 1, "", core.Environment{Name: "local"}, true)
	if err != nil {
		t.Fatal(err)
	}
	before := core.Digest(m)
	foreign := sub
	foreign.ID = "other-subject"
	for _, kind := range []string{"foreign-subject", "expired-mandate"} {
		who := foreign
		if kind == "expired-mandate" {
			who = sub
			s.Now = func() time.Time { return m.ExpiresAt.Add(time.Second) }
		}
		_, err := s.startSessionLaneAs(ctx, who, m.ID, "none", "proof-no-key")
		var failure *sessionStartFailure
		if !errors.As(err, &failure) || failure.Stage != "mandate_validation" {
			t.Fatalf("%s crossed original admission: %v", kind, err)
		}
		s.Now = func() time.Time { return now }
	}
	if _, err = s.StartSessionAs(ctx, sub, m.ID); !errors.Is(err, hermes.ErrProviderAuthAbsent) {
		t.Fatalf("ordinary session must still require its default authentication: %v", err)
	}
	if _, err = s.startSessionLaneAs(ctx, sub, m.ID, "none", "unapproved"); !errors.Is(err, core.ErrUnauthorizedHermesLane) {
		t.Fatalf("unapproved bootstrap lane accepted: %v", err)
	}
	contexts, err := s.Authority.ListAuthorityContexts(ctx)
	if err != nil || len(contexts) != 0 {
		t.Fatalf("denial issued authority: %v", err)
	}
	session, err := s.startSessionLaneAs(ctx, sub, m.ID, "none", "proof-no-key")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Hermes.Terminate(context.Background(), session.RuntimeSessionID, true) })
	if session.LaunchProvider != "none" || session.LaunchModel != "proof-no-key" {
		t.Fatal("effective launch lane missing from readback")
	}
	readback, err := s.GetSession(session.ID)
	if err != nil || readback.LaunchProvider != session.LaunchProvider || readback.LaunchModel != session.LaunchModel {
		t.Fatal("effective launch lane was not persisted", err)
	}
	if core.Digest(session.Mandate) != before {
		t.Fatal("bootstrap rewrote immutable mandate")
	}
	stored, err := s.GetMandate(m.ID)
	if err != nil || core.Digest(stored) != before {
		t.Fatal("stored mandate drift", err)
	}
	charter, err := s.GetCharter(c.AgentID, 1)
	if err != nil || charter.Digest != canonical.Digest {
		t.Fatal("stored charter drift", err)
	}
	contexts, err = s.Authority.ListAuthorityContexts(ctx)
	if err != nil || len(contexts) != 1 {
		t.Fatal("missing exact session authority", err)
	}
	if err := core.ValidateAuthorityContext(contexts[0], m); err != nil {
		t.Fatal(err)
	}
	if contexts[0].Authority.Hermes.Provider != "openai-codex" {
		t.Fatal("authority envelope was narrowed or rewritten")
	}
}

func TestQueuedLoopSessionLaneUsesOnlyHermesEffects(t *testing.T) {
	for _, tc := range []struct {
		name            string
		steps           []loop.Step
		provider, model string
	}{
		{"empty", nil, "", ""},
		{"default effect", []loop.Step{{Kind: loop.StepAction}}, "", ""},
		{"explicit effect", []loop.Step{{Kind: loop.StepAction, Provider: "ollama", Model: "exact:1"}}, "ollama", "exact:1"},
		{"terminal is not a bootstrap effect", []loop.Step{{Kind: loop.StepTerminal, Provider: "ignored", Model: "ignored"}, {Kind: loop.StepAction, Model: "approved-model"}}, "", "approved-model"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, m := queuedLoopSessionLane(loop.LoopRevision{Steps: tc.steps})
			if p != tc.provider || m != tc.model {
				t.Fatalf("lane = %q/%q", p, m)
			}
		})
	}
}
