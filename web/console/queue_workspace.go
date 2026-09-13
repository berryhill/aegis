package consoleweb

import (
	"fmt"
	"sort"
	"strings"

	"github.com/berryhill/aegis/internal/graph"
)

// QueuePinnedTopology is a presentation-only projection of the exact pinned
// Graph revision onto authoritative runtime state for one Queue item. It
// cannot validate, sequence, or admit execution. Array positions, never
// untrusted IDs, identify inspector panels.
//
// Positioning uses CSS-grid track placement (gridColumn/gridRow) rather than
// inline pixel styles. This keeps the rendered HTML compatible with the
// console's strict Content Security Policy (style-src 'self').
type QueuePinnedTopology struct {
	Nodes                 []QueueControlNodeModel
	Edges                 []QueueControlEdgeModel
	EdgeGeometry          []QueueEdgeLine
	Issues                []GraphIssueModel
	Columns               int
	Rows                  int
	CycleMembers          []string
	CycleMaxIterations    uint16
	CycleExitCondition    string
	CycleExhaustionToID   string
	ExhaustionDestination string
	FailureLocationNodeID string
	ReconstructionWarning string
}

// QueueEdgeLine projects the SVG geometry for one drawn edge.
type QueueEdgeLine struct {
	Index int
	Path  string
}

