package consoleweb

// BuildLoopExecutionTopology adapts content-free pinned steps and transitions
// to the definition canvas. Observations are decorations, never layout inputs.
func BuildLoopExecutionTopology(nodes []QueueControlNodeModel, edges []QueueControlEdgeModel) QueuePinnedTopology {
	d := &LoopDetailModel{}
	for _, n := range nodes {
		d.Steps = append(d.Steps, LoopStepModel{ID: n.NodeID})
	}
	for _, e := range edges {
		d.Transitions = append(d.Transitions, LoopTransitionModel{ID: e.EdgeID, FromStepID: e.From, ToStepID: e.To, Condition: e.Condition, MaxTraversals: e.Limit})
	}
	canonical := buildLoopTopology(d)
	t := QueuePinnedTopology{Columns: canonical.Columns, Rows: canonical.Rows, Nodes: append([]QueueControlNodeModel(nil), nodes...), Edges: edges, Loop: true, CycleMembers: canonical.CycleMembers, CycleMaxIterations: canonical.CycleMaxIterations, CycleExitCondition: canonical.CycleExitCondition, CycleExhaustionToID: canonical.CycleExhaustionToID, ExhaustionDestination: canonical.ExhaustionDestination}
	if len(canonical.Nodes) == 0 {
		t.Nodes = nil
	}
	for _, issue := range canonical.Issues {
		t.Issues = append(t.Issues, GraphIssueModel{Code: issue.Code, Path: issue.Path, Message: issue.Message})
	}
	for _, n := range canonical.Nodes {
		t.Nodes[n.Index].Index = n.Index
		t.Nodes[n.Index].GridColumn = n.GridColumn
		t.Nodes[n.Index].GridRow = n.GridRow
	}
	for _, e := range canonical.Edges {
		t.EdgeGeometry = append(t.EdgeGeometry, QueueEdgeLine{Index: e.Index, Path: e.Path, Back: e.Back, LabelX: e.LabelX, LabelY: e.LabelY, Label: loopTransitionLabel(d.Transitions[e.Index])})
	}
	return t
}

// QueueTopology is the unchanged Graph-specific presentation adapter.
func BuildDebuggerGraphTopology(detail *QueueDetailModel) QueuePinnedTopology {
	return buildQueuePinnedTopology(detail)
}
func queueTopologyHeight(t QueuePinnedTopology) int {
	if t.Loop {
		return t.Rows*134 + 72
	}
	return t.Rows*134 + 32
}
func queueGeometryClass(e QueueEdgeLine) string {
	if e.Back {
		return "queue-edge-path feedback"
	}
	return "queue-edge-path"
}
