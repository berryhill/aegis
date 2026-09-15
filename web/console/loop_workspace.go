package consoleweb

import (
	"fmt"
	"sort"
	"strings"

	"github.com/berryhill/aegis/internal/loop"
)

// loopGridPlacementCSS emits only bounded, application-owned integer rules.
// Served in the external stylesheet, these cover every admitted topology cell
// without inline styles, dynamic CSS values, or a JavaScript layout dependency.
func loopGridPlacementCSS() []byte {
	var css strings.Builder
	css.WriteByte('\n')
	for i := 1; i <= loop.MaxSteps; i++ {
		fmt.Fprintf(&css, ".loop-workspace .loop-node[data-grid-column=\"%d\"]{grid-column:%d}\n", i, i)
		fmt.Fprintf(&css, ".loop-workspace .loop-node[data-grid-row=\"%d\"]{grid-row:%d}\n", i, i)
	}
	return []byte(css.String())
}

// buildLoopTopology projects one Loop revision into a presentation-only
// topology. It cannot validate, sequence, or admit execution. Array positions,
// never untrusted IDs, identify inspector panels.
//
// Positioning uses CSS-grid track placement (gridColumn/gridRow) rather than
// inline pixel styles. This keeps the rendered HTML compatible with the
// console's strict Content Security Policy (style-src 'self').
func buildLoopTopology(detail *LoopDetailModel) LoopTopology {
	t := LoopTopology{Columns: 1, Rows: 1}
	if detail == nil {
		return t
	}
	if len(detail.Steps) > loop.MaxSteps || len(detail.Transitions) > loop.MaxTransitions {
		t.Issues = append(t.Issues, LoopIssueModel{Code: "display.bound_exceeded", Path: "topology", Message: "Canvas omitted: definition exceeds 256 steps or 1024 transitions. Inspect the exact record; no partial topology is presented."})
		return t
	}
	if len(detail.Steps) == 0 {
		t.Issues = append(t.Issues, LoopIssueModel{Code: "display.empty", Path: "steps", Message: "No steps declared; no executable topology is implied."})
		return t
	}
	counts, indices, edgeCounts := map[string]int{}, map[string]int{}, map[string]int{}
	for i, s := range detail.Steps {
		counts[s.ID]++
		indices[s.ID] = i
	}
	for _, e := range detail.Transitions {
		edgeCounts[e.ID]++
	}
	for i := range detail.Steps {
		step := detail.Steps[i]
		t.Nodes = append(t.Nodes, LoopPosition{Index: i, GridColumn: 1, GridRow: 1, Step: step, DisplayName: loopStepDisplayName(step)})
		if step.ID == "" || counts[step.ID] != 1 {
			t.Issues = append(t.Issues, LoopIssueModel{Code: "display.ambiguous_node", Path: fmt.Sprintf("steps[%d]", i), Message: "Empty or duplicate step ID; incident transitions are not drawn."})
		}
	}
	adjacent := make([][]int, len(detail.Steps))
	indegree, rank := make([]int, len(detail.Steps)), make([]int, len(detail.Steps))
	drawable := make([]bool, len(detail.Transitions))
	conditionFor := map[[2]int]string{}
	for i, e := range detail.Transitions {
		// Preserve all declared adjacency, including malformed edges, as text.
		for j, s := range detail.Steps {
			if e.FromStepID == s.ID {
				t.Nodes[j].Outgoing = append(t.Nodes[j].Outgoing, e)
				t.Nodes[j].OutDegree++
			}
			if e.ToStepID == s.ID {
				t.Nodes[j].Incoming = append(t.Nodes[j].Incoming, e)
				t.Nodes[j].InDegree++
			}
		}
		if e.ID == "" || edgeCounts[e.ID] != 1 || e.FromStepID == "" || e.ToStepID == "" || counts[e.FromStepID] != 1 || counts[e.ToStepID] != 1 || e.FromStepID == e.ToStepID {
			t.Issues = append(t.Issues, LoopIssueModel{Code: "display.unlinked_edge", Path: fmt.Sprintf("transitions[%d]", i), Message: "Transition has empty, duplicate, missing, ambiguous or self-linked endpoints/identity; preserved as text, not drawn."})
			continue
		}
		a, b := indices[e.FromStepID], indices[e.ToStepID]
		adjacent[a] = append(adjacent[a], b)
		indegree[b]++
		drawable[i] = true
		conditionFor[[2]int{a, b}] = e.Condition
	}
	ready := []int{}
	for i, degree := range indegree {
		if degree == 0 {
			ready = append(ready, i)
		}
	}
	for next := 0; next < len(ready); next++ {
		a := ready[next]
		for _, b := range adjacent[a] {
			rank[b] = max(rank[b], rank[a]+1)
			indegree[b]--
			if indegree[b] == 0 {
				ready = append(ready, b)
			}
		}
	}
	cyclic := len(ready) != len(detail.Steps)
	cycleIndices := map[int]bool{}
	if cyclic {
		t.Issues = append(t.Issues, LoopIssueModel{Code: "display.cycle", Path: "transitions", Message: "Cycle detected: directed layout withheld. Nodes are indexed below; all declared transitions remain in the textual equivalent."})
		cycleIndices = loopDetectFirstSCC(detail.Steps, detail.Transitions, indices)
		// Distribute cyclic nodes into the first four ranks so they remain
		// selectable from the textual equivalent.
		for i := range rank {
			if cycleIndices[i] {
				rank[i] = i % 4
			}
		}
	}
	// Group nodes by rank into rows. Each rank becomes a grid column; nodes
	// inside the same rank stack into successive rows.
	rows := map[int]int{}
	maxCol := 0
	for i := range t.Nodes {
		t.Nodes[i].GridColumn = rank[i] + 1
		t.Nodes[i].GridRow = rows[rank[i]] + 1
		rows[rank[i]]++
		if rank[i]+1 > maxCol {
			maxCol = rank[i] + 1
		}
	}
	t.Columns = max(1, maxCol)
	t.Rows = 1
	for _, count := range rows {
		if count > t.Rows {
			t.Rows = count
		}
	}
	// Cycle analytics: members, exit condition, exhaustion destination.
	if cyclic && len(cycleIndices) > 0 {
		members, maxIter, exitCond, exhaustID := loopCycleAnalytics(detail, cycleIndices, conditionFor)
		t.CycleMembers = members
		t.CycleMaxIterations = maxIter
		t.CycleExitCondition = exitCond
		t.CycleExhaustionToID = exhaustID
		t.ExhaustionDestination = loopExhaustionDestination(detail, exhaustID)
	}
	if !cyclic {
		// Track dimensions match the CSS grid in loop_workspace.css:
		// 212px nodes with 32px column gap, 102px nodes with 32px row gap,
		// and 32px padding around the stage. SVG paths use absolute pixel
		// coordinates inside a viewBox so the same coordinate system used
		// by the Graph workspace remains consistent. CSS-grid places the
		// buttons on the same tracks so SVG and HTML stay aligned without
		// inline styles.
		nodeW := 212
		nodeH := 102
		gap := 32
		pad := 32
		colStride := nodeW + gap
		rowStride := nodeH + gap
		for i, e := range detail.Transitions {
			if !drawable[i] {
				continue
			}
			a, b := t.Nodes[indices[e.FromStepID]], t.Nodes[indices[e.ToStepID]]
			x1 := float64((a.GridColumn-1)*colStride + pad + nodeW)
			y1 := float64((a.GridRow-1)*rowStride + pad + nodeH/2)
			x2 := float64((b.GridColumn-1)*colStride + pad)
			y2 := float64((b.GridRow-1)*rowStride + pad + nodeH/2)
			path := fmt.Sprintf("M %.2f %.2f C %.2f %.2f, %.2f %.2f, %.2f %.2f", x1, y1, x1+40, y1, x2-40, y2, x2, y2)
			if rank[indices[e.ToStepID]]-rank[indices[e.FromStepID]] > 1 {
				// Skip-rank edges travel in the gutter above subsequent rows so
				// they do not imply links to intermediate nodes. The gutter
				// sits a fixed pixel offset from the viewBox top so the
				// SVG geometry API can resolve the full path.
				gutterY := float64(pad) - 8
				path = fmt.Sprintf("M %.2f %.2f L %.2f %.2f L %.2f %.2f L %.2f %.2f L %.2f %.2f L %.2f %.2f", x1, y1, x1+28, y1, x1+28, gutterY, x2-28, gutterY, x2-28, y2, x2, y2)
			}
			t.Edges = append(t.Edges, LoopLine{Index: i, Path: path})
		}
	}
	return t
}

