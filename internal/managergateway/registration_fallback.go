package managergateway

// RegistrationFallback is only for a missing conversational runtime. Normal
// registration language must reach the general proposal/input-collection path.
func RegistrationFallback(input string) (TurnResult, bool) {
	route := detectAuthoritativeIntent(input)
	defer route.Wipe()
	if route.kind == intentAgentRegistration {
		return agentRegistrationGuidance(), true
	}
	return TurnResult{}, false
}
