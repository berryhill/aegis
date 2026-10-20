package consoleweb

import (
	"fmt"
	"strings"
	"testing"
)

func TestIssue330QueueGridPlacementBounds(t *testing.T) {
	css := string(queueGridPlacementCSS())
	if strings.Count(css, "{grid-") != 512 {
		t.Fatal("queue placement must contain exactly the bounded row and column rules")
	}
	for _, axis := range []string{"column", "row"} {
		for i := 1; i <= 256; i++ {
			rule := fmt.Sprintf(".queue-workspace .queue-node[data-grid-%s=\"%d\"]{grid-%s:%d}", axis, i, axis, i)
			if !strings.Contains(css, rule) || !strings.Contains(string(CSS), rule) {
				t.Fatalf("missing external placement rule: %s", rule)
			}
		}
	}
}

func TestIssue330CanonicalAdapterBoundsAndDeclarations(t *testing.T) {
	for _, size := range []int{9, 256, 257} {
		for _, reverse := range []bool{false, true} {
			d := &LoopDetailModel{}
			nodes := []QueueControlNodeModel{}
			edges := []QueueControlEdgeModel{}
			for i := 0; i < size; i++ {
				id := fmt.Sprintf("step-%03d", i)
				d.Steps = append(d.Steps, LoopStepModel{ID: id})
				nodes = append(nodes, QueueControlNodeModel{NodeID: id})
			}
			for i := 1; i < size; i++ {
				a, b := i-1, i
				if reverse {
					a, b = b, a
				}
				e := LoopTransitionModel{ID: fmt.Sprint(i), FromStepID: nodes[a].NodeID, ToStepID: nodes[b].NodeID, Condition: "retry", MaxTraversals: 2}
				d.Transitions = append(d.Transitions, e)
				edges = append(edges, QueueControlEdgeModel{EdgeID: e.ID, From: e.FromStepID, To: e.ToStepID, Condition: e.Condition, Limit: e.MaxTraversals})
			}
			want := buildLoopTopology(d)
			got := BuildLoopExecutionTopology(nodes, edges)
			if got.Columns != want.Columns || got.Rows != want.Rows || len(got.EdgeGeometry) != len(want.Edges) {
				t.Fatalf("size %d reverse %v geometry mismatch", size, reverse)
			}
			if size > 256 {
				if len(got.Issues) == 0 {
					t.Fatal("bound not reported")
				}
				continue
			}
			for i, n := range want.Nodes {
				if got.Nodes[i].GridColumn != n.GridColumn || got.Nodes[i].GridRow != n.GridRow {
					t.Fatal("cell mismatch")
				}
			}
			for i, e := range want.Edges {
				g := got.EdgeGeometry[i]
				if g.Path != e.Path || g.LabelX != e.LabelX || g.LabelY != e.LabelY || g.Back != e.Back || g.Label != loopTransitionLabel(d.Transitions[e.Index]) {
					t.Fatal("edge mismatch")
				}
			}
		}
	}
}
