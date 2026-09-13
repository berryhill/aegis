package app

import (
	"testing"

	"github.com/berryhill/aegis/internal/disposition"
	"github.com/berryhill/aegis/internal/execution"
)

func TestQueueExecutionStateFromExecution_MappingAndTerminal(t *testing.T) {
	cases := []struct {
		name string
		in   execution.State
		want QueueExecutionState
	}{
		{"requested", execution.StateRequested, "requested"},
		{"started", execution.StateStarted, "started"},
		{"succeeded", execution.StateSucceeded, "succeeded"},
		{"failed", execution.StateFailed, "failed"},
		{"denied", execution.StateDenied, "denied"},
		{"cancelled", execution.StateCancelled, "cancelled"},
		{"expired", execution.StateExpired, "expired"},
		{"revoked", execution.StateRevoked, "revoked"},
		{"unknown", execution.State(""), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := QueueExecutionStateFromExecution(c.in); got != c.want {
				t.Fatalf("state=%q want=%q got=%q", c.in, c.want, got)
			}
			if c.want != "" {
				if got := c.want.IsTerminal(); got != terminalOf(c.want) {
					t.Fatalf("IsTerminal mismatch for %q: got=%v want=%v", c.want, got, terminalOf(c.want))
				}
			}
		})
	}
}

func terminalOf(s QueueExecutionState) bool {
	switch s {
	case "succeeded", "failed", "denied", "cancelled", "expired", "revoked":
		return true
	}
	return false
}

func TestQueueExecutionView_DispositionCompletenessAndTerminalNodes(t *testing.T) {
	succeeded := disposition.Record{State: execution.StateSucceeded}
	failed := disposition.Record{State: execution.StateFailed}
	view := QueueExecutionView{
		LoopExecutions: []execution.LoopExecution{
			{GraphNodeID: "v1", State: execution.StateSucceeded, LoopExecutionID: "le-1"},
			{GraphNodeID: "v2", State: execution.StateFailed, LoopExecutionID: "le-2"},
			{GraphNodeID: "v3", State: execution.StateStarted, LoopExecutionID: "le-3"},
		},
	}
	cases := []struct {
		name    string
		record  *disposition.Record
		compl   string
		termIDs []string
	}{
		{"succeeded", &succeeded, "complete", []string{"v1"}},
		{"failed", &failed, "unavailable", []string{"v2"}},
		{"nil", nil, "inapplicable", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			view.Disposition = c.record
			if got := view.QueueOutputCompleteness(); got != c.compl {
				t.Fatalf("QueueOutputCompleteness: got=%q want=%q", got, c.compl)
			}
			term := view.QueueTerminalNodeIDs()
			if len(term) != len(c.termIDs) {
				t.Fatalf("QueueTerminalNodeIDs size: got=%d want=%d (%v)", len(term), len(c.termIDs), term)
			}
			for _, id := range c.termIDs {
				if !term[id] {
					t.Fatalf("expected node %q in terminal set: %v", id, term)
				}
			}
		})
	}
}

func TestQueueExecutionView_CurrentControlAndAttempt(t *testing.T) {
	view := QueueExecutionView{
		GraphRun: execution.GraphRun{State: execution.StateStarted},
		LoopExecutions: []execution.LoopExecution{
			{GraphNodeID: "v1", LoopExecutionID: "le-1", State: execution.StateSucceeded},
			{GraphNodeID: "v2", LoopExecutionID: "le-2", State: execution.StateStarted},
			{GraphNodeID: "v3", LoopExecutionID: "le-3", State: execution.StateRequested},
		},
		Attempts: []execution.Attempt{
			{LoopExecutionID: "le-1", AttemptNumber: 1, State: execution.StateSucceeded},
			{LoopExecutionID: "le-2", AttemptNumber: 2, State: execution.StateStarted},
		},
	}
	if got := view.QueueCurrentControlNodeID(); got != "v2" {
		t.Fatalf("current control: got=%q want=v2", got)
	}
	num, ok := view.QueueNodeAttemptNumber("v2")
	if !ok || num != 2 {
		t.Fatalf("attempt number for v2: got=%d ok=%v want=2 true", num, ok)
	}
	if state := view.QueueNodeAttemptState("v2"); state != "started" {
		t.Fatalf("attempt state for v2: got=%q want=started", state)
	}
	if state := view.QueueLoopExecutionState("v1"); state != "succeeded" {
		t.Fatalf("loop execution state for v1: got=%q want=succeeded", state)
	}
	if taken := view.QueueLoopExecutionTaken("v1"); !taken {
		t.Fatalf("expected taken edge for v1")
	}
	if taken := view.QueueLoopExecutionTaken("v3"); taken {
		t.Fatalf("expected not-taken edge for v3")
	}
}
