package tui

import "time"

type Origin string

const (
	AegisAuthoritative Origin = "aegis-authoritative"
	AegisDiagnostic    Origin = "aegis-diagnostic"
	RuntimeHermes      Origin = "runtime-hermes"
	ModelUntrusted     Origin = "model-untrusted"
	UserInput          Origin = "user-input"
)

type EventKind string

const (
	BootstrapInspectionStarted  EventKind = "bootstrap.inspection.started"
	BootstrapInspectionComplete EventKind = "bootstrap.inspection.completed"
	BootstrapStageStarted       EventKind = "bootstrap.stage.started"
	BootstrapStageComplete      EventKind = "bootstrap.stage.completed"
	BootstrapStageFailed        EventKind = "bootstrap.stage.failed"
	PrincipalAuthenticated      EventKind = "principal.authenticated"
	PrincipalDenied             EventKind = "principal.denied"
	TrustSelected               EventKind = "trust.selected"
	TrustDenied                 EventKind = "trust.denied"
	MandateIssued               EventKind = "mandate.issued"
	MandateExpiring             EventKind = "mandate.expiring"
	MandateExpired              EventKind = "mandate.expired"
	MandateRevoked              EventKind = "mandate.revoked"
	RuntimeDiscoveryStarted     EventKind = "runtime.discovery.started"
	RuntimeDiscoveryComplete    EventKind = "runtime.discovery.completed"
	RuntimeDiscoveryFailed      EventKind = "runtime.discovery.failed"
	ModelLoadStarted            EventKind = "model.load.started"
	ModelLoadComplete           EventKind = "model.load.completed"
	ModelLoadFailed             EventKind = "model.load.failed"
	ModelUnloadStarted          EventKind = "model.unload.started"
	ModelUnloadComplete         EventKind = "model.unload.completed"
	ModelUnloadFailed           EventKind = "model.unload.failed"
	ProxyOpened                 EventKind = "proxy.opened"
	ProxyClosed                 EventKind = "proxy.closed"
	ProxyDenied                 EventKind = "proxy.denied"
	HermesStarted               EventKind = "hermes.started"
	HermesStopped               EventKind = "hermes.stopped"
	HermesFailed                EventKind = "hermes.failed"
	ManagerReady                EventKind = "manager.ready"
	ManagerDegraded             EventKind = "manager.degraded"
	InputAccepted               EventKind = "input.accepted"
	InputBlocked                EventKind = "input.blocked"
	InputDiscarded              EventKind = "input.discarded"
	TurnQueued                  EventKind = "turn.queued"
	TurnStarted                 EventKind = "turn.started"
	TurnProgress                EventKind = "turn.progress"
	TurnCompleted               EventKind = "turn.completed"
	TurnFailed                  EventKind = "turn.failed"
	TurnInterrupted             EventKind = "turn.interrupted"
	AssistantDelta              EventKind = "assistant.delta"
	AssistantCompleted          EventKind = "assistant.completed"
	AssistantRejected           EventKind = "assistant.rejected"
	ProposalReceived            EventKind = "proposal.received"
	ProposalValidated           EventKind = "proposal.validated"
	ProposalDenied              EventKind = "proposal.denied"
	ApprovalRequested           EventKind = "approval.requested"
	ApprovalConfirmed           EventKind = "approval.confirmed"
	ApprovalCancelled           EventKind = "approval.cancelled"
	ApprovalExpired             EventKind = "approval.expired"
	IntakeRequested             EventKind = "intake.requested"
	IntakeStarted               EventKind = "intake.started"
	IntakeCompleted             EventKind = "intake.completed"
	IntakeCancelled             EventKind = "intake.cancelled"
	IntakeFailed                EventKind = "intake.failed"
	OperationStarted            EventKind = "operation.started"
	OperationCompleted          EventKind = "operation.completed"
	OperationFailed             EventKind = "operation.failed"
	CommandAccepted             EventKind = "command.accepted"
	CommandResult               EventKind = "command.result"
	CommandUnavailable          EventKind = "command.unavailable"
	CommandDenied               EventKind = "command.denied"
	CommandCancelled            EventKind = "command.cancelled"
	CleanupRequested            EventKind = "cleanup.requested"
	CleanupStage                EventKind = "cleanup.stage"
	CleanupCompleted            EventKind = "cleanup.completed"
	CleanupFailed               EventKind = "cleanup.failed"
	TerminalWarning             EventKind = "terminal.warning"
	TerminalResize              EventKind = "terminal.resize"
	TerminalCapabilitiesChanged EventKind = "terminal.capabilities.changed"
)

type Event struct {
	Kind     EventKind
	Origin   Origin
	At       time.Time
	Stage    string
	Message  string
	Reason   string
	Security *SecurityContext
	Fields   map[string]any
}

func (e Event) Valid() bool {
	if e.Kind == "" || e.Origin == "" {
		return false
	}
	if e.Origin == ModelUntrusted && authoritativeKind(e.Kind) {
		return false
	}
	return true
}

func authoritativeKind(kind EventKind) bool {
	switch kind {
	case PrincipalAuthenticated, PrincipalDenied, TrustSelected, TrustDenied, MandateIssued, MandateExpiring, MandateExpired, MandateRevoked, ProposalValidated, ProposalDenied, ApprovalRequested, ApprovalConfirmed, ApprovalCancelled, ApprovalExpired, OperationStarted, OperationCompleted, OperationFailed, CommandAccepted, CommandResult, CommandUnavailable, CommandDenied, CommandCancelled, CleanupRequested, CleanupStage, CleanupCompleted, CleanupFailed:
		return true
	}
	return false
}
