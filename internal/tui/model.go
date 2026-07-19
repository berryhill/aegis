package tui

import "time"

type SecurityContext struct {
	Principal     string
	Stanza        string
	MandateID     string
	MandateState  string
	ExpiresAt     time.Time
	Runtime       string
	RuntimeState  string
	Route         string
	Model         string
	ModelDigest   string
	PolicyDigest  string
	Certification string
	Authority     string
	NoFallback    bool
}

type Component struct {
	Kind   EventKind
	Origin Origin
	At     time.Time
	Text   string
	Bytes  int
}

type State struct {
	Capabilities      Capabilities
	Security          SecurityContext
	Lifecycle         string
	Activity          string
	ActivitySince     time.Time
	Components        []Component
	ComponentBytes    int
	MaxComponents     int
	MaxComponentBytes int
	Closing           bool
	Failed            bool
}

func NewState(capabilities Capabilities, security SecurityContext) State {
	return State{Capabilities: capabilities, Security: security, Lifecycle: "created", MaxComponents: 256, MaxComponentBytes: 1 << 20}
}

func Update(state State, event Event) State {
	if !event.Valid() {
		state.Failed = true
		state.Lifecycle = "inconsistent"
		return state
	}
	if state.Closing && event.Kind != CleanupStage && event.Kind != CleanupCompleted && event.Kind != CleanupFailed && event.Kind != TerminalWarning && event.Kind != TerminalResize && event.Kind != TerminalCapabilitiesChanged {
		return state
	}
	if event.Security != nil && event.Origin == AegisAuthoritative {
		state.Security = *event.Security
	}
	switch event.Kind {
	case ManagerReady:
		state.Lifecycle = "active"
	case ManagerDegraded:
		state.Lifecycle = "degraded"
	case CleanupRequested:
		state.Lifecycle = "closing"
		state.Closing = true
	case CleanupStage:
		state.Lifecycle = "cleaning"
		state.Closing = true
	case CleanupCompleted:
		state.Lifecycle = "closed"
		state.Closing = true
		state.Activity = ""
	case CleanupFailed:
		state.Lifecycle = "failed"
		state.Closing = true
		state.Failed = true
		state.Activity = ""
	case TurnStarted, BootstrapStageStarted, OperationStarted, CommandAccepted, IntakeStarted, ApprovalRequested:
		state.Activity = event.Stage
		if state.Activity == "" {
			state.Activity = string(event.Kind)
		}
		state.ActivitySince = event.At
	case TurnCompleted, TurnFailed, TurnInterrupted, BootstrapStageComplete, BootstrapStageFailed, OperationCompleted, OperationFailed, CommandResult, CommandUnavailable, CommandDenied, CommandCancelled, IntakeCompleted, IntakeCancelled, IntakeFailed, ApprovalConfirmed, ApprovalCancelled, ApprovalExpired:
		state.Activity = ""
	}
	if event.Message != "" || event.Reason != "" {
		text := event.Message
		if text == "" {
			text = event.Reason
		}
		context := Prose
		if event.Origin == AegisAuthoritative {
			context = SecurityField
		}
		text = Sanitize(text, DefaultSanitizeOptions(context))
		component := Component{Kind: event.Kind, Origin: event.Origin, At: event.At, Text: text, Bytes: len(text)}
		state.Components = append(state.Components, component)
		state.ComponentBytes += component.Bytes
	}
	for len(state.Components) > state.MaxComponents || state.ComponentBytes > state.MaxComponentBytes {
		state.ComponentBytes -= state.Components[0].Bytes
		state.Components[0] = Component{}
		state.Components = state.Components[1:]
	}
	return state
}