func loopStepDisplayName(step LoopStepModel) string {
	if step.DisplayName != "" {
		return step.DisplayName
	}
	if step.ID != "" {
		return step.ID
	}
	return "Unnamed step"
}

// loopDetectFirstSCC returns the set of vertex indices that participate in
// the first non-trivial strongly connected component reachable in declared
// order. A self-loop counts as a non-trivial SCC. The presentation never
// invents which loop a viewer sees: ties are broken by smallest declared
// index.
func loopDetectFirstSCC(steps []LoopStepModel, transitions []LoopTransitionModel, indexOf map[string]int) map[int]bool {
	adj := make([][]int, len(steps))
	for _, t := range transitions {
		a, okA := indexOf[t.FromStepID]
		b, okB := indexOf[t.ToStepID]
		if !okA || !okB {
			continue
		}
		adj[a] = append(adj[a], b)
	}
	for v := range adj {
		for _, w := range adj[v] {
			if w == v {
				return map[int]bool{v: true}
			}
		}
	}
	const (
		white, gray, black = 0, 1, 2
	)
	color := make([]int, len(steps))
	parent := make([]int, len(steps))
	for i := range parent {
		parent[i] = -1
	}
	var scc []int
	var dfs func(v int)
	dfs = func(v int) {
		color[v] = gray
		for _, w := range adj[v] {
			switch color[w] {
			case white:
				parent[w] = v
				dfs(w)
			case gray:
				// Found a back-edge: w is an ancestor of v.
				cycle := []int{}
				cur := v
				cycle = append(cycle, cur)
				for cur != w && parent[cur] != -1 {
					cur = parent[cur]
					cycle = append(cycle, cur)
				}
				if len(cycle) > 1 {
					scc = cycle
				}
			}
		}
		color[v] = black
	}
	for v := range steps {
		if color[v] == white {
			dfs(v)
			if len(scc) > 1 {
				break
			}
		}
	}
	out := map[int]bool{}
	for _, v := range scc {
		out[v] = true
	}
	return out
}

