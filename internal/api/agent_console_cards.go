package api

import (
	"context"
	"fmt"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	consoleweb "github.com/berryhill/aegis/web/console"
)

// Summaries cover only the visible current roster. Charter declarations and
// evaluated operator authority remain separate; receipt presence is evidence,
// not current provisioning integrity or runtime readiness. No runtime is probed.
func enrichConsoleAgentCards(ctx context.Context, svc *app.Service, subject core.Subject, model *consoleweb.SurfaceModel) error {
	if err := svc.RequirePrincipalIdentity(subject); err != nil {
		return err
	}
	receipts, receiptErr := svc.ListReceipts()
	for i := range model.Records {
		record := &model.Records[i]
		detail := record.Agent
		if detail == nil {
			continue
		}
		detail.DeclaredStanzaSummary, detail.EffectiveAuthoritySummary, detail.ReceiptSummary = "Unavailable", "Unavailable", "Unavailable"
		c, err := svc.GetCharter(detail.CharterID, detail.CharterRevision)
		if err != nil || c.Digest != detail.CharterDigest || c.Charter.Runtime.Adapter != detail.RuntimeAdapter || c.Charter.Runtime.Runtime != detail.Runtime || c.Charter.Runtime.Target != detail.RuntimeTarget {
			continue
		}
		enabled := 0
		for _, stanza := range c.Charter.Stanzas {
			if stanza.Enabled {
				enabled++
			}
		}
		detail.DeclaredStanzaSummary = fmt.Sprintf("%d of %d on", enabled, len(c.Charter.Stanzas))
		digest, authority, decision, evaluationErr := svc.EffectiveAuthorityAs(subject, detail.CharterID, detail.CharterRevision, "", core.Environment{Name: "local"})
		if digest == detail.CharterDigest {
			switch {
			case evaluationErr == nil && decision.Allowed && decision.Selected != nil && decision.MatchingCount == 1:
				detail.EffectiveAuthoritySummary = authority.StanzaID
			case decision.Reason == "multiple_authorized_matches":
				detail.EffectiveAuthoritySummary = "Ambiguous · denied"
			case decision.Reason != "" && !decision.Allowed:
				detail.EffectiveAuthoritySummary = "None · denied"
			}
		}
		if receiptErr == nil {
			detail.ReceiptSummary = "None recorded"
			for _, receipt := range receipts {
				if receipt.CharterDigest == detail.CharterDigest {
					detail.ReceiptSummary = "Receipt recorded"
					break
				}
			}
		}
	}
	return ctx.Err()
}
