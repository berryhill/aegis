package consoleweb

// actionExplanation is presentation only. The original admission result remains
// attached to the rendered diagnostic; none of these links authorize an action.
type actionExplanation struct {
	State, Heading, Message, URL, LinkLabel string
}

func explainAction(action ActionModel) actionExplanation {
	result := actionExplanation{State: action.State, Heading: stateHeading(action.State)}
	switch {
	case action.ReasonCode == "unknown_action" || action.State == "unsupported":
		result.State, result.Heading = "unsupported", "Operation not supported"
		result.Message = "This operation is not supported here. No Console destination or alternative authority is supplied; choose an existing supported operation instead."
	case action.State == "denied" && action.ReasonCode == "authority_context_required":
		result.State, result.Heading = "context_required", "Not evaluated for an exact action"
		result.Message = "Operation-specific authority has not been selected for this overview check. This does not mean initial setup is incomplete or that an exact action was denied. Preparing an action does not authorize it; submission still requires fresh admission."
		switch action.Key {
		case "loop_publish":
			result.URL, result.LinkLabel = "/console/loops/compose", "Prepare a Loop revision"
		case "graph_publish":
			result.URL, result.LinkLabel = "/console/graphs/compose", "Prepare a Graph revision"
		case "submission":
			result.URL, result.LinkLabel = "/console/graphs/run", "Prepare an execution request"
		default:
			result.Message += " No Console preparation destination is supported for this operation."
		}
	case action.ReasonCode == "credential_authority_ready":
		result.Heading = "Credential metadata available"
		result.Message = "The encrypted credential collection is readable, including when it is empty. This is not permission to create, use, or back up credentials. Each supported operation repeats independent credential authority checks."
		if action.Key == "prepare_credential" {
			result.URL, result.LinkLabel = "/console/credentials/operation?operation=create", "Prepare credential creation"
		} else {
			result.Message += " No Console backup preparation destination is supported."
		}
	case action.State == "locked":
		result.Heading = "Credential custody locked"
		result.Message = "Credential custody is configured but locked in the owning process. Use the configured protected custody unlock workflow with the controller operator; there is no Console unlock action. Do not initialize a replacement authority. This does not block credential-independent Registry, Loop, Graph, or Queue actions."
	case action.State == "unconfigured":
		result.Heading = "Credential custody not configured"
		result.Message = "No credential authority is configured. If this credential operation is needed, the controller operator must review the existing independent custody workflow: aegis --config OWNER_CONFIG secret initialize. Do not run it merely to inspect Registry, Loop, Graph, or Queue records."
	case action.State == "ready":
		result.Heading = "Ready for the evaluated context"
		result.Message = "The supplied context passed this readiness check only. This is not execution, an authority grant, or a promise of later admission; confirmation repeats fresh server-side checks."
	case action.ReasonCode == "agent_not_enabled":
		result.Heading = "Agent prerequisite not met"
		result.Message = "The evaluated Agent revision is not enabled. Inspect its exact lifecycle and authority before using supported lifecycle controls; this page changes nothing."
		result.URL, result.LinkLabel = "/console/agents", "Inspect Agent Registry"
	case action.State == "empty" && action.ReasonCode == "definition_not_found":
		result.Heading = "Required definition not found"
		result.Message = "The exact definition requested by this action was not found. This does not mean the service is unavailable. Inspect published records before preparing another action."
		for _, repair := range action.RepairActions {
			switch repair {
			case "register_fleet_agent":
				result.URL, result.LinkLabel = "/console/agents", "Inspect Agent Registry"
			case "publish_exact_loop_revision":
				result.URL, result.LinkLabel = "/console/loops", "Inspect Loops"
			case "publish_exact_graph_revision":
				result.URL, result.LinkLabel = "/console/graphs", "Inspect Graphs"
			}
		}
	case action.State == "denied":
		result.Message = "The evaluated action was denied. This result grants no authority and does not establish missing credentials or incomplete installation. No automatic repair or authority renewal is available here."
		switch action.ReasonCode {
		case "authority_inactive_or_inconsistent":
			result.Message = "The evaluated authority is inactive or inconsistent. The check does not distinguish expiry, revocation, or a binding inconsistency. No authority is renewed here; use independently authenticated authority review outside this overview."
		case "authority_expired", "mandate_expired":
			result.Heading, result.Message = "Expired authority", "The evaluated authority has expired. Browsing does not renew it. A new independently authenticated authority decision is required outside this overview."
		case "authority_revoked", "mandate_revoked":
			result.Heading, result.Message = "Revoked authority", "The evaluated authority was revoked. Refreshing does not restore it. A new independently authenticated authority decision is required outside this overview."
		}
	case action.State == "degraded_repair_required":
		result.Message = "This action cannot be evaluated safely because authoritative state requires repair. No count, permission, or automatic recovery is asserted. Recovery is not supported from this panel."
	default:
		result.Heading = "Readiness unavailable"
		result.Message = "This action could not be evaluated or is not supported here. Unavailable is not denied, ready, or an empty collection. No supported Console repair destination is known."
	}
	return result
}
