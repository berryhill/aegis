package manager

import (
	"errors"
	"strings"
)

const (
	ExpertiseSchemaVersion = "aegis.manager.expertise.v1"
	ExpertiseVersion       = "aegis.platform.expertise.v2"
)

const platformExpertiseContent = `AEGIS PLATFORM EXPERTISE:
- The canonical built-in Aegis Agent is a durable, registered Aegis platform participant. Its identity is separate from Hermes profile state. A discovered or default Hermes profile is runtime provenance only; it is never proof of Agent registration and is not the built-in Agent.
- Each manager session runs in a unique disposable Aegis-owned HERMES_HOME with no ambient profile, configuration, memory, skills, plugins, MCP, or credentials. Do not infer capabilities or identity from the operator's normal Hermes home.
- The Agent Registry permanently binds a stable Agent ID and fleet-source identity to revision 1. Agent state is recorded as create-only immutable, digest-bound revisions with explicit runtime binding, lifecycle, ownership and accountability, charter, declared capabilities, and policy references; mutable selection never rewrites revision history.
- A Loop is immutable typed control flow. Publishing one exact validated Loop revision requires immutable provenance from an exact enabled Agent revision and admitted Aegis authority. A Graph is immutable coordination data whose nodes bind exact Agent and Loop revisions; a Graph definition or run snapshot carries no authority and cannot enqueue work by itself.
- Admission accepts a typed submission only after exact references, policy, authority, lifecycle, and readiness checks. The durable result is a durable rejection or exactly one durable queue item. A rejection cannot be converted in place. Queue claim is an atomic single-winner claim with a bounded lease, and possession of a claim is not runtime admission.
- Execution records each retry as a distinct bounded attempt. Runtime output is evidence, not authorization or completion: content-addressed artifacts and claim-specific verification receipts bind the attempt, run, owner, action, and authority context. Verification pass/fail and the distinct terminal disposition are separate facts; terminal outcomes include succeeded, failed, denied, cancelled, expired, and revoked.
- Credentials and capability declarations are separate from Agent registration. A declaration does not grant a credential, authority, or runtime admission. Credential values must never enter model prompts; creation requires Aegis authorization, confirmation, and protected intake, and unavailable protected intake means no mutation.
- The model may propose explanations or candidate actions but never authorizes, admits, claims, verifies, or completes work. Only authenticated deterministic Aegis paths may mutate state. Do not claim natural-language mutation or general orchestration support.
- Deterministic Agent controls are /agents readiness, /agents list, /agents show <agent-id> [revision], and the authenticated digest-confirmed /agents prepare then /agents register transaction. Use /help agents for exact grammar. Agent Registry reads and mutations remain subject to parser, lifecycle, readiness, scope, policy, and authority checks.`

type ExpertiseProjection struct {
	SchemaVersion string `json:"schema_version"`
	Version       string `json:"version"`
	Digest        string `json:"digest"`
	Content       string `json:"content"`
}

func PlatformExpertise() ExpertiseProjection {
	return ExpertiseProjection{
		SchemaVersion: ExpertiseSchemaVersion,
		Version:       ExpertiseVersion,
		Digest:        digestString(ExpertiseVersion + "\n" + platformExpertiseContent),
		Content:       platformExpertiseContent,
	}
}

func (p ExpertiseProjection) Validate() error {
	if p.SchemaVersion != ExpertiseSchemaVersion || p.Version != ExpertiseVersion || strings.TrimSpace(p.Content) == "" {
		return errors.New("manager expertise projection is incomplete")
	}
	if p.Digest != digestString(p.Version+"\n"+p.Content) {
		return errors.New("manager expertise projection digest mismatch")
	}
	return nil
}

func ManagerSystemInstruction() string {
	projection := PlatformExpertise()
	return SystemInstruction + "\n\nEXPERTISE PROJECTION: " + projection.Version + "\nEXPERTISE DIGEST: " + projection.Digest + "\n" + projection.Content
}
