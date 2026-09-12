package consoleweb

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestGraphTopologyConnectedLayoutAndDisconnectedNodes(t *testing.T) {
	g := &GraphDetailModel{Nodes: []GraphNodeModel{{ID: "a", Loop: "l r1"}, {ID: "b", Loop: "l r1"}, {ID: "isolated", Loop: "l r2"}}, Edges: []GraphEdgeModel{{ID: "ab", From: "a", To: "b", Mappings: "x → y"}}}
	topology := buildGraphTopology(g)
	if len(topology.Issues) != 0 || len(topology.Nodes) != 3 || len(topology.Edges) != 1 || topology.LoopCount != 2 {
		t.Fatalf("bad projection: %+v", topology)
	}
	a, b := topology.Nodes[0], topology.Nodes[1]
	if a.GridColumn >= b.GridColumn || len(a.Outgoing) != 1 || len(b.Incoming) != 1 {
		t.Fatal("layout or adjacency lost")
	}
	if !strings.HasPrefix(topology.Edges[0].Path, fmt.Sprintf("M %d.%d", 32+(a.GridColumn-1)*292+212, 0)) {
		t.Fatalf("edge not anchored to exact nodes: %s", topology.Edges[0].Path)
	}
	if topology.Nodes[2].GridColumn == a.GridColumn && topology.Nodes[2].GridRow == a.GridRow {
		t.Fatal("disconnected component overlaps")
	}
	if g.Nodes[0].ID != "a" || g.Edges[0].From != "a" {
		t.Fatal("projection mutated definition")
	}
}

func TestGraphTopologyNeverInventsAmbiguousEdges(t *testing.T) {
	for _, tc := range []struct {
		name  string
		nodes []GraphNodeModel
		edges []GraphEdgeModel
		code  string
	}{
		{"duplicate nodes", []GraphNodeModel{{ID: "a"}, {ID: "a"}, {ID: "b"}}, []GraphEdgeModel{{ID: "ab", From: "a", To: "b"}}, "display.ambiguous_node"},
		{"missing endpoint", []GraphNodeModel{{ID: "a"}}, []GraphEdgeModel{{ID: "ab", From: "a", To: "missing"}}, "display.unlinked_edge"},
		{"self", []GraphNodeModel{{ID: "a"}}, []GraphEdgeModel{{ID: "self", From: "a", To: "a"}}, "display.unlinked_edge"},
		{"duplicate edges", []GraphNodeModel{{ID: "a"}, {ID: "b"}}, []GraphEdgeModel{{ID: "ab", From: "a", To: "b"}, {ID: "ab", From: "a", To: "b"}}, "display.unlinked_edge"},
		{"cycle", []GraphNodeModel{{ID: "a"}, {ID: "b"}}, []GraphEdgeModel{{ID: "ab", From: "a", To: "b"}, {ID: "ba", From: "b", To: "a"}}, "display.cycle"},
		{"empty node ID", []GraphNodeModel{{ID: ""}, {ID: "b"}}, []GraphEdgeModel{{ID: "ab", From: "", To: "b"}}, "display.ambiguous_node"},
		{"empty edge ID", []GraphNodeModel{{ID: "a"}, {ID: "b"}}, []GraphEdgeModel{{From: "a", To: "b"}}, "display.unlinked_edge"},
		{"empty", nil, nil, "display.empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := &GraphDetailModel{Nodes: tc.nodes, Edges: tc.edges}
			p := buildGraphTopology(g)
			if len(p.Edges) != 0 {
				t.Fatalf("fabricated geometry: %+v", p.Edges)
			}
			found := false
			for _, issue := range p.Issues {
				found = found || issue.Code == tc.code
			}
			if !found {
				t.Fatalf("missing diagnostic %s", tc.code)
			}
			if len(p.Nodes) != len(tc.nodes) {
				t.Fatal("invalid nodes disappeared")
			}
		})
	}
}

func TestGraphTopologyBoundedDefinitions(t *testing.T) {
	g := &GraphDetailModel{}
	for i := 0; i < 128; i++ {
		g.Nodes = append(g.Nodes, GraphNodeModel{ID: fmt.Sprint(i)})
	}
	for i := 0; i < 127; i++ {
		g.Edges = append(g.Edges, GraphEdgeModel{ID: fmt.Sprint(i), From: fmt.Sprint(i), To: fmt.Sprint(i + 1)})
	}
	p := buildGraphTopology(g)
	if len(p.Nodes) != 128 || len(p.Edges) != 127 || p.Columns > 256 || p.Rows > 256 {
		t.Fatal("bounded DAG lost")
	}
	g.Nodes = append(g.Nodes, GraphNodeModel{ID: "overflow"})
	p = buildGraphTopology(g)
	if len(p.Nodes) != 0 || len(p.Edges) != 0 || len(p.Issues) != 1 || p.Issues[0].Code != "display.bound_exceeded" {
		t.Fatal("oversized definition silently truncated")
	}
}