// loopCycleAnalytics extracts the bounded members, maximum traversal, exit
// condition, and exhaustion destination for one SCC. It preserves the first
// back-edge encountered in deterministic order so the presentation never
// invents which loop a viewer sees.
func loopCycleAnalytics(detail *LoopDetailModel, cycleIndices map[int]bool, conditionFor map[[2]int]string) (members []string, maxIter uint16, exitCondition, exhaustID string) {
	ids := make([]int, 0, len(cycleIndices))
	for v := range cycleIndices {
		ids = append(ids, v)
	}
	sort.Ints(ids)
	for _, v := range ids {
		members = append(members, detail.Steps[v].ID)
	}
	for _, t := range detail.Transitions {
		a, b, ok := loopLookupTransitionEndpoints(t, detail.Steps)
		if !ok {
			continue
		}
		if cycleIndices[a] && t.MaxTraversals > maxIter {
			maxIter = t.MaxTraversals
		}
		// A transition whose source is in the SCC but target is outside
		// is the cycle's exit edge.
		if cycleIndices[a] && !cycleIndices[b] {
			if cond := conditionFor[[2]int{a, b}]; cond != "" && exitCondition == "" {
				exitCondition = cond
				exhaustID = t.ToStepID
			} else if exhaustID == "" {
				exhaustID = t.ToStepID
			}
		}
	}
	return members, maxIter, exitCondition, exhaustID
}

func loopLookupTransitionEndpoints(t LoopTransitionModel, steps []LoopStepModel) (int, int, bool) {
	var a, b int = -1, -1
	for i, s := range steps {
		if s.ID == t.FromStepID {
			a = i
		}
		if s.ID == t.ToStepID {
			b = i
		}
	}
	return a, b, a >= 0 && b >= 0
}

func loopExhaustionDestination(detail *LoopDetailModel, exitStepID string) string {
	if detail == nil {
		return ""
	}
	if exitStepID != "" {
		for _, s := range detail.Steps {
			if s.ID == exitStepID {
				if s.TerminalOutcome != "" {
					return s.ID + " · terminal " + s.TerminalOutcome
				}
				return s.ID
			}
		}
		return exitStepID
	}
	for _, s := range detail.Steps {
		if s.TerminalOutcome != "" {
			return s.ID + " · terminal " + s.TerminalOutcome
		}
	}
	return "No terminal step declared"
}

// loopRoleSummary composes the headline row used by the page summary.
func loopRoleSummary(detail *LoopDetailModel) string {
	if detail == nil {
		return ""
	}
	parts := []string{}
	if len(detail.Steps) > 0 {
		parts = append(parts, fmt.Sprintf("%d step(s)", len(detail.Steps)))
	}
	if len(detail.Transitions) > 0 {
		parts = append(parts, fmt.Sprintf("%d transition(s)", len(detail.Transitions)))
	}
	if detail.CycleSummary != "" {
		parts = append(parts, detail.CycleSummary)
	}
	return strings.Join(parts, " · ")
}
