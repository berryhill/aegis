package app

import (
	"github.com/berryhill/aegis/internal/disposition"
	"github.com/berryhill/aegis/internal/execution"
)

// QueueExecutionState describes the visual lifecycle phase derived from
// authoritative execution records. The constants mirror the lower-case string
// forms of execution.State so the projection layer can speak in strings
// without importing the execution package. Each constant is one durable
// outcome that the workspace must render distinctly.
type QueueExecutionState string

const (
	queueExecutionRequested QueueExecutionState = "requested"
	queueExecutionStarted   QueueExecutionState = "started"
	queueExecutionSucceeded QueueExecutionState = "succeeded"
	queueExecutionFailed    QueueExecutionState = "failed"
	queueExecutionDenied    QueueExecutionState = "denied"
	queueExecutionCancelled QueueExecutionState = "cancelled"
	queueExecutionExpired   QueueExecutionState = "expired"
	queueExecutionRevoked   QueueExecutionState = "revoked"
)

// String returns the lower-case string form of the state. It matches the
// execution.State string values used in canonical records.
func (s QueueExecutionState) String() string {
	return string(s)
}

// IsTerminal reports whether the visual state is a durable terminal outcome.
func (s QueueExecutionState) IsTerminal() bool {
	switch s {
	case queueExecutionSucceeded, queueExecutionFailed, queueExecutionDenied,
		queueExecutionCancelled, queueExecutionExpired, queueExecutionRevoked:
		return true
	}
	return false
}

// QueueExecutionStateFromExecution converts an execution.State to its
// presentation string counterpart. Unknown states map to the empty string
// so the projection layer treats them as "not yet recorded". It is exported
// so presentation-only callers do not need to import internal/execution.
func QueueExecutionStateFromExecution(s execution.State) QueueExecutionState {
	switch s {
	case execution.StateRequested:
		return queueExecutionRequested
	case execution.StateStarted:
		return queueExecutionStarted
	case execution.StateSucceeded:
		return queueExecutionSucceeded
	case execution.StateFailed:
		return queueExecutionFailed
	case execution.StateDenied:
		return queueExecutionDenied
	case execution.StateCancelled:
		return queueExecutionCancelled
	case execution.StateExpired:
		return queueExecutionExpired
	case execution.StateRevoked:
		return queueExecutionRevoked
	}
	return ""
}

// queueExecutionDispositionStateFromRecord converts a recorded disposition to
// its visual state. A nil disposition or a non-terminal disposition state maps
// to the empty string so the projection layer treats it as "not yet recorded".
func queueExecutionDispositionStateFromRecord(d *disposition.Record) QueueExecutionState {
	if d == nil {
		return ""
	}
	state := QueueExecutionStateFromExecution(d.State)
	if state.IsTerminal() {
		return state
	}
	return ""
}

// QueueExecutionView projection helpers. They translate authoritative
// execution records into presentation-only states the HTTP layer can render
// without re-importing internal/execution.

// QueueGraphRunState returns the visual state of the authoritative GraphRun.
func (v QueueExecutionView) QueueGraphRunState() QueueExecutionState {
	return QueueExecutionStateFromExecution(v.GraphRun.State)
}

// QueueLoopExecutionState returns the visual state recorded for one Graph
// node across all Loop executions, or empty when no record is found.
func (v QueueExecutionView) QueueLoopExecutionState(graphNodeID string) QueueExecutionState {
	for _, child := range v.LoopExecutions {
		if child.GraphNodeID == graphNodeID {
			return QueueExecutionStateFromExecution(child.State)
		}
	}
	return ""
}

// QueueLoopExecutionTaken reports whether at least one Loop execution for the
// given graph node recorded a started or terminal outcome (i.e. inbound edge
// was taken).
func (v QueueExecutionView) QueueLoopExecutionTaken(graphNodeID string) bool {
	state := v.QueueLoopExecutionState(graphNodeID)
	return state == queueExecutionStarted || state.IsTerminal()
}

// QueueCurrentControlNodeID returns the graph node ID currently holding
// control. It mirrors the dispatch rule that one Graph node is the authority
// current control at any moment. The empty string means no node currently
// holds control.
func (v QueueExecutionView) QueueCurrentControlNodeID() string {
	switch v.GraphRun.State {
	case execution.StateStarted:
		for _, attempt := range v.Attempts {
			if attempt.State != execution.StateStarted {
				continue
			}
			for _, child := range v.LoopExecutions {
				if child.LoopExecutionID == attempt.LoopExecutionID {
					return child.GraphNodeID
				}
			}
		}
		return ""
	case execution.StateRequested:
		for _, child := range v.LoopExecutions {
			if child.State == execution.StateRequested {
				return child.GraphNodeID
			}
		}
	}
	return ""
}

// QueueNodeAttemptState returns the visual state of the latest attempt for
// one graph node, or empty when no attempt was recorded.
func (v QueueExecutionView) QueueNodeAttemptState(graphNodeID string) QueueExecutionState {
	for _, child := range v.LoopExecutions {
		if child.GraphNodeID != graphNodeID {
			continue
		}
		var latest *execution.Attempt
		for i := range v.Attempts {
			attempt := v.Attempts[i]
			if attempt.LoopExecutionID != child.LoopExecutionID {
				continue
			}
			if latest == nil || attempt.AttemptNumber > latest.AttemptNumber {
				latest = &attempt
			}
		}
		if latest == nil {
			continue
		}
		return QueueExecutionStateFromExecution(latest.State)
	}
	return ""
}

// QueueNodeAttemptNumber returns the attempt number of the latest attempt
// for one graph node. The boolean reports whether a record was found.
func (v QueueExecutionView) QueueNodeAttemptNumber(graphNodeID string) (uint32, bool) {
	for _, child := range v.LoopExecutions {
		if child.GraphNodeID != graphNodeID {
			continue
		}
		var latest *execution.Attempt
		for i := range v.Attempts {
			attempt := v.Attempts[i]
			if attempt.LoopExecutionID != child.LoopExecutionID {
				continue
			}
			if latest == nil || attempt.AttemptNumber > latest.AttemptNumber {
				latest = &attempt
			}
		}
		if latest == nil {
			continue
		}
		return latest.AttemptNumber, true
	}
	return 0, false
}

// QueueTerminalNodeIDs returns the graph node IDs whose Loop execution
// matches the authoritative disposition's terminal state.
func (v QueueExecutionView) QueueTerminalNodeIDs() map[string]bool {
	terminal := v.QueueExecutionDispositionState()
	if terminal == "" {
		return map[string]bool{}
	}
	out := map[string]bool{}
	for _, child := range v.LoopExecutions {
		if QueueExecutionStateFromExecution(child.State) == terminal {
			out[child.GraphNodeID] = true
		}
	}
	return out
}

// QueueExecutionDispositionState returns the terminal visual state recorded
// in the authoritative disposition, or empty when no terminal disposition
// has been recorded.
func (v QueueExecutionView) QueueExecutionDispositionState() QueueExecutionState {
	return queueExecutionDispositionStateFromRecord(v.Disposition)
}

// QueueOutputCompleteness returns the canonical completeness label recorded
// against declared Graph outputs based on the disposition outcome:
// "complete" for succeeded, "unavailable" for other terminal outcomes, and
// "inapplicable" otherwise.
func (v QueueExecutionView) QueueOutputCompleteness() string {
	state := v.QueueExecutionDispositionState()
	switch state {
	case queueExecutionSucceeded:
		return "complete"
	}
	if state.IsTerminal() {
		return "unavailable"
	}
	return "inapplicable"
}
