package consoleweb

import (
	"fmt"

	"github.com/berryhill/aegis/internal/graph"
)

// GraphTopology is a presentation-only projection. It cannot validate or admit
// execution. Array positions, never untrusted IDs, identify inspector panels.
//
// Positioning uses CSS-grid track placement (gridColumn/gridRow) rather than
// inline pixel styles. This keeps the rendered HTML compatible with the
// console's strict Content Security Policy (style-src 'self').
type GraphTopology struct {
	Nodes     []GraphPosition
	Edges     []GraphLine
	Issues    []GraphIssueModel
	Columns   int
	Rows      int
	LoopCount int
}

type GraphPosition struct {
	Index              int
	GridColumn         int
	GridRow            int
	Node               GraphNodeModel
	Incoming, Outgoing []GraphEdgeModel
}

type GraphLine struct {
	Index int
	Path  string
}

func buildGraphTopology(g *GraphDetailModel) GraphTopology {
	t := GraphTopology{Columns: 1, Rows: 1}
	if g == nil {
		return t
	}
	loops := map[string]bool{}
	for _, node := range g.Nodes {
		if node.Loop != "" {
			loops[node.Loop] = true
		}
	}
	t.LoopCount = len(loops)
	if len(g.Nodes) > graph.MaxNodes || len(g.Edges) > graph.MaxDependencies {
		t.Issues = append(t.Issues, GraphIssueModel{"display.bound_exceeded", "topology", "Canvas omitted: definition exceeds 128 nodes or 512 edges. Inspect the exact record; no partial topology is presented."})
		return t
	}
	if len(g.Nodes) == 0 {
		t.Issues = append(t.Issues, GraphIssueModel{"display.empty", "nodes", "No nodes defined; no executable topology is implied."})
		return t
	}
	counts, indices, edgeCounts := map[string]int{}, map[string]int{}, map[string]int{}
	for i, n := range g.Nodes {
		counts[n.ID]++
		indices[n.ID] = i
	}
	for _, e := range g.Edges {
		edgeCounts[e.ID]++
	}
	for i, n := range g.Nodes {
		t.Nodes = append(t.Nodes, GraphPosition{Index: i, GridColumn: 1, GridRow: 1, Node: n})
		if n.ID == "" || counts[n.ID] != 1 {
			t.Issues = append(t.Issues, GraphIssueModel{"display.ambiguous_node", fmt.Sprintf("nodes[%d]", i), "Empty or duplicate node ID; incident edges are not drawn."})
		}
	}
	adjacent := make([][]int, len(g.Nodes))
	indegree, rank := make([]int, len(g.Nodes)), make([]int, len(g.Nodes))
	drawable := make([]bool, len(g.Edges))
	for i, e := range g.Edges {
		// Preserve all declared adjacency, including malformed edges, as text.
		for j, n := range g.Nodes {
			if e.From == n.ID {
				t.Nodes[j].Outgoing = append(t.Nodes[j].Outgoing, e)
			}
			if e.To == n.ID {
				t.Nodes[j].Incoming = append(t.Nodes[j].Incoming, e)
			}
		}
		if e.ID == "" || edgeCounts[e.ID] != 1 || e.From == "" || e.To == "" || counts[e.From] != 1 || counts[e.To] != 1 || e.From == e.To {
			t.Issues = append(t.Issues, GraphIssueModel{"display.unlinked_edge", fmt.Sprintf("dependencies[%d]", i), "Edge has empty, duplicate, missing, ambiguous or self-linked endpoints/identity; preserved as text, not drawn."})
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
	cyclic := len(ready) != len(g.Nodes)
	if cyclic {
		t.Issues = append(t.Issues, GraphIssueModel{"display.cycle", "dependencies", "Cycle detected: directed layout withheld. Nodes are indexed below; all declared edges remain in the textual equivalent."})
		for i := range rank {
			rank[i] = i % 4
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
	if !cyclic {
		// Track dimensions match the CSS grid in graph_workspace.css:
		// 212px nodes with 32px column gap, 102px nodes with 32px row gap,
		// and 32px padding around the stage. SVG paths use absolute pixel
		// coordinates inside a viewBox so getPointAtLength/getCTM can resolve
		// endpoints. CSS-grid places the buttons on the same tracks so SVG
		// and HTML stay aligned without inline styles.
		nodeW := 212
		nodeH := 102
		gap := 32
		pad := 32
		colStride := nodeW + gap
		rowStride := nodeH + gap
		for i, e := range g.Edges {
			if !drawable[i] {
				continue
			}
			a, b := t.Nodes[indices[e.From]], t.Nodes[indices[e.To]]
			x1 := float64((a.GridColumn-1)*colStride + pad + nodeW)
			y1 := float64((a.GridRow-1)*rowStride + pad + nodeH/2)
			x2 := float64((b.GridColumn-1)*colStride + pad)
			y2 := float64((b.GridRow-1)*rowStride + pad + nodeH/2)
			path := fmt.Sprintf("M %.2f %.2f C %.2f %.2f, %.2f %.2f, %.2f %.2f", x1, y1, x1+40, y1, x2-40, y2, x2, y2)
			if rank[indices[e.To]]-rank[indices[e.From]] > 1 {
				// Skip-rank edges travel in the gutter above subsequent rows so
				// they do not imply links to intermediate nodes. The gutter
				// sits a fixed pixel offset from the viewBox top so the
				// SVG geometry API can resolve the full path.
				gutterY := float64(pad) - 8
				path = fmt.Sprintf("M %.2f %.2f L %.2f %.2f L %.2f %.2f L %.2f %.2f L %.2f %.2f L %.2f %.2f", x1, y1, x1+28, y1, x1+28, gutterY, x2-28, gutterY, x2-28, y2, x2, y2)
			}
			t.Edges = append(t.Edges, GraphLine{Index: i, Path: path})
		}
	}
	return t
}
