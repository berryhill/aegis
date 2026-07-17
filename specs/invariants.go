package specs

import (
	"strings"
	"time"
)

// ValidateCharter enforces the structural MVP invariants that do not require a
// runtime adapter, identity provider, or external policy decision.
func ValidateCharter(c Charter) error {
	var violations []Violation
	add := func(field, rule string) {
		violations = append(violations, Violation{Feature: "charter", Field: field, Rule: rule})
	}

	if c.SchemaVersion == "" {
		add("schema_version", "must be explicit")
	}
	if c.AgentID == "" {
		add("agent_id", "must be stable and non-empty")
	}
	if strings.TrimSpace(c.Name) == "" {
		add("name", "must be non-empty")
	}
	if c.Revision == 0 {
		add("revision", "must be greater than zero")
	}
	if c.Runtime.AdapterID == "" || c.Runtime.RuntimeID == "" {
		add("runtime", "adapter and runtime must be explicit")
	}
	if len(c.Stanzas) == 0 {
		add("stanzas", "must contain at least one trust stanza")
	}

	seen := make(map[StanzaID]struct{}, len(c.Stanzas))
	for _, stanza := range c.Stanzas {
		prefix := "stanzas[" + stanza.Name + "]"
		if stanza.ID == "" {
			add(prefix+".id", "must be stable and non-empty")
		} else if _, exists := seen[stanza.ID]; exists {
			add(prefix+".id", "must be unique")
		}
		seen[stanza.ID] = struct{}{}
		if len(stanza.Authentication.AllowedMethods) == 0 || len(stanza.Authentication.Selectors) == 0 {
			add(prefix+".authentication", "must be explicit")
		}
		if stanza.Session.MaximumLifetime <= 0 {
			add(prefix+".session.maximum_lifetime", "must be positive")
		}
		if stanza.Session.Delegation {
			add(prefix+".session.delegation", "must be disabled in the MVP")
		}
		if stanza.InformationFlow.CrossStanza != InformationFlowDeny {
			add(prefix+".information_flow.cross_stanza", "must default to deny in the MVP")
		}
		for _, capability := range stanza.Grant.Capabilities {
			if capability == "*" || strings.EqualFold(string(capability), "all") {
				add(prefix+".grant.capabilities", "wildcards are forbidden")
			}
		}
		for _, tool := range stanza.Grant.Tools {
			if tool == "*" || strings.EqualFold(string(tool), "all") {
				add(prefix+".grant.tools", "wildcards are forbidden")
			}
		}
	}

	if len(violations) != 0 {
		return &ValidationError{Violations: violations}
	}
	return nil
}

// ValidateDesignLaunch enforces that a design runtime is proposal-only and
// isolated from ambient runtime state.
func ValidateDesignLaunch(s DesignLaunchSpec) error {
	var violations []Violation
	add := func(field, rule string) {
		violations = append(violations, Violation{Feature: "design_session", Field: field, Rule: rule})
	}
	if s.Principal.ID == "" || s.Principal.PrincipalID == "" {
		add("principal", "must be authenticated and mapped to a principal")
	}
	if !s.ReadOnly {
		add("read_only", "must be true")
	}
	if s.Provisioning {
		add("provisioning", "must be false")
	}
	if s.AmbientMemory || s.AmbientPlugins || s.AmbientMCP {
		add("ambient_state", "memory, plugins, and MCP must be disabled")
	}
	if s.PersistentProfile {
		add("persistent_profile", "must be false")
	}
	if s.Isolation == "" {
		add("isolation", "must be explicit")
	}
	if len(violations) != 0 {
		return &ValidationError{Violations: violations}
	}
	return nil
}

// ValidateMandate enforces binding, lifetime, and non-delegation assumptions.
func ValidateMandate(m Mandate, now time.Time) error {
	var violations []Violation
	add := func(field, rule string) {
		violations = append(violations, Violation{Feature: "mandate", Field: field, Rule: rule})
	}
	if m.ID == "" || m.Subject.ID == "" || m.AgentID == "" || m.StanzaID == "" {
		add("binding", "mandate, subject, agent, and exactly one stanza must be bound")
	}
	if m.CharterRevision == 0 || m.CharterDigest == "" {
		add("charter", "revision and digest must be bound")
	}
	if m.Runtime.AdapterID == "" || m.Runtime.RuntimeID == "" {
		add("runtime", "must be explicit")
	}
	if m.IssuedAt.IsZero() || !m.ExpiresAt.After(m.IssuedAt) {
		add("lifetime", "must be finite and positive")
	}
	if !m.RevokedAt.IsZero() {
		add("revocation", "revoked mandates are invalid")
	}
	if !now.Before(m.ExpiresAt) {
		add("expiry", "mandate is expired")
	}
	if len(violations) != 0 {
		return &ValidationError{Violations: violations}
	}
	return nil
}
