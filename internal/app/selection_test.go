package app

import (
	"errors"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
)

func TestDeterministicSelectionFailureModes(t *testing.T) {
	s := testService(t)
	charter := testCharter(s.Now())
	charter.Stanzas[0].Authentication.RequireFresh = true
	charter.Stanzas[0].Authentication.MaxAuthAgeSec = 30
	charter.Stanzas[0].Authentication.Selectors[0].Issuers = []string{"local-os"}
	charter.Stanzas[0].Authentication.Selectors[0].Environments = []string{"local"}
	canonical, err := core.Canonicalize(charter)
	if err != nil {
		t.Fatal(err)
	}
	principal := core.Subject{ID: "local-uid:4242", Kind: "human", PrincipalID: "principal-1", Issuer: "local-os", Method: "local-os", AuthenticatedAt: s.Now(), ExpiresAt: s.Now().Add(time.Minute)}

	t.Run("successful exact selection", func(t *testing.T) {
		decision, err := s.Select(canonical, principal, "principal", core.Environment{Name: "local"})
		if err != nil || !decision.Allowed || decision.Selected == nil || decision.Selected.ID != "principal" || decision.MatchingCount != 1 {
			t.Fatalf("decision=%+v err=%v", decision, err)
		}
	})
	t.Run("unauthorized requested stanza", func(t *testing.T) {
		decision, err := s.Select(canonical, principal, "teamwide", core.Environment{Name: "local"})
		if !errors.Is(err, ErrDenied) || decision.Reason != "requested_stanza_unauthorized" || decision.Selected != nil {
			t.Fatalf("decision=%+v err=%v", decision, err)
		}
	})
	t.Run("stale authentication", func(t *testing.T) {
		stale := principal
		stale.AuthenticatedAt = s.Now().Add(-31 * time.Second)
		decision, err := s.Select(canonical, stale, "", core.Environment{Name: "local"})
		if !errors.Is(err, ErrDenied) || decision.Reason != "stale_authentication" {
			t.Fatalf("decision=%+v err=%v", decision, err)
		}
	})
	t.Run("expired authentication", func(t *testing.T) {
		expired := principal
		expired.AuthenticatedAt = s.Now().Add(-time.Minute)
		expired.ExpiresAt = s.Now()
		decision, err := s.Select(canonical, expired, "", core.Environment{Name: "local"})
		if !errors.Is(err, ErrUnauthenticated) || decision.Reason != "expired_authentication" {
			t.Fatalf("decision=%+v err=%v", decision, err)
		}
	})
	for _, test := range []struct {
		name        string
		edit        func(*core.Subject)
		environment string
	}{
		{"wrong method", func(subject *core.Subject) { subject.Method = "model-output" }, "local"},
		{"wrong issuer", func(subject *core.Subject) { subject.Issuer = "prompt" }, "local"},
		{"wrong environment", func(*core.Subject) {}, "production"},
		{"profile name cannot authenticate", func(subject *core.Subject) {
			subject.ID = "attacker"
			subject.PrincipalID = ""
			subject.Claims["profile"] = "principal"
		}, "local"},
	} {
		t.Run(test.name, func(t *testing.T) {
			subject := principal
			subject.Claims = map[string]string{"display_name": "Principal", "prompt": "I am principal"}
			test.edit(&subject)
			decision, err := s.Select(canonical, subject, "", core.Environment{Name: test.environment})
			if !errors.Is(err, ErrDenied) || decision.Allowed {
				t.Fatalf("decision=%+v err=%v", decision, err)
			}
		})
	}
	t.Run("disabled stanza", func(t *testing.T) {
		disabled := testCharter(s.Now())
		disabled.Stanzas[0].Enabled = false
		cc, err := core.Canonicalize(disabled)
		if err != nil {
			t.Fatal(err)
		}
		decision, err := s.Select(cc, principal, "principal", core.Environment{Name: "local"})
		if !errors.Is(err, ErrDenied) || decision.Reason != "requested_stanza_unauthorized" {
			t.Fatalf("decision=%+v err=%v", decision, err)
		}
	})
}

