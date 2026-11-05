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
		if e.ID == "" || edgeCounts[e.ID] != 1 || e.FromStepID == "" || e.ToStepID == "" || counts[e.FromStepID] != 1 || counts[e.ToStepID] != 1 {
			t.Issues = append(t.Issues, LoopIssueModel{Code: "display.unlinked_edge", Path: fmt.Sprintf("transitions[%d]", i), Message: "Transition has empty, duplicate, missing, ambiguous endpoints/identity; preserved as text, not drawn."})
			continue
		}
		a, b := indices[e.FromStepID], indices[e.ToStepID]
		adjacent[a] = append(adjacent[a], b)
		indegree[b]++
		drawable[i] = true
	}
	// Classify feedback arcs in declared order. Removing only those arcs
	// leaves a DAG for placement; no stored transition is removed from the
	// drawing. A malformed or ambiguous edge never participates in layout.
	feedback := make([]bool, len(detail.Transitions))
	color := make([]uint8, len(detail.Steps))
	var visit func(int)
	visit = func(a int) {
		color[a] = 1
		for i, e := range detail.Transitions {
			if !drawable[i] || indices[e.FromStepID] != a {
				continue
			}
			b := indices[e.ToStepID]
			if color[b] == 1 {
				feedback[i] = true
			} else if color[b] == 0 {
				visit(b)
			}
		}
		color[a] = 2
	}
	for i := range detail.Steps {
		if color[i] == 0 {
			visit(i)
		}
	}
	adjacent = make([][]int, len(detail.Steps))
	indegree = make([]int, len(detail.Steps))
	for i, e := range detail.Transitions {
		if drawable[i] && !feedback[i] {
			a, b := indices[e.FromStepID], indices[e.ToStepID]
			adjacent[a] = append(adjacent[a], b)
			indegree[b]++
		}
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
	cyclic := false
	for _, back := range feedback {
		cyclic = cyclic || back
	}
	valid := make([]LoopTransitionModel, 0, len(detail.Transitions))
	if cyclic {
		t.Issues = append(t.Issues, LoopIssueModel{Code: "display.cycle", Path: "transitions", Message: "Bounded control-flow cycle: feedback transitions use the return gutter; all drawable stored transitions retain direction."})
		for i, edge := range detail.Transitions {
			if drawable[i] {
				valid = append(valid, edge)
			}
		}
	}
	for _, component := range loopStrongComponents(detail.Steps, valid, indices) {
		cycle := LoopCycleModel{}
		for i, step := range detail.Steps {
			if component[i] {
				cycle.Members = append(cycle.Members, step.ID)
			}
		}
		for _, edge := range valid {
			if component[indices[edge.FromStepID]] {
				cycle.Transitions = append(cycle.Transitions, edge)
				if !component[indices[edge.ToStepID]] {
					cycle.Exits = append(cycle.Exits, edge)
				}
			}
		}
		t.Cycles = append(t.Cycles, cycle)
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

	{
		// Track dimensions match the CSS grid in loop_workspace.css:
		// 212px nodes with 32px gaps. Reserve a 72px top gutter for the
		// feedback condition and ordinary edge labels on separate lanes.
		// SVG paths use absolute pixel
		// coordinates inside a viewBox so the same coordinate system used
		// by the Graph workspace remains consistent. CSS-grid places the
		// buttons on the same tracks so SVG and HTML stay aligned without
		// inline styles.
		nodeW := 212
		nodeH := 102
		gap := 32
		pad := 32
		padY := 72
		colStride := nodeW + gap
		rowStride := nodeH + gap
		parallel := map[[2]int]int{}
		for i, e := range detail.Transitions {
			if !drawable[i] {
				continue
			}
			from, to := indices[e.FromStepID], indices[e.ToStepID]
			a, b := t.Nodes[from], t.Nodes[to]
			pair := [2]int{from, to}
			laneIndex := parallel[pair]
			parallel[pair]++
			x1 := float64((a.GridColumn-1)*colStride + pad + nodeW)
			y1 := float64((a.GridRow-1)*rowStride + padY + nodeH/2)
			x2 := float64((b.GridColumn-1)*colStride + pad)
			y2 := float64((b.GridRow-1)*rowStride + padY + nodeH/2)
			if from == to {
				x := float64((a.GridColumn-1)*colStride + pad + nodeW/2)
				y := float64((a.GridRow-1)*rowStride + padY)
				path := fmt.Sprintf("M %.2f %.2f C %.2f %.2f, %.2f %.2f, %.2f %.2f", x-24, y, x-80, y-64, x+80, y-64, x+24, y)
				t.Edges = append(t.Edges, LoopLine{Index: i, Path: path, Back: true, LabelX: int(x), LabelY: int(y) - 48})
				continue
			}
			if feedback[i] {
				if a.GridRow == 1 && b.GridRow == 1 {
					// Return above the first row instead of crossing the
					// intervening nodes on a horizontal left-gutter route.
					// Both anchors are on their top borders.
					x1 = float64((a.GridColumn-1)*colStride + pad + nodeW/2)
					x2 = float64((b.GridColumn-1)*colStride + pad + nodeW/2)
					y1 = float64(padY)
					y2 = float64(padY)
					lane := float64(7 + (i%3)*3)
					path := fmt.Sprintf("M %.2f %.2f L %.2f %.2f L %.2f %.2f L %.2f %.2f", x1, y1, x1, lane, x2, lane, x2, y2)
					t.Edges = append(t.Edges, LoopLine{Index: i, Path: path, Back: true, LabelX: int((x1 + x2) / 2), LabelY: 28})
					continue
				}
				// Attach to left borders and travel through the reserved
				// viewport gutter. Index-based lanes distinguish parallel arcs.
				x1 = float64((a.GridColumn-1)*colStride + pad)
				lane := float64(8 + (i%3)*7)
				path := fmt.Sprintf("M %.2f %.2f L %.2f %.2f L %.2f %.2f L %.2f %.2f", x1, y1, lane, y1, lane, y2, x2, y2)
				t.Edges = append(t.Edges, LoopLine{Index: i, Path: path, Back: true, LabelX: int(lane) + 8, LabelY: int((y1 + y2) / 2)})
				continue
			}
			// Parallel edges retain border anchors and separate the first
			// three curves; the visible legend disambiguates larger bundles.
			laneOffset := float64(min(laneIndex, 2) * 14)
			path := fmt.Sprintf("M %.2f %.2f C %.2f %.2f, %.2f %.2f, %.2f %.2f", x1, y1, x1+40, y1+laneOffset, x2-40, y2+laneOffset, x2, y2)
			if rank[indices[e.ToStepID]]-rank[indices[e.FromStepID]] > 1 {
				// Skip-rank edges travel in the gutter above subsequent rows so
				// they do not imply links to intermediate nodes. The gutter
				// sits a fixed pixel offset from the viewBox top so the
				// SVG geometry API can resolve the full path.
				gutterY := float64(padY) - 28 + laneOffset
				path = fmt.Sprintf("M %.2f %.2f L %.2f %.2f L %.2f %.2f L %.2f %.2f L %.2f %.2f L %.2f %.2f", x1, y1, x1+28, y1, x1+28, gutterY, x2-28, gutterY, x2-28, y2, x2, y2)
			}
			labelX := int((x1 + x2) / 2)
			labelRow := max(a.GridRow, b.GridRow)
			if a.GridRow != b.GridRow && b.GridRow < a.GridRow {
				labelX += 70 // separate an upward branch from the lower-row exit
			}
			labelY := (labelRow-1)*rowStride + padY - 8
			t.Edges = append(t.Edges, LoopLine{Index: i, Path: path, LabelX: labelX, LabelY: labelY})
		}
	}
	return t
}

func loopEdgeClass(edge LoopLine) string {
	if edge.Back {
		return "loop-edge-path loop-edge-return"
	}
	return "loop-edge-path"
}

func loopTransitionLabel(t LoopTransitionModel) string {
	label := t.ID
	if t.Condition != "" {
		label = t.Condition
	}
	if t.MaxTraversals > 0 {
		label += fmt.Sprintf(" · max %d", t.MaxTraversals)
	}
	return label
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

func loopMissingStepFields(step LoopStepModel) []string {
	missing := []string{}
	for _, field := range []struct{ name, value string }{
		{"Display name", step.DisplayName}, {"Description", step.Description},
		{"Instruction or condition", step.Instruction}, {"Declared tool", step.Tool},
		{"Expected output", step.ExpectedOutput}, {"Failure behavior", step.FailureBehavior},
	} {
		if field.value == "" {
			missing = append(missing, field.name)
		}
	}
	if len(step.Capabilities) == 0 {
		missing = append(missing, "Declared capabilities")
	}
	if step.TimeoutSeconds == 0 {
		missing = append(missing, "Step timeout")
	}
	return missing
}

// loopStrongComponents computes complete cyclic SCCs in declared node order.
// Opaque condition labels do not imply exhaustion semantics or global budgets.
func loopStrongComponents(steps []LoopStepModel, transitions []LoopTransitionModel, indexOf map[string]int) []map[int]bool {
	adj := make([][]int, len(steps))
	self := map[int]bool{}
	for _, e := range transitions {
		a, okA := indexOf[e.FromStepID]
		b, okB := indexOf[e.ToStepID]
		if okA && okB {
			adj[a] = append(adj[a], b)
			if a == b {
				self[a] = true
			}
		}
	}
	next := 0
	indexes, low := make([]int, len(steps)), make([]int, len(steps))
	for i := range indexes {
		indexes[i] = -1
	}
	stack := []int{}
	active := make([]bool, len(steps))
	result := []map[int]bool{}
	var visit func(int)
	visit = func(v int) {
		indexes[v] = next
		low[v] = next
		next++
		stack = append(stack, v)
		active[v] = true
		for _, w := range adj[v] {
			if indexes[w] < 0 {
				visit(w)
				low[v] = min(low[v], low[w])
			} else if active[w] {
				low[v] = min(low[v], indexes[w])
			}
		}
		if low[v] == indexes[v] {
			component := map[int]bool{}
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				active[w] = false
				component[w] = true
				if w == v {
					break
				}
			}
			if len(component) > 1 || self[v] {
				result = append(result, component)
			}
		}
	}
	for v := range steps {
		if indexes[v] < 0 {
			visit(v)
		}
	}
	first := func(c map[int]bool) int {
		m := len(steps)
		for v := range c {
			m = min(m, v)
		}
		return m
	}
	sort.Slice(result, func(i, j int) bool { return first(result[i]) < first(result[j]) })
	return result
}

func loopDetectFirstSCC(steps []LoopStepModel, transitions []LoopTransitionModel, indexOf map[string]int) map[int]bool {
	components := loopStrongComponents(steps, transitions, indexOf)
	if len(components) > 0 {
		return components[0]
	}
	return map[int]bool{}
}

// loopTextualNodes is independent of canvas admission: stored steps and their
// adjacency remain inspectable even when bounded geometry is omitted.
func loopTextualNodes(detail *LoopDetailModel) []LoopPosition {
	nodes := make([]LoopPosition, 0, len(detail.Steps))
	for i, step := range detail.Steps {
		node := LoopPosition{Index: i, Step: step, DisplayName: loopStepDisplayName(step)}
		for _, e := range detail.Transitions {
			if e.FromStepID == step.ID {
				node.Outgoing = append(node.Outgoing, e)
			}
			if e.ToStepID == step.ID {
				node.Incoming = append(node.Incoming, e)
			}
		}
		nodes = append(nodes, node)
	}
	return nodes
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

func queueGridPlacementCSS() []byte {
	var css strings.Builder
	for i := 1; i <= loop.MaxSteps; i++ {
		fmt.Fprintf(&css, ".queue-workspace .queue-node[data-grid-column=\"%d\"]{grid-column:%d}\n", i, i)
		fmt.Fprintf(&css, ".queue-workspace .queue-node[data-grid-row=\"%d\"]{grid-row:%d}\n", i, i)
	}
	return []byte(css.String())
}
