package specs

import (
	"errors"
	"testing"
	"time"
)

func validCharter() Charter {
	return Charter{
		SchemaVersion: "aegis.dev/v1alpha1",
		AgentID:       "office",
		Name:          "Principal Office",
		Revision:      1,
		Runtime: RuntimeConstraint{
			AdapterID: "hermes",
			RuntimeID: "hermes-agent",
		},
		Stanzas: []TrustStanza{
			{
				ID:      "principal",
				Name:    "principal",
				Enabled: true,
				Authentication: AuthenticationPolicy{
					AllowedMethods: []string{"local-os"},
					Selectors:      []IdentitySelector{{PrincipalIDs: []PrincipalID{"principal-1"}}},
				},
				Grant:           CapabilityGrant{Tools: []ToolID{"read_file"}},
				Session:         SessionPolicy{MaximumLifetime: time.Hour},
				InformationFlow: InformationFlowPolicy{CrossStanza: InformationFlowDeny},
			},
			{
				ID:      "teamwide",
				Name:    "teamwide",
				Enabled: true,
				Authentication: AuthenticationPolicy{
					AllowedMethods: []string{"service-certificate"},
					Selectors:      []IdentitySelector{{Kinds: []SubjectKind{SubjectAgent}}},
				},
				Grant:           CapabilityGrant{Tools: []ToolID{"kanban_list"}},
				Session:         SessionPolicy{MaximumLifetime: 30 * time.Minute},
				InformationFlow: InformationFlowPolicy{CrossStanza: InformationFlowDeny},
			},
		},
	}
}

func TestValidateCharterAcceptsMVPShape(t *testing.T) {
	if err := ValidateCharter(validCharter()); err != nil {
		t.Fatalf("ValidateCharter() error = %v", err)
	}
}

func TestValidateCharterRejectsMissingAndAmbiguousStructure(t *testing.T) {
	tests := map[string]func(*Charter){
		"no stanza":         func(c *Charter) { c.Stanzas = nil },
		"duplicate stanza":  func(c *Charter) { c.Stanzas[1].ID = c.Stanzas[0].ID },
		"implicit auth":     func(c *Charter) { c.Stanzas[0].Authentication = AuthenticationPolicy{} },
		"delegation":        func(c *Charter) { c.Stanzas[0].Session.Delegation = true },
		"cross stanza flow": func(c *Charter) { c.Stanzas[0].InformationFlow.CrossStanza = "allow" },
		"wildcard tool":     func(c *Charter) { c.Stanzas[0].Grant.Tools = []ToolID{"*"} },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			charter := validCharter()
			mutate(&charter)
			var validation *ValidationError
			if err := ValidateCharter(charter); !errors.As(err, &validation) {
				t.Fatalf("ValidateCharter() error = %v, want ValidationError", err)
			}
		})
	}
}

func TestValidateDesignLaunchRejectsAuthority(t *testing.T) {
	spec := DesignLaunchSpec{
		Principal:    AuthenticatedSubject{ID: "principal-local", PrincipalID: "principal-1"},
		Isolation:    IsolationDisposableHome,
		ReadOnly:     true,
		Provisioning: true,
	}
	if err := ValidateDesignLaunch(spec); err == nil {
		t.Fatal("ValidateDesignLaunch() error = nil, want provisioning violation")
	}
}

func TestValidateMandateLifecycle(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	mandate := Mandate{
		ID:              "mandate-1",
		Subject:         AuthenticatedSubject{ID: "principal-local"},
		AgentID:         "office",
		StanzaID:        "principal",
		CharterRevision: 1,
		CharterDigest:   "sha256:abc",
		Runtime:         RuntimeDescriptor{AdapterID: "hermes", RuntimeID: "hermes-agent"},
		IssuedAt:        now,
		ExpiresAt:       now.Add(time.Hour),
	}
	if err := ValidateMandate(mandate, now); err != nil {
		t.Fatalf("ValidateMandate() error = %v", err)
	}
	if err := ValidateMandate(mandate, mandate.ExpiresAt); err == nil {
		t.Fatal("ValidateMandate() at expiry = nil, want error")
	}
}
