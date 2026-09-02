package managergateway

import (
	"regexp"
	"strings"

	managerdomain "github.com/berryhill/aegis/internal/manager"
)

const (
	intentAgentRegistration = "agent_registration"
	intentCredentialCreate  = "credential_creation"
	intentCredentialIntake  = "credential_intake"
)

type authoritativeRoute struct {
	kind             string
	credential       managerdomain.CreateIntent
	credentialParsed bool
}

func (route *authoritativeRoute) Wipe() {
	if route.credentialParsed {
		route.credential.Wipe()
	}
}

// detectAuthoritativeIntent parses credential creation exactly once. Credential
// parsing runs before normalization so an inline value is never copied into a
// lower-cased routing string, and unrecognized credential-bearing syntax fails
// closed instead of reaching the conversational runtime.
func detectAuthoritativeIntent(input string) authoritativeRoute {
	candidate := input
	if body, polite := politeRequestBody(input); polite && authoritativeRequestAction(body) {
		candidate = body
	}
	if intent, ok := managerdomain.ParseCreateIntent(candidate); ok {
		return authoritativeRoute{kind: intentCredentialCreate, credential: intent, credentialParsed: true}
	}
	if managerdomain.ContainsInlineCredentialValue(input) || managerdomain.ContainsDetectedCredentialMaterial(input) {
		return authoritativeRoute{kind: intentCredentialIntake}
	}

	if strings.ContainsAny(candidate, "\r\n") {
		return authoritativeRoute{}
	}
	normalized := normalizedIntent(candidate)
	if normalized == "" || conversationalQuestion(normalized) {
		return authoritativeRoute{}
	}
	if agentRegistrationGuidancePattern.MatchString(normalized) {
		return authoritativeRoute{kind: intentAgentRegistration}
	}
	return authoritativeRoute{}
}

var agentRegistrationGuidancePattern = regexp.MustCompile(`^(?:please )?(?:register|add) (?:(?:an?|the|our|another|new) )?agents?(?: [a-z0-9][a-z0-9._-]{0,63})?(?: for me)?$`)

func authoritativeIntent(input string) string {
	route := detectAuthoritativeIntent(input)
	defer route.Wipe()
	return route.kind
}

func politeRequestBody(input string) (string, bool) {
	trimmed := strings.TrimSpace(input)
	for _, prefix := range []string{"can you ", "could you ", "would you "} {
		if len(trimmed) >= len(prefix) && strings.EqualFold(trimmed[:len(prefix)], prefix) {
			return strings.TrimSpace(trimmed[len(prefix):]), true
		}
	}
	return input, false
}

func authoritativeRequestAction(input string) bool {
	trimmed := strings.TrimSpace(input)
	for _, action := range []string{"register", "add", "create", "make", "store", "save", "stash", "remember", "keep"} {
		if len(trimmed) == len(action) && strings.EqualFold(trimmed, action) ||
			len(trimmed) > len(action) && strings.EqualFold(trimmed[:len(action)], action) && (trimmed[len(action)] == ' ' || trimmed[len(action)] == '\t') {
			return true
		}
	}
	return false
}

func conversationalQuestion(normalized string) bool {
	for _, prefix := range []string{"how ", "what ", "why ", "when ", "where ", "can ", "could ", "should ", "would ", "do ", "does ", "is ", "are "} {
		if strings.HasPrefix(normalized, prefix) {
			return true
		}
	}
	return false
}

func expertiseData() map[string]any {
	projection := managerdomain.PlatformExpertise()
	return map[string]any{
		"expertise_schema_version": projection.SchemaVersion,
		"expertise_version":        projection.Version,
		"expertise_digest":         projection.Digest,
		"model_bypassed":           true,
	}
}

func agentRegistrationGuidance() TurnResult {
	data := expertiseData()
	data["registered"] = false
	data["readiness_command"] = "/agents readiness"
	data["grammar_command"] = "/help agents"
	return TurnResult{
		Kind:   "agent_registration_guidance",
		Origin: TurnOriginAuthoritative,
		Message: "Registration was not performed. A Hermes profile, including the default profile, is runtime provenance and not Agent registration, identity, or authority. " +
			"Inspect authoritative Registry readiness with /agents readiness. Registration requires an exact charter fixture and fleet/source binding through the authenticated /agents prepare transaction, followed by /agents register with the exact prepared revision digest.",
		Data: data,
	}
}

func platformGuidance(kind string) TurnResult {
	data := map[string]any{
		"guidance_version": "aegis.platform.guidance.v1",
		"model_bypassed":   true,
	}
	switch kind {
	case "skills_install":
		data["scope"] = "selected Hermes profile"
		data["mutated"] = false
		return TurnResult{Kind: "aegis_skills_installation_guidance", Origin: TurnOriginAuthoritative, Data: data, Message: "Install the advisory Aegis routing skill into the explicitly selected Hermes profile with:\nhermes skills tap add berryhill/aegis\nhermes skills search aegis\nhermes skills inspect berryhill/aegis/skills/aegis\nhermes skills install berryhill/aegis/skills/aegis --yes\nhermes skills list\nhermes skills audit aegis\nInstall another bundled skill with hermes skills install berryhill/aegis/skills/<skill-slug> --yes. This changes only that selected Hermes profile; it does not register an Agent, grant Aegis authority, or install skills into the manager's disposable runtime."}
	case "registered_agent_expertise":
		data["registration_imports_profile_contents"] = false
		return TurnResult{Kind: "registered_agent_expertise_guidance", Origin: TurnOriginAuthoritative, Data: data, Message: "A registered imported Hermes Agent is an immutable Aegis Registry record with runtime provenance; registration does not import the profile's skills, prompts, memories, plugins, credentials, model binding, or authority. The Aegis manager receives a separate versioned, digest-bound platform expertise projection. Check skills installed in a normal Hermes profile with hermes skills list under that profile's HERMES_HOME."}
	case "agent_rename":
		data["renamed"] = false
		return TurnResult{Kind: "agent_rename_guidance", Origin: TurnOriginAuthoritative, Data: data, Message: "The Agent was not renamed. Its canonical Agent ID is immutable because identity, provenance, authorization, execution history, and audit bind to it. A separate authenticated display-name or alias operation is not shipped yet; principal authentication does not make an unsupported identity mutation safe. Registration and history remain unchanged."}
	default:
		return TurnResult{}
	}
}

func credentialCreationGuidance(intent managerdomain.CreateIntent, parsed bool) TurnResult {
	data := expertiseData()
	data["created"] = false
	data["protected_intake_available"] = false
	if parsed && intent.Arguments.Reference != "" {
		data["reference"] = intent.Arguments.Reference
	}
	return TurnResult{
		Kind:   "credential_creation_guidance",
		Origin: TurnOriginAuthoritative,
		Message: "Credential creation was not performed. Aegis requires authorization, confirmation, and protected no-echo intake; protected no-echo intake is unavailable through the gateway turn endpoint. " +
			"No supplied credential value was sent to the model or retained in this response. Use an Aegis-owned interactive terminal boundary that supports protected intake.",
		Data: data,
	}
}

func credentialIntakeGuidance() TurnResult {
	data := expertiseData()
	data["created"] = false
	data["protected_intake_available"] = false
	data["detected_credential_material"] = true
	return TurnResult{
		Kind:    "credential_intake_required",
		Origin:  TurnOriginAuthoritative,
		Message: "Detected credential material was not sent to the model or retained in this response. Credential values are accepted only through an Aegis-owned protected no-echo intake boundary, which is unavailable through the gateway turn endpoint.",
		Data:    data,
	}
}
