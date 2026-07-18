package core

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func completePolicyCharter() Charter {
	return Charter{
		SchemaVersion: SchemaVersion,
		AgentID:       "policy-agent",
		Name:          "Policy Agent",
		Revision:      1,
		Runtime:       RuntimeConstraint{Adapter: "hermes", Runtime: "hermes-agent", VersionConstraint: ">=0.18.0,<0.19.0", Target: "ephemeral"},
		Stanzas: []TrustStanza{{
			ID: "principal", Name: "Principal", Enabled: true,
			Authentication:  AuthenticationPolicy{Methods: []string{"local-os"}, Selectors: []IdentitySelector{{SubjectIDs: []string{"local-uid:1"}, Issuers: []string{"local-os"}, Environments: []string{"local"}}}, RequireFresh: true, MaxAuthAgeSec: 60},
			Grant:           Grant{Capabilities: []string{"chat"}, Tools: []string{"no_mcp"}},
			Scopes:          Scopes{Memory: []string{"private"}, Credentials: []string{"provider:test"}},
			Session:         SessionPolicy{MaximumLifetimeSec: 60, IdleTimeoutSec: 30, RequireReauth: true},
			Approval:        ApprovalPolicy{RequiredOperations: []string{"provision"}, MaximumLifetimeSec: 60, SingleUse: true},
			InformationFlow: InformationFlowPolicy{CrossStanza: "deny"},
			Hermes:          HermesConfig{Toolsets: []string{"no_mcp"}, Model: "test-model", Provider: "test"},
		}},
		CreatedBy: "principal-1", CreatedAt: time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC),
	}
}

func TestDecodeCharterRequiresEveryPolicyStanza(t *testing.T) {
	data, err := json.Marshal(completePolicyCharter())
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err = json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	stanza := document["stanzas"].([]any)[0].(map[string]any)
	for _, field := range []string{"authentication", "grant", "scopes", "session", "approval", "information_flow", "hermes"} {
		t.Run(field, func(t *testing.T) {
			copyDocument := make(map[string]any, len(document))
			for key, value := range document {
				copyDocument[key] = value
			}
			copyStanza := make(map[string]any, len(stanza))
			for key, value := range stanza {
				copyStanza[key] = value
			}
			delete(copyStanza, field)
			copyDocument["stanzas"] = []any{copyStanza}
			mutated, _ := json.Marshal(copyDocument)
			if _, err := DecodeCharter(bytes.NewReader(mutated)); err == nil || !strings.Contains(err.Error(), "is required") {
				t.Fatalf("missing %s accepted: %v", field, err)
			}
		})
	}
}

func TestCharterRejectsWildcardUnsupportedRuntimeAndAuthentication(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Charter)
	}{
		{"selector wildcard", func(c *Charter) { c.Stanzas[0].Authentication.Selectors[0].SubjectIDs = []string{"*"} }},
		{"unsupported method", func(c *Charter) { c.Stanzas[0].Authentication.Methods = []string{"prompt"} }},
		{"persistent profile", func(c *Charter) { c.Stanzas[0].Hermes.Profile = "principal" }},
		{"persistent home", func(c *Charter) { c.Stanzas[0].Hermes.PersistentHome = true }},
		{"ambient plugin", func(c *Charter) { c.Stanzas[0].Hermes.Plugins = []string{"ambient"} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			charter := completePolicyCharter()
			test.edit(&charter)
			if err := ValidateCharter(charter); err == nil {
				t.Fatal("unsafe policy accepted")
			}
		})
	}
}

func TestAuthorityRelevantMutationsChangeCanonicalDigest(t *testing.T) {
	base := completePolicyCharter()
	canonical, err := Canonicalize(base)
	if err != nil {
		t.Fatal(err)
	}
	mutations := []struct {
		name string
		edit func(*TrustStanza)
	}{
		{"selector", func(s *TrustStanza) { s.Authentication.Selectors[0].SubjectIDs[0] = "local-uid:2" }},
		{"grant", func(s *TrustStanza) { s.Grant.Capabilities[0] = "review" }},
		{"tools", func(s *TrustStanza) { s.Grant.Tools[0], s.Hermes.Toolsets[0] = "web", "web" }},
		{"memory", func(s *TrustStanza) { s.Scopes.Memory[0] = "other" }},
		{"credentials", func(s *TrustStanza) { s.Scopes.Credentials = append(s.Scopes.Credentials, "api/read") }},
		{"session", func(s *TrustStanza) { s.Session.MaximumLifetimeSec = 59 }},
		{"approval", func(s *TrustStanza) { s.Approval.MaximumLifetimeSec = 59 }},
		{"flow", func(s *TrustStanza) { s.InformationFlow.CrossStanza = "allow" }},
		{"model", func(s *TrustStanza) { s.Hermes.Model = "other-model" }},
		{"enabled", func(s *TrustStanza) { s.Enabled = false }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			changed := completePolicyCharter()
			mutation.edit(&changed.Stanzas[0])
			// Even invalid authority changes must alter the bytes whose approval is bound.
			if Digest(changed) == canonical.Digest {
				t.Fatal("authority mutation did not alter charter digest")
			}
		})
	}
}
