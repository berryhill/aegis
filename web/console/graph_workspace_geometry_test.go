package consoleweb

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/graph"
)

func TestGraphPlacementBothAxesThroughDisplayBound(t *testing.T) {
	for i := 1; i <= graph.MaxNodes; i++ {
		for _, axis := range []string{"column", "row"} {
			want := fmt.Sprintf(".graph-workspace .graph-node[data-grid-%s=\"%d\"]{grid-%s:%d}", axis, i, axis, i)
			if !bytes.Contains(CSS, []byte(want)) {
				t.Fatalf("missing external placement %s", want)
			}
		}
	}
	for _, want := range []string{"grid-auto-columns:212px", "grid-auto-rows:102px", "height:102px", "box-sizing:border-box"} {
		if !bytes.Contains(graphWorkspaceCSS, []byte(want)) {
			t.Errorf("missing fixed geometry %q", want)
		}
	}
	if bytes.Contains(CSS, []byte(`.graph-workspace .graph-node[data-grid-column="129"]`)) {
		t.Fatal("placement exceeds graph bound")
	}
}

func TestGraphBranchMergeReverseAndDisconnectedGeometry(t *testing.T) {
	edges := []GraphEdgeModel{{ID: "ab", From: "a", To: "b"}, {ID: "bc", From: "b", To: "c"}, {ID: "bd", From: "b", To: "d"}, {ID: "ce", From: "c", To: "e"}, {ID: "de", From: "d", To: "e"}, {ID: "ef", From: "e", To: "f"}}
	for _, order := range [][]string{{"a", "b", "c", "d", "e", "f", "isolated"}, {"f", "e", "d", "c", "b", "a", "isolated"}} {
		g := &GraphDetailModel{Edges: edges}
		for _, id := range order {
			g.Nodes = append(g.Nodes, GraphNodeModel{ID: id})
		}
		p := buildGraphTopology(g)
		byID := map[string]GraphPosition{}
		occupied := map[[2]int]bool{}
		for _, n := range p.Nodes {
			cell := [2]int{n.GridColumn, n.GridRow}
			if occupied[cell] {
				t.Fatal("overlapping projected cells")
			}
			occupied[cell] = true
			byID[n.Node.ID] = n
			col := map[string]int{"a": 1, "b": 2, "c": 3, "d": 3, "e": 4, "f": 5, "isolated": 1}[n.Node.ID]
			if n.GridColumn != col {
				t.Fatalf("%s rank = %d, want %d", n.Node.ID, n.GridColumn, col)
			}
		}
		if byID["e"].GridRow != 1 || byID["f"].GridRow != 1 || p.Columns != 5 || p.Rows != 2 {
			t.Fatalf("branch/merge row drift: %+v", p)
		}
		for _, line := range p.Edges {
			edge := g.Edges[line.Index]
			a, b := byID[edge.From], byID[edge.To]
			var x1, y1, c1, c2, c3, c4, x2, y2 float64
			if n, err := fmt.Sscanf(line.Path, "M %f %f C %f %f, %f %f, %f %f", &x1, &y1, &c1, &c2, &c3, &c4, &x2, &y2); err != nil || n != 8 {
				t.Fatal(line.Path, err)
			}
			if x1 != float64(24+(a.GridColumn-1)*290+212) || y1 != float64(24+(a.GridRow-1)*132+51) || x2 != float64(24+(b.GridColumn-1)*290) || y2 != float64(24+(b.GridRow-1)*132+51) {
				t.Fatalf("edge/card mismatch: %s", line.Path)
			}
		}
	}
}

func TestGraphFullRankAndRowGeometry(t *testing.T) {
	for _, chain := range []bool{true, false} {
		g := &GraphDetailModel{}
		for i := 0; i < graph.MaxNodes; i++ {
			g.Nodes = append(g.Nodes, GraphNodeModel{ID: fmt.Sprint(i)})
			if chain && i > 0 {
				g.Edges = append(g.Edges, GraphEdgeModel{ID: fmt.Sprint(i), From: fmt.Sprint(i - 1), To: fmt.Sprint(i)})
			}
		}
		p := buildGraphTopology(g)
		for i, n := range p.Nodes {
			col, row := 1, i+1
			if chain {
				col, row = i+1, 1
			}
			if n.GridColumn != col || n.GridRow != row {
				t.Fatalf("node %d: %+v", i, n)
			}
		}
		if chain && (p.Columns != graph.MaxNodes || len(p.Edges) != graph.MaxNodes-1) {
			t.Fatal("full chain lost")
		}
		if !chain && p.Rows != graph.MaxNodes {
			t.Fatal("full row stack lost")
		}
	}
}

func TestGraphOverBoundRecordBackedText(t *testing.T) {
	for _, nodeOverflow := range []bool{true, false} {
		g := &GraphDetailModel{Nodes: []GraphNodeModel{{ID: "duplicate", Participant: "agent exact", Loop: "loop exact"}, {ID: "duplicate"}}}
		if nodeOverflow {
			for i := 2; i <= graph.MaxNodes; i++ {
				g.Nodes = append(g.Nodes, GraphNodeModel{ID: fmt.Sprintf("declared-%d", i)})
			}
		}
		count := 1
		if !nodeOverflow {
			count = graph.MaxDependencies + 1
		}
		for i := 0; i < count; i++ {
			g.Edges = append(g.Edges, GraphEdgeModel{ID: "duplicate-edge", From: "duplicate", To: "<missing>"})
		}
		p := buildGraphTopology(g)
		if len(p.Nodes) != 0 || len(p.Edges) != 0 {
			t.Fatal("partial oversized canvas")
		}
		r := &RecordModel{Graph: g}
		var out bytes.Buffer
		if err := GraphWorkspace(SurfaceModel{Domain: DomainGraphs}, r, p).Render(context.Background(), &out); err != nil {
			t.Fatal(err)
		}
		html := out.String()
		for i, n := range g.Nodes {
			want := fmt.Sprintf("%s · node %d", n.ID, i+1)
			if !strings.Contains(html, want) {
				t.Fatalf("record node omitted: %s", want)
			}
		}
		if strings.Count(html, "<strong>duplicate-edge</strong>") != count || strings.Contains(html, "<missing>") || !strings.Contains(html, "&lt;missing&gt;") {
			t.Fatal("malformed/duplicate edges lost or unescaped")
		}
		for _, want := range []string{"agent exact", "loop exact", "Canvas and per-node adjacency omitted", "display.bound_exceeded"} {
			if !strings.Contains(html, want) {
				t.Errorf("missing %q", want)
			}
		}
		if strings.Contains(html, `data-graph-node="`) {
			t.Fatal("oversized nodes drawn")
		}
	}
}
