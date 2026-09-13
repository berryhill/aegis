package consoleweb

import (
	"fmt"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/loop"
)

// These checks cover the no-JavaScript, strict-CSP coordinate contract.
// Browser-computed endpoint alignment remains a separate acceptance check.
func TestLoopGridPlacementCoversSupportedBounds(t *testing.T) {
	css := string(CSS)
	for i := 1; i <= loop.MaxSteps; i++ {
		for _, axis := range []string{"column", "row"} {
			rule := fmt.Sprintf(".loop-workspace .loop-node[data-grid-%s=\"%d\"]{grid-%s:%d}", axis, i, axis, i)
			if !strings.Contains(css, rule) {
				t.Fatalf("missing explicit placement: %s", rule)
			}
		}
	}
	for _, rule := range []string{"grid-auto-columns:212px", "height:102px;box-sizing:border-box"} {
		if !strings.Contains(string(loopWorkspaceCSS), rule) {
			t.Fatalf("missing fixed geometry: %s", rule)
		}
	}
}

func TestLoopGridPlacementRejectsOutOfRangeSelectors(t *testing.T) {
	css := string(loopGridPlacementCSS())
	for _, index := range []int{-1, 0, loop.MaxSteps + 1} {
		for _, axis := range []string{"row", "column"} {
			selector := fmt.Sprintf("data-grid-%s=\"%d\"", axis, index)
			if strings.Contains(css, selector) {
				t.Fatalf("out-of-range placement rule: %s", selector)
			}
		}
	}
	if got := strings.Count(css, "{grid-"); got != 2*loop.MaxSteps {
		t.Fatalf("unexpected placement rule count: %d", got)
	}
}

func TestLoopGeometryReverseDeclarationBranchAndJoin(t *testing.T) {
	detail := &LoopDetailModel{
		Steps: []LoopStepModel{{ID: "join"}, {ID: "right"}, {ID: "left"}, {ID: "entry", Entry: true}},
		Transitions: []LoopTransitionModel{
			{ID: "el", FromStepID: "entry", ToStepID: "left"},
			{ID: "er", FromStepID: "entry", ToStepID: "right"},
			{ID: "lj", FromStepID: "left", ToStepID: "join"},
			{ID: "rj", FromStepID: "right", ToStepID: "join"},
		},
	}
	topology := buildLoopTopology(detail)
	if len(topology.Issues) != 0 || topology.Columns != 3 || topology.Rows != 2 {
		t.Fatalf("unexpected topology: %+v", topology)
	}
	positions := map[string]LoopPosition{}
	occupied := map[[2]int]bool{}
	for _, node := range topology.Nodes {
		cell := [2]int{node.GridColumn, node.GridRow}
		if occupied[cell] {
			t.Fatalf("overlapping grid cell: %v", cell)
		}
		occupied[cell] = true
		positions[node.Step.ID] = node
	}
	if positions["entry"].GridColumn != 1 || positions["join"].GridColumn != 3 {
		t.Fatal("declaration order overrode directed ranks")
	}
	for _, edge := range topology.Edges {
		transition := detail.Transitions[edge.Index]
		a, b := positions[transition.FromStepID], positions[transition.ToStepID]
		start := fmt.Sprintf("M %.2f %.2f ", float64((a.GridColumn-1)*244+244), float64((a.GridRow-1)*134+83))
		end := fmt.Sprintf(", %.2f %.2f", float64((b.GridColumn-1)*244+32), float64((b.GridRow-1)*134+83))
		if !strings.HasPrefix(edge.Path, start) || !strings.HasSuffix(edge.Path, end) {
			t.Fatalf("edge %s misses projected node border: %s", transition.ID, edge.Path)
		}
	}
}
