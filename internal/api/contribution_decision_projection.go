package api

import (
	"github.com/berryhill/aegis/internal/app"
	consoleweb "github.com/berryhill/aegis/web/console"
	"time"
)

func contributionDecisionProjection(d app.ContributionPreparationDecisionView, now time.Time) *consoleweb.ContributionDecisionModel {
	m := &consoleweb.ContributionDecisionModel{Purpose: d.Stage, Role: d.Role, Agent: d.Agent, Charter: d.Charter, Plan: d.PlanID, PlanDigest: d.PlanDigest, Candidate: d.Candidate, Contract: d.ContractDigest, Status: d.Status, Expires: d.ExpiresAt.UTC().Format(time.RFC3339)}
	switch d.Status {
	case "unavailable":
		m.Message = "Protected decision delivery unavailable. No human approval request was delivered; this is not a human denial or pending approval. Restore the independently authenticated protected setup transport before continuing. Resume does not repeat this retained decision."
	case "denied":
		m.Message = "The independent human decision denied this exact provisioning purpose. No grant is inferred."
	case "requested":
		m.Message = "Exact decision retained; delivery and human approval are not established."
	case "delivered":
		m.Message = "Protected review was delivered; approval is not established."
	case "approved":
		m.Message = "Read canonical consumed approval and verified provisioning receipt before any role effect."
	case "expired":
		m.Message = "The independent decision expired; it grants no continuation authority."
	default:
		m.Message = "Retained protected decision outcome; no approval is inferred. Uncertain effects require canonical readback, not repeated consent."
	}
	if !now.Before(d.ExpiresAt) && d.Status != "approved" {
		m.Message += " The retained decision expiry has elapsed; its historical delivery status is unchanged."
	}
	return m
}