func TestGraphTopologyEdgeBoundAndMalformedAdjacency(t *testing.T) {
	g := &GraphDetailModel{Nodes: []GraphNodeModel{{ID: "a"}, {ID: "b"}}}
	for i := 0; i < 512; i++ {
		g.Edges = append(g.Edges, GraphEdgeModel{ID: fmt.Sprint(i), From: "a", To: "b"})
	}
	p := buildGraphTopology(g)
	if len(p.Edges) != 512 || len(p.Issues) != 0 {
		t.Fatal("edge limit rejected")
	}
	g.Edges = append(g.Edges, GraphEdgeModel{ID: "overflow", From: "a", To: "b"})
	p = buildGraphTopology(g)
	if len(p.Nodes) != 0 || len(p.Edges) != 0 || len(p.Issues) != 1 || p.Issues[0].Code != "display.bound_exceeded" {
		t.Fatal("edge overflow silently truncated")
	}
	g.Edges = []GraphEdgeModel{{ID: "valid", From: "a", To: "b"}, {ID: "broken", From: "a", To: "absent"}}
	p = buildGraphTopology(g)
	if len(p.Edges) != 1 || p.Edges[0].Index != 0 || len(p.Nodes[0].Outgoing) != 2 || len(p.Issues) != 1 || p.Issues[0].Path != "dependencies[1]" {
		t.Fatal("malformed adjacency lost or drawn as valid")
	}
}

func TestGraphTopologySkipRankEdgeUsesGutter(t *testing.T) {
	g := &GraphDetailModel{Nodes: []GraphNodeModel{{ID: "a"}, {ID: "b"}, {ID: "c"}}, Edges: []GraphEdgeModel{{ID: "ab", From: "a", To: "b"}, {ID: "bc", From: "b", To: "c"}, {ID: "ac", From: "a", To: "c"}}}
	p := buildGraphTopology(g)
	if len(p.Edges) != 3 || len(p.Issues) != 0 {
		t.Fatal("skip-rank graph lost")
	}
	path := p.Edges[2].Path
	if !strings.HasPrefix(path, "M ") || !strings.Contains(path, " ") || !strings.Contains(path, ".00") {
		t.Fatalf("skip-rank edge lacks gutter waypoint: %s", path)
	}
}

func TestGraphWorkspaceRendersReadOnlyContextAndTextFallback(t *testing.T) {
	g := &GraphDetailModel{GraphID: "graph-x", LatestVersion: "r2 · viewing historical revision", CurrentValidation: "invalid", Validation: "valid · stored-digest", Nodes: []GraphNodeModel{{ID: "<script>alert(1)</script>", Participant: "agent r7 @ sha256:a", Loop: "loop r2 @ sha256:l", InputMappings: []FieldModel{{Label: "source", Value: "target"}}}, {ID: "b"}}, Edges: []GraphEdgeModel{{ID: "edge", From: "<script>alert(1)</script>", To: "b"}}, ValidationIssues: []GraphIssueModel{{Code: "invalid.test", Path: "nodes[0]", Message: "test failure"}}}
	r := RecordModel{Key: "graph-x:1", Label: "graph-x", Revision: "r1", Lifecycle: "draft", Graph: g}
	var out bytes.Buffer
	if err := GraphDetailPage(SurfaceModel{Domain: DomainGraphs}, &r).Render(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	html := out.String()
	for _, want := range []string{"#/graphs/graph-x:1", "Prepare execution request", "Definition details", "graph-edge-path", "marker-end=\"url(#graph-arrow)\"", "data-graph-node=\"0\"", "data-graph-node-panel=\"0\"", "aria-pressed=\"false\"", "Accessible textual equivalent", "Input mappings", "Incoming edges", "Outgoing edges", "data-reason-code=\"invalid.test\"", "viewing historical revision", "data-grid-column=\"1\"", "Stored validation"} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, bad := range []string{"<script>alert(1)</script>", "Run Graph", "data-detail-link", "method=\"post\""} {
		if strings.Contains(html, bad) {
			t.Errorf("unexpected %q", bad)
		}
	}
}
