package managergateway

import (
	"context"
	"fmt"
	"strings"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
)

// DispatchDeterministicAegisTurn handles manager requests whose meaning and
// authority boundary do not depend on the conversational runtime. The caller
// must provide the already-authenticated subject for authoritative Registry
// reads. Guidance routes are explicitly non-mutating.
func DispatchDeterministicAegisTurn(ctx context.Context, service *app.Service, subject core.Subject, input string) (TurnResult, bool, error) {
	if intent := parseAgentRegistryIntent(input); intent.kind != "" {
		switch intent.kind {
		case "count", "list":
			agents, err := service.ListFleetAgentsAs(ctx, subject)
			if err != nil {
				return TurnResult{}, true, fmt.Errorf("Agent Registry read denied: %w", err)
			}
			ids := make([]string, 0, len(agents))
			for _, agent := range agents {
				ids = append(ids, agent.Registration.AgentID)
			}
			if intent.kind == "count" {
				return TurnResult{Kind: "agent_registry_count", Origin: TurnOriginAuthoritative, Message: fmt.Sprintf("The authenticated Aegis Agent Registry contains %d registered agent(s).", len(agents)), Data: map[string]any{"count": len(agents), "empty_registry_valid": true, "model_bypassed": true}}, true, nil
			}
			return TurnResult{Kind: "agent_registry_list", Origin: TurnOriginAuthoritative, Message: fmt.Sprintf("Registered agents (%d): %s", len(ids), strings.Join(ids, ", ")), Data: map[string]any{"agents": agents, "count": len(agents), "empty_registry_valid": true, "model_bypassed": true}}, true, nil
		case "show":
			agent, err := service.GetFleetAgentAs(ctx, subject, intent.agentID, intent.revision)
			if err != nil {
				return TurnResult{}, true, fmt.Errorf("Agent Registry exact readback denied: %w", err)
			}
			return TurnResult{Kind: "agent_registry_exact_readback", Origin: TurnOriginAuthoritative, Message: fmt.Sprintf("Agent %s revision %d is registered with lifecycle %s.", agent.Registration.AgentID, agent.Revision.Revision, agent.Revision.Lifecycle), Data: map[string]any{"agent": agent, "requested_revision": intent.revision, "latest_requested": !intent.hasRevision, "model_bypassed": true}}, true, nil
		}
	}
	if guidance := platformGuidanceIntent(input); guidance != "" {
		return platformGuidance(guidance), true, nil
	}
	route := detectAuthoritativeIntent(input)
	defer route.Wipe()
	switch route.kind {
	case intentAgentRegistration:
		return agentRegistrationGuidance(), true, nil
	case intentManagerLifecycle:
		return managerLifecycleGuidance(), true, nil
	default:
		return TurnResult{}, false, nil
	}
}
