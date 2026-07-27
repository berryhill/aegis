package core

import (
	"testing"
	"time"
)

func TestAuthorityContextExactlyBindsMandateAndSession(t *testing.T) {
	mandate, authority := testAuthorityBinding()
	if err := ValidateAuthorityContext(authority, mandate); err != nil {
		t.Fatalf("valid authority rejected: %v", err)
	}
	mutations := []struct {
		name string
		edit func(*AuthorityContext)
	}{
		{"missing session", func(a *AuthorityContext) { a.SessionID = "" }},
		{"subject", func(a *AuthorityContext) { a.SubjectID = "other-subject" }},
		{"runtime", func(a *AuthorityContext) { a.Runtime.Version = "0.18.3" }},
		{"stanza", func(a *AuthorityContext) { a.Authority.StanzaID = "teamwide" }},
		{"tools", func(a *AuthorityContext) { a.Authority.Tools = append(a.Authority.Tools, "web") }},
		{"expiry widening", func(a *AuthorityContext) { a.ExpiresAt = mandate.ExpiresAt.Add(time.Second) }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			changed := authority
			changed.Authority.Tools = append([]string(nil), authority.Authority.Tools...)
			mutation.edit(&changed)
			changed.Digest = AuthorityContextDigest(changed)
			if err := ValidateAuthorityContext(changed, mandate); err == nil {
				t.Fatal("authority mutation accepted")
			}
		})
	}
}

func TestAuthorityRevocationIsSeparateAndEffectiveAtCutoff(t *testing.T) {
	_, authority := testAuthorityBinding()
	before := authority.IssuedAt.Add(time.Second)
	revokedAt := before.Add(time.Second)
	revocation := AuthorityRevocation{ID: "revocation-1", MandateID: authority.MandateID, Reason: "operator", RevokedAt: revokedAt, RecordedBy: "principal"}
	if !AuthorityEffectiveAt(authority, []AuthorityRevocation{revocation}, before) {
		t.Fatal("future revocation disabled authority early")
	}
	if AuthorityEffectiveAt(authority, []AuthorityRevocation{revocation}, revokedAt) {
		t.Fatal("authority remained effective at revocation cutoff")
	}
	if authority.Digest != AuthorityContextDigest(authority) {
		t.Fatal("revocation mutated immutable authority context")
	}
}

func testAuthorityBinding() (Mandate, AuthorityContext) {
	issuedAt := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	expiresAt := issuedAt.Add(time.Hour)
	runtime := RuntimeDescriptor{Name: "Hermes Agent", Runtime: "hermes-agent", Version: "0.18.2"}
	mandate := Mandate{
		ID: "mandate-1", Subject: Subject{ID: "subject-1", Kind: "human", PrincipalID: "principal", Issuer: "local-os", Method: "local-os", AuthenticatedAt: issuedAt, ExpiresAt: expiresAt},
		AgentID: "agent-1", StanzaID: "principal", CharterRevision: 3, CharterDigest: "sha256:charter", Runtime: runtime,
		Capabilities: []string{"chat"}, Tools: []string{"no_mcp"}, Scopes: Scopes{Memory: []string{"private"}, Credentials: []string{"provider:test"}},
		Hermes: HermesConfig{Toolsets: []string{"no_mcp"}}, IssuedAt: issuedAt, ExpiresAt: expiresAt,
	}
	authority := AuthorityContext{
		ID: "authority-1", MandateID: mandate.ID, SessionID: "session-1", SubjectID: mandate.Subject.ID, AgentID: mandate.AgentID,
		CharterRevision: mandate.CharterRevision, CharterDigest: mandate.CharterDigest, Runtime: runtime,
		Authority: EffectiveAuthority{StanzaID: mandate.StanzaID, Capabilities: []string{"chat"}, Tools: []string{"no_mcp"}, Memory: []string{"private"}, Credentials: []string{"provider:test"}, Hermes: mandate.Hermes},
		IssuedAt:  issuedAt, ExpiresAt: expiresAt,
	}
	authority.Digest = AuthorityContextDigest(authority)
	return mandate, authority
}
