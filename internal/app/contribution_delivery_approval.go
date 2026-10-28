package app

import (
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/delivery"
	"slices"
)

// Reload exactly the session-bound charter, never union other stanza policies.
func contributionDeliveryOperationApproval(ac core.AuthorityContext, c core.CanonicalCharter, op delivery.Operation) error {
	if ac.AgentID == "" || ac.CharterDigest == "" || c.Charter.AgentID != ac.AgentID || c.Charter.Revision != ac.CharterRevision || c.Digest != ac.CharterDigest || core.Digest(c.Charter) != c.Digest {
		return delivery.ErrDenied
	}
	var selected *core.TrustStanza
	for n := range c.Charter.Stanzas {
		stanza := &c.Charter.Stanzas[n]
		if stanza.ID == ac.Authority.StanzaID {
			if selected != nil || !stanza.Enabled {
				return delivery.ErrDenied
			}
			selected = stanza
		}
	}
	if selected == nil {
		return delivery.ErrDenied
	}
	// Retain the conservative effective-context requirement as well.
	if slices.Contains(selected.Approval.RequiredOperations, string(op)) || slices.Contains(ac.Authority.Approval.RequiredOperations, string(op)) {
		return ErrContributionDeliveryApprovalUnavailable
	}
	return nil
}
