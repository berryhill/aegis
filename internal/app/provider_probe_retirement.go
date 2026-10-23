package app

import (
	"context"
	"encoding/json"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

// Diagnostics create their own zero-authority session, not a Queue reservation.
// A completed diagnostic must not poison subsequent tasks for the same Agent.
// This accepts only authoritative terminal lifecycle plus the exact stored probe
// dispatch, never process absence, an arbitrary session reason, or live authority.
func providerProbeRetirementMatches(sub core.Subject, agent registry.AgentRevision, authority core.AuthorityContext, session core.Session, dispatch execution.Dispatch, admitted bool) bool {
	return !admitted && session.Status == "terminated" && session.EndReason == "provider_format_probe_complete" && session.EndedAt != nil && !session.EndedAt.Before(session.StartedAt) &&
		session.ID == authority.SessionID && authority.SubjectID == sub.ID && session.Mandate.Subject.PrincipalID == sub.PrincipalID && authority.AgentID == agent.AgentID && authority.CharterDigest == agent.Charter.Digest &&
		core.ValidateAuthorityContext(authority, session.Mandate) == nil && len(authority.Authority.Capabilities) == 0 && len(authority.Authority.Tools) == 0 && len(authority.Authority.Memory) == 0 && len(authority.Authority.Credentials) == 0 &&
		dispatch.AuthorityContextID == authority.ID && dispatch.State == execution.StateSucceeded && dispatch.Reason == "clean_diagnostic_session_started" && dispatch.FinishedAt != nil
}

func (s *Service) completedProviderProbeAuthority(ctx context.Context, sub core.Subject, agent registry.AgentRevision, authority core.AuthorityContext) bool {
	session, err := s.GetSession(authority.SessionID)
	if err != nil || session.EndedAt == nil || session.EndedAt.After(s.Now()) {
		return false
	}
	admission, err := s.AuthorityCommands.AuthorityAdmission(ctx, authority.ID, authority.Digest, s.Now())
	if err != nil || admission.Admitted {
		return false
	}
	matched := false
	if err = s.Store.List("controller-provider-probe-dispatches", func(raw json.RawMessage) error {
		var dispatch execution.Dispatch
		if json.Unmarshal(raw, &dispatch) != nil {
			return ErrDenied
		}
		if providerProbeRetirementMatches(sub, agent, authority, session, dispatch, admission.Admitted) {
			matched = true
		}
		return nil
	}); err != nil || !matched {
		return false
	}
	return s.predecessorAuthorityIdle(ctx, reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: authority.ID, Digest: authority.Digest}) == nil
}
