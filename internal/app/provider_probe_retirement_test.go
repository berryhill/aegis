package app

import (
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

func TestProviderProbeRetirementRequiresExactEndedZeroAuthority(t *testing.T) {
	for _, name := range []string{"valid", "live-authority", "running-session", "wrong-reason", "missing-end", "wrong-owner", "wrong-agent", "wrong-dispatch", "unfinished-dispatch", "privileged"} {
		t.Run(name, func(t *testing.T) {
			issued := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
			ended := issued.Add(time.Second)
			sub := core.Subject{ID: "subject", PrincipalID: "principal", Kind: "human", Issuer: "local-os", Method: "local-os", AuthenticatedAt: issued, ExpiresAt: issued.Add(time.Hour)}
			runtime := core.RuntimeDescriptor{Name: "Hermes Agent", Runtime: "hermes-agent", Version: "0.21.3"}
			mandate := core.Mandate{ID: "mandate", Subject: sub, AgentID: "agent", StanzaID: "principal", CharterRevision: 1, CharterDigest: "sha256:charter", Runtime: runtime, IssuedAt: issued, ExpiresAt: sub.ExpiresAt}
			authority := core.AuthorityContext{ID: "authority", MandateID: mandate.ID, SessionID: "session", SubjectID: sub.ID, AgentID: mandate.AgentID, CharterRevision: 1, CharterDigest: mandate.CharterDigest, Runtime: runtime, Authority: core.EffectiveAuthority{StanzaID: mandate.StanzaID}, IssuedAt: issued, ExpiresAt: sub.ExpiresAt}
			authority.Digest = core.AuthorityContextDigest(authority)
			session := core.Session{ID: authority.SessionID, Mandate: mandate, Status: "terminated", StartedAt: issued, EndedAt: &ended, EndReason: "provider_format_probe_complete"}
			agent := registry.AgentRevision{AgentID: "agent", Charter: reference.RevisionRef{Revision: 1, Digest: mandate.CharterDigest}}
			dispatch := execution.Dispatch{AuthorityContextID: authority.ID, State: execution.StateSucceeded, Reason: "clean_diagnostic_session_started", FinishedAt: &ended}
			admitted := false
			switch name {
			case "live-authority":
				admitted = true
			case "running-session":
				session.Status = "running"
			case "wrong-reason":
				session.EndReason = "unreviewed"
			case "missing-end":
				session.EndedAt = nil
			case "wrong-owner":
				sub.PrincipalID = "foreign"
			case "wrong-agent":
				agent.AgentID = "foreign"
			case "wrong-dispatch":
				dispatch.AuthorityContextID = "other"
			case "unfinished-dispatch":
				dispatch.FinishedAt = nil
			case "privileged":
				authority.Authority.Tools = []string{"terminal"}
				session.Mandate.Tools = []string{"terminal"}
				authority.Digest = core.AuthorityContextDigest(authority)
			}
			got := providerProbeRetirementMatches(sub, agent, authority, session, dispatch, admitted)
			if got != (name == "valid") {
				t.Fatalf("eligible=%v", got)
			}
		})
	}
}