// buildQueuePinnedTopology projects the exact pinned Graph revision onto
// authoritative runtime state. It does not consult the current catalogue.
func buildQueuePinnedTopology(detail *QueueDetailModel) QueuePinnedTopology {
	t := QueuePinnedTopology{Columns: 1, Rows: 1}
	if detail == nil {
		return t
	}
	t.ReconstructionWarning = detail.ReconstructionWarn
	if len(detail.Nodes) > graph.MaxNodes || len(detail.Edges) > graph.MaxDependencies {
		t.Issues = append(t.Issues, GraphIssueModel{Code: "display.bound_exceeded", Path: "topology", Message: "Canvas omitted: pinned definition exceeds 128 nodes or 512 edges. Inspect the exact record; no partial topology is presented."})
		return t
	}
	if len(detail.Nodes) == 0 {
		t.Issues = append(t.Issues, GraphIssueModel{Code: "display.empty", Path: "nodes", Message: "No pinned Graph nodes resolved from the snapshot binding. The control-flow canvas remains blank."})
		return t
	}
	counts, indices, edgeCounts := map[string]int{}, map[string]int{}, map[string]int{}
	for i, n := range detail.Nodes {
		counts[n.NodeID]++
		indices[n.NodeID] = i
	}
	for _, e := range detail.Edges {
		edgeCounts[e.EdgeID]++
	}
	nodes := make([]QueueControlNodeModel, len(detail.Nodes))
	for i, n := range detail.Nodes {
		nodes[i] = n
		nodes[i].Index = i
	}
	adjacent := make([][]int, len(detail.Nodes))
	indegree, rank := make([]int, len(detail.Nodes)), make([]int, len(detail.Nodes))
	drawable := make([]bool, len(detail.Edges))
	for i, e := range detail.Edges {
		if e.EdgeID == "" || edgeCounts[e.EdgeID] != 1 || e.From == "" || e.To == "" || counts[e.From] != 1 || counts[e.To] != 1 || e.From == e.To {
			t.Issues = append(t.Issues, GraphIssueModel{Code: "display.unlinked_edge", Path: fmt.Sprintf("dependencies[%d]", i), Message: "Edge has empty, duplicate, missing, ambiguous or self-linked endpoints/identity; preserved as text, not drawn."})
			continue
		}
		a, b := indices[e.From], indices[e.To]
		adjacent[a] = append(adjacent[a], b)
		indegree[b]++
		drawable[i] = true
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
	cyclic := len(ready) != len(detail.Nodes)
	cycleIndices := map[int]bool{}
	if cyclic {
		t.Issues = append(t.Issues, GraphIssueModel{Code: "display.cycle", Path: "dependencies", Message: "Cycle detected: directed layout withheld. Nodes are indexed below; all declared edges remain in the textual equivalent."})
		cycleIndices = queueDetectFirstSCC(detail.Nodes, detail.Edges, indices)
		for i := range rank {
			if cycleIndices[i] {
				rank[i] = i % 4
			}
		}
	}
	rows := map[int]int{}
	maxCol := 0
	for i := range nodes {
		nodes[i].CycleMember = cycleIndices[i]
		nodes[i].Index = i
		col := rank[i] + 1
		row := rows[rank[i]] + 1
		rows[rank[i]]++
		if col > maxCol {
			maxCol = col
		}
		nodes[i].GridColumn = col
		nodes[i].GridRow = row
	}
	t.Columns = max(1, maxCol)
	t.Rows = 1
	for _, count := range rows {
		if count > t.Rows {
			t.Rows = count
		}
	}
	if cyclic && len(cycleIndices) > 0 {
		members, maxIter, exitCond, exhaustID := queueCycleAnalytics(detail.Nodes, detail.Edges, cycleIndices)
		t.CycleMembers = members
		t.CycleMaxIterations = maxIter
		t.CycleExitCondition = exitCond
		t.CycleExhaustionToID = exhaustID
		t.ExhaustionDestination = queueExhaustionDestination(detail.Nodes, exhaustID)
	}
	if detail.FailureLocation != "" {
		t.FailureLocationNodeID = detail.FailureLocation
	}
	t.Nodes = nodes
	t.Edges = detail.Edges
	if !cyclic {
		t.EdgeGeometry = queueLayoutEdges(nodes, detail.Edges, drawable, indices, rank, rows)
	}
	return t
}

// queueLayoutEdges produces SVG paths in the same coordinate system used by
// the Loop and Graph workspaces: 212px nodes, 32px gap, 32px padding.
func queueLayoutEdges(nodes []QueueControlNodeModel, edges []QueueControlEdgeModel, drawable []bool, indices map[string]int, rank []int, rows map[int]int) []QueueEdgeLine {
	_ = nodes
	_ = rows
	out := make([]QueueEdgeLine, 0, len(edges))
	for i, e := range edges {
		if !drawable[i] {
			continue
		}
		a, okA := indices[e.From]
		b, okB := indices[e.To]
		if !okA || !okB {
			continue
		}
		x1 := float64(rank[a]*212 + 32 + 212)
		y1 := float64(32 + 102/2)
		x2 := float64(rank[b]*212 + 32)
		y2 := float64(32 + 102/2)
		path := fmt.Sprintf("M %.2f %.2f C %.2f %.2f, %.2f %.2f, %.2f %.2f", x1, y1, x1+40, y1, x2-40, y2, x2, y2)
		if rank[b]-rank[a] > 1 {
			gutterY := float64(32) - 8
			path = fmt.Sprintf("M %.2f %.2f L %.2f %.2f L %.2f %.2f L %.2f %.2f L %.2f %.2f L %.2f %.2f", x1, y1, x1+28, y1, x1+28, gutterY, x2-28, gutterY, x2-28, y2, x2, y2)
		}
		out = append(out, QueueEdgeLine{Index: i, Path: path})
	}
	return out
}

// queueDetectFirstSCC returns the set of vertex indices that participate in
// the first non-trivial strongly connected component reachable in declared
// order. A self-loop counts as a non-trivial SCC. Ties are broken by smallest
// declared index.
func queueDetectFirstSCC(nodes []QueueControlNodeModel, edges []QueueControlEdgeModel, indexOf map[string]int) map[int]bool {
	adj := make([][]int, len(nodes))
	for _, e := range edges {
		a, okA := indexOf[e.From]
		b, okB := indexOf[e.To]
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
	color := make([]int, len(nodes))
	parent := make([]int, len(nodes))
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
	for v := range nodes {
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

// queueCycleAnalytics extracts bounded members, maximum traversal, exit
// condition, and exhaustion destination for one SCC.
func queueCycleAnalytics(nodes []QueueControlNodeModel, edges []QueueControlEdgeModel, cycleIndices map[int]bool) (members []string, maxIter uint16, exitCondition, exhaustID string) {
	ids := make([]int, 0, len(cycleIndices))
	for v := range cycleIndices {
		ids = append(ids, v)
	}
	sort.Ints(ids)
	for _, v := range ids {
		members = append(members, nodes[v].NodeID)
	}
	for _, e := range edges {
		a, okA := indexOfNode(nodes, e.From)
		b, okB := indexOfNode(nodes, e.To)
		if !okA || !okB {
			continue
		}
		_ = b
		if cycleIndices[a] && !cycleIndices[b] {
			if exitCondition == "" {
				exitCondition = "transition-out"
				exhaustID = e.To
			} else if exhaustID == "" {
				exhaustID = e.To
			}
		}
	}
	for _, e := range edges {
		if !strings.HasPrefix(e.Outcome, "max=") {
			continue
		}
		var iter uint16
		_, _ = fmt.Sscanf(e.Outcome, "max=%d", &iter)
		if iter > maxIter {
			maxIter = iter
		}
	}
	return members, maxIter, exitCondition, exhaustID
}

func indexOfNode(nodes []QueueControlNodeModel, id string) (int, bool) {
	for i, n := range nodes {
		if n.NodeID == id {
			return i, true
		}
	}
	return -1, false
}

func queueExhaustionDestination(nodes []QueueControlNodeModel, exitNodeID string) string {
	for _, n := range nodes {
		if n.NodeID == exitNodeID {
			if n.TerminalEligible {
				return n.NodeID + " · terminal-eligible"
			}
			return n.NodeID
		}
	}
	if exitNodeID != "" {
		return exitNodeID
	}
	return "No exhaustion destination declared"
}