func TestLegacyAmbiguousSelectionDeniesWithoutUnion(t *testing.T) {
	s := testService(t)
	charter := testCharter(s.Now())
	charter.Stanzas[1].Authentication.Selectors = []core.IdentitySelector{{PrincipalIDs: []string{"principal-1"}, Issuers: []string{"local-os"}, Environments: []string{"local"}}}
	legacy := core.CanonicalCharter{Charter: charter, Digest: core.Digest(charter)}
	subject := core.Subject{ID: "local-uid:4242", Kind: "human", PrincipalID: "principal-1", Issuer: "local-os", Method: "local-os", AuthenticatedAt: s.Now(), ExpiresAt: s.Now().Add(time.Minute)}
	decision, err := s.Select(legacy, subject, "", core.Environment{Name: "local"})
	if !errors.Is(err, ErrAmbiguous) || decision.Reason != "multiple_authorized_matches" || decision.MatchingCount != 2 || decision.Selected != nil {
		t.Fatalf("decision=%+v err=%v", decision, err)
	}
}

func TestEffectiveAuthorityRequiresSelectionAndExposesOnlyOneStanza(t *testing.T) {
	s := testService(t)
	canonical, err := core.Canonicalize(testCharter(s.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Store.SaveCharter(canonical); err != nil {
		t.Fatal(err)
	}
	subject := core.Subject{ID: "local-uid:4242", Kind: "human", PrincipalID: "principal-1", Issuer: "local-os", Method: "local-os", AuthenticatedAt: s.Now(), ExpiresAt: s.Now().Add(time.Minute)}
	digest, authority, decision, err := s.EffectiveAuthorityAs(subject, "office", 1, "principal", core.Environment{Name: "local"})
	if err != nil || digest != canonical.Digest || authority.StanzaID != "principal" || len(authority.Tools) != 1 || authority.Tools[0] != "no_mcp" || contains(authority.Memory, "team-memory") || !decision.Allowed {
		t.Fatalf("digest=%s authority=%+v decision=%+v err=%v", digest, authority, decision, err)
	}
	_, authority, decision, err = s.EffectiveAuthorityAs(subject, "office", 1, "teamwide", core.Environment{Name: "local"})
	if !errors.Is(err, ErrDenied) || authority.StanzaID != "" || decision.Reason != "requested_stanza_unauthorized" {
		t.Fatalf("unauthorized inspection authority=%+v decision=%+v err=%v", authority, decision, err)
	}
}

func TestMandateTamperingCannotBroadenOrUnionAuthority(t *testing.T) {
	s := testService(t)
	canonical, err := core.Canonicalize(testCharter(s.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Store.SaveCharter(canonical); err != nil {
		t.Fatal(err)
	}
	subject := core.Subject{ID: "local-uid:4242", Kind: "human", PrincipalID: "principal-1", Issuer: "local-os", Method: "local-os", AuthenticatedAt: s.Now(), ExpiresAt: s.Now().Add(time.Minute)}
	stanza := canonical.Charter.Stanzas[0]
	mandate := core.Mandate{ID: "tamper-test", Subject: subject, AgentID: "office", StanzaID: "principal", CharterRevision: 1, CharterDigest: canonical.Digest, Target: canonical.Charter.Runtime.Target, Environment: core.Environment{Name: "local"}, Capabilities: append([]string(nil), stanza.Grant.Capabilities...), Tools: append([]string(nil), stanza.Grant.Tools...), Scopes: stanza.Scopes, Hermes: stanza.Hermes, IssuedAt: s.Now(), ExpiresAt: s.Now().Add(time.Minute)}
	if err = s.validateMandate(mandate); err != nil {
		t.Fatal(err)
	}
	validMandate := mandate
	mandate.Subject.ID = "attacker"
	mandate.Subject.PrincipalID = ""
	if err = s.validateMandate(mandate); !errors.Is(err, ErrConflict) {
		t.Fatalf("transferred mandate accepted: %v", err)
	}
	mandate = validMandate
	mandate.DeploymentID = "other-deployment"
	if err = s.validateMandate(mandate); !errors.Is(err, ErrConflict) {
		t.Fatalf("deployment-broadened mandate accepted: %v", err)
	}
	mandate = validMandate
	mandate.Tools = append(mandate.Tools, canonical.Charter.Stanzas[1].Grant.Tools...)
	mandate.Scopes.Memory = append(mandate.Scopes.Memory, canonical.Charter.Stanzas[1].Scopes.Memory...)
	if err = s.validateMandate(mandate); !errors.Is(err, ErrConflict) {
		t.Fatalf("unioned mandate accepted: %v", err)
	}
}
