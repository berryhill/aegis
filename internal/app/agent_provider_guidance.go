package app

import (
	"fmt"
	"strings"

	"github.com/berryhill/aegis/internal/core"
)

// These continuations carry no authority. Each generated input is admitted again
// by the owning service; rendering never reads profile or authentication bytes.
type AgentProviderChoice struct {
	CredentialID       string `json:"credential_id"`
	AccountFingerprint string `json:"account_fingerprint"`
	Command            string `json:"command"`
	CLI                string `json:"cli"`
}

func providerCLI(command string) string {
	if command == "" {
		return ""
	}
	return "aegis " + strings.TrimPrefix(command, "/")
}

func setAgentProviderNext(out *AgentProviderSetupResult) {
	prefix := "/agents providers " + out.Agent.Revision.AgentID
	out.Reason = "source_consent_required"
	out.Next = prefix + " setup default"
	switch {
	case out.Models.Provider == "none":
		out.Reason, out.Next = "model_read_consent_required", prefix+" models"
	case out.Models.Provider == "antigravity-oauth":
		out.Reason, out.Next = "antigravity_native_transport_unavailable", "/agents show "+out.Agent.Revision.AgentID
		out.Qualification = "unavailable"
	case out.Models.ProviderAuthentication != nil && out.Models.ProviderAuthentication.Mode == core.ControllerCodexAccessTokenV1:
		out.Reason, out.Next = "native_transport_qualification_required", "/agents show "+out.Agent.Revision.AgentID
	}
}

func renderAgentProviderSetup(out *AgentProviderSetupResult, configFile, origin string) {
	var b strings.Builder
	fmt.Fprintf(&b, "Agent %s, revision %d. Approved default model: %s / %s.\n", out.Agent.Revision.AgentID, out.Agent.Revision.Revision, out.Models.Provider, out.Models.Model)
	if out.Models.Provider == "none" {
		b.WriteString("Model decision: explicitly read the default profile's model configuration to prepare an exact charter review. No login is read or connected by models.\n")
	} else {
		b.WriteString("Omitted node/step selections use this immutable default; overrides must name an approved lane.\n")
	}
	if out.Connection != nil {
		c := out.Connection
		fmt.Fprintf(&b, "Source/account decision: %s; source %s; account %s; entry %s.\n", c.Status, c.ProfileHome, c.AccountID, c.CredentialID)
		if c.ReviewID != "" && c.Status != "connected" {
			out.Reason = "source_account_connection_consent_required"
			b.WriteString("Connect authorizes only this reviewed controller source/account; it does not approve an Agent charter.\n")
		}
	}
	if out.Review != nil {
		out.Reason = "exact_charter_approval_required"
		r := out.Review
		h := r.Charter.Charter.Stanzas[0].Hermes
		fmt.Fprintf(&b, "Separate exact-charter decision: revision %d, digest %s. Proposed default: %s / %s; authentication %s.\n", r.Charter.Charter.Revision, r.Charter.Digest, h.Provider, h.Model, h.ProviderAuthentication.Mode)
		for _, lane := range h.AllowedLanes {
			fmt.Fprintf(&b, "  Reviewed lane: %s / %s (%s)\n", lane.Provider, lane.Model, lane.ProviderAuthentication.Mode)
		}
		b.WriteString("Prepared only. Approve the exact digest below to append the successor; connection consent is not charter approval.\n")
	} else {
		switch out.Reason {
		case "source_consent_required":
			b.WriteString("Next source decision: setup default explicitly requests native source inspection for this approved default. A single eligible grant is previewed, never connected automatically; multiple grants require your exact choice.\n")
		case "explicit_account_selection_required":
			b.WriteString("Choose one eligible account/entry below. No account was selected or connected.\n")
		case "no_eligible_native_grant":
			b.WriteString("No eligible grant in the explicitly selected source; no login or replacement is attempted.\n")
		case "antigravity_native_transport_unavailable":
			b.WriteString("Antigravity execution is unavailable: no admitted native Antigravity transport. The configured default is preserved; no conversion to Gemini billing or Codex fallback.\n")
		case "connected_charter_review_unavailable":
			b.WriteString("Connection readback succeeded, but successor preparation was unavailable. Do not replay consumed connect consent; use setup again under fresh authority to prepare a new exact review.\n")
		}
	}
	for _, choice := range out.Choices {
		fmt.Fprintf(&b, "Account fingerprint %s; entry %s\n%s\n", choice.AccountFingerprint, choice.CredentialID, choice.Command)
	}
	fmt.Fprintf(&b, "Runtime qualification: %s. Executable: false; no session or inference was started.\n", out.Qualification)
	if out.Next != "" {
		fmt.Fprintf(&b, "Next input:\n%s", out.Next)
	}
	// Preserve the owning target in CLI continuations rather than requiring the
	// operator to reconstruct configuration/target flags at every decision.
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	cli := func(command string) string {
		if command == "" {
			return ""
		}
		return "aegis --config " + quote(configFile) + " --target " + quote(origin) + " " + strings.TrimPrefix(command, "/")
	}
	for i := range out.Choices {
		out.Choices[i].CLI = cli(out.Choices[i].Command)
	}
	out.NextCLI, out.Message = cli(out.Next), b.String()
}

// LocalHermesImportGuidance offers, but does not perform, a new scope decision.
func LocalHermesImportGuidance(p LocalHermesAgentImportProposal) string {
	message := fmt.Sprintf("Registration review: Agent %s. Proposed Registry lifecycle: %s. Enabled means Registry eligibility, not runtime readiness or authority. Exact registration digest %s.\nConfirm only this reviewed registration:\n%s", p.AgentID, p.Lifecycle, p.RevisionDigest, p.Confirmation)
	if p.ModelSnapshot == nil {
		message += "\nTo review the configured default model and allowed lanes instead, explicitly allow the bounded model-config read:\n/agents import hermes default with-models\nThis replaces the metadata-only proposal; it reads no login and connects no account."
	} else {
		message += fmt.Sprintf("\nModel decision: configured default %s / %s.", p.ModelSnapshot.DefaultProvider, p.ModelSnapshot.DefaultModel)
		for _, lane := range p.ModelSnapshot.Lanes {
			message += fmt.Sprintf("\nReviewed lane: %s / %s", lane.Provider, lane.Model)
		}
	}
	return message + "\nRegistration is not execution or provider authorization."
}

func LocalHermesImportContinuation(agent FleetAgent, models bool) (string, string) {
	next := "/agents providers " + agent.Revision.AgentID + " models"
	message := "Registered the exact Agent; no execution was activated. Next model decision: explicitly read the default profile's model configuration and review its exact successor charter."
	if models {
		next = "/agents providers " + agent.Revision.AgentID + " setup default"
		message = "Registered the exact configured default model and allowed lanes. Next source/account decision: setup default explicitly inspects the native source for that approved default; it previews but never connects a grant automatically. Unsupported defaults remain unavailable without fallback."
	}
	return next, message + "\nNext input:\n" + next
}
