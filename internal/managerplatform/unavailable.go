package managerplatform

import "github.com/berryhill/aegis/internal/manager"

// Absence of an adapter is not absence of the backend feature. Foundational
// decisions and host/config effects retain their independent protected surfaces.
func unavailableCapabilities() []manager.PlatformCapability {
	rows := []struct{ name, service, reason string }{
		{"agents.approve-charter", "ApproveAgentCharterAs", "protected_principal_decision_required: conversation cannot approve foundational authority"},
		{"graphs.activate", "Graph lifecycle", "genuinely-unsupported: publication selects active revision atomically; no independent lifecycle mutation"},
		{"graphs.retire", "Graph lifecycle", "genuinely-unsupported: no canonical lifecycle mutation service"},
		{"queue.process", "ProcessQueueItemAs", "protected_authority_required: existing backend runtime control requires controller-issued authority and bounded worker identities; use loops.queue for supported exact Run"},
		{"queue.bind-runtime", "BindQueueRuntimeAs", "protected_authority_required: existing backend requires independent controller runtime-authority binding; use loops.queue"},
		{"queue.ordinary-retry", "RetryQueueItemAs", "genuinely-unsupported: durable runtime-stop acknowledgement absent"},
		{"queue.schedule", "QueueWorker", "genuinely-unsupported: no general scheduler"},
		{"charter.approve", "DecideApprovalAs", "protected_principal_decision_required: model or conversational confirmation is not foundational approval"},
		{"provision.apply", "ApplyAs", "protected_principal_decision_required: exact approved plan and independent approval required"},
		{"session.preview", "PreviewSessionAs", "protected_authority_required: preview issues and persists a mandate; not a non-authorizing read"},
		{"approval.decide", "DecideApprovalAs", "protected_principal_decision_required: exact approve/reject decision belongs to independent protected review"},
		{"design.smoke", "DesignSmokeAs", "protected_runtime_required: starts disposable external runtime; not a local template constructor"},
		{"session.start", "StartSessionAs", "unavailable-in-this-client: clean session and independently admitted mandate must not be inferred from conversation"},
		{"secret.create", "protected credential intake", "unavailable-in-platform-adapter: use existing protected terminal protocol; never include credential values in proposals"},
		{"secret.rotate", "protected credential intake", "unavailable-in-platform-adapter: protected no-echo intake required"},
		{"controller-codex.connect", "ApplyControllerCodexAs", "protected_principal_decision_required: provider/credential custody changes are not ordinary conversational effects"},
		{"gateway.install", "service unit owner", "unavailable-in-this-client: reviewed authenticated workstation service plan required"},
		{"update.apply", "release updater", "unavailable-in-this-client: executable replacement and service lifecycle require protected operator route"},
		{"reset", "state lifecycle", "unavailable-in-this-client: destructive maintenance is not a model operation"},
		{"deployment.project", "deployment projection", "genuinely-unsupported: deployment projection is a specification; no shipped CLI or API backend"},
		{"loops.mcp", "Loop MCP", "unavailable-in-this-client: no shipped MCP Loop adapter"},
	}
	result := make([]manager.PlatformCapability, 0, len(rows))
	for _, r := range rows {
		result = append(result, manager.PlatformCapability{Operation: r.name, Mutation: true, Service: r.service, Availability: "unavailable", Reason: r.reason, InputSchema: map[string]any{}})
	}
	return result
}
