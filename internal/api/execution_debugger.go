package api

import (
	"encoding/json"
	"github.com/berryhill/aegis/internal/app"
	consoleweb "github.com/berryhill/aegis/web/console"
)

func applyQueueDebugger(detail *consoleweb.QueueDetailModel, d app.ExecutionDebugger) {
	wire, _ := json.Marshal(d)
	detail.DebuggerJSON = string(wire)
	detail.Nodes = nil
	detail.Edges = nil
	detail.ReconstructionWarn = ""
	if d.Direct && len(d.Nodes) == 1 {
		detail.LiveLoop = true
		n := d.Nodes[0]
		for i, s := range n.Steps {
			state := "not observed"
			current := false
			for _, a := range n.Attempts {
				for _, f := range a.Facts {
					if f.StepID != s.ID {
						continue
					}
					switch f.Kind {
					case "step_start":
						state = "running"
						current = true
					case "step_finish":
						state = f.Outcome
						current = false
					case "verification":
						if f.Outcome == "failed" {
							state = "failed"
						}
					}
				}
			}
			detail.Nodes = append(detail.Nodes, consoleweb.QueueControlNodeModel{Index: i, NodeID: s.ID, State: state, ExecutionState: "Interpreter observation · not Queue disposition", Current: current, TerminalEligible: s.Terminal != ""})
		}
		for i, e := range n.Edges {
			outcome := "pending"
			for _, a := range n.Attempts {
				for _, f := range a.Facts {
					if f.Kind == "transition" && f.TransitionID == e.ID {
						outcome = "taken"
					}
				}
			}
			detail.Edges = append(detail.Edges, consoleweb.QueueControlEdgeModel{Index: i, EdgeID: e.ID, From: e.From, To: e.To, Outcome: outcome})
		}
	} else {
		for i, n := range d.Nodes {
			state := "not observed"
			if len(n.Attempts) > 0 {
				state = "attempt recorded"
			}
			detail.Nodes = append(detail.Nodes, consoleweb.QueueControlNodeModel{Index: i, NodeID: n.NodeID, State: state, ExecutionState: n.ExecutionID})
		}
		for i, e := range d.Dependencies {
			detail.Edges = append(detail.Edges, consoleweb.QueueControlEdgeModel{Index: i, EdgeID: e.ID, From: e.From, To: e.To, Outcome: "pending"})
		}
	}
	detail.NodeCount = len(detail.Nodes)
	detail.EdgeCount = len(detail.Edges)
}
