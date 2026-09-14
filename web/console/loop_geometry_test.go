package consoleweb

import (
	"fmt"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/loop"
)

// loopGeometryCoverage rejects missing, duplicate, and unknown identities before
// endpoint checks, so iterating an empty projection cannot count as evidence.
func loopGeometryCoverage(topology LoopTopology, detail *LoopDetailModel) error {
	if len(detail.Steps) == 0 || len(detail.Transitions) == 0 || len(topology.Nodes) != len(detail.Steps) || len(topology.Edges) != len(detail.Transitions) {
		return fmt.Errorf("incomplete topology: %d nodes, %d edges; want %d nodes, %d edges", len(topology.Nodes), len(topology.Edges), len(detail.Steps), len(detail.Transitions))
	}
	nodes := map[string]int{}
	for _, node := range topology.Nodes {
		nodes[node.Step.ID]++
	}
	for _, step := range detail.Steps {
		if nodes[step.ID] != 1 {
			return fmt.Errorf("node %s appears %d times", step.ID, nodes[step.ID])
		}
	}
	edges := map[int]int{}
	for _, edge := range topology.Edges {
		if edge.Index < 0 || edge.Index >= len(detail.Transitions) {
			return fmt.Errorf("unknown transition index %d", edge.Index)
		}
		edges[edge.Index]++
	}
	for index, transition := range detail.Transitions {
		if edges[index] != 1 {
			return fmt.Errorf("transition %s (%s -> %s) appears %d times", transition.ID, transition.FromStepID, transition.ToStepID, edges[index])
		}
	}
	return nil
}

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

func TestLoopNarrowViewportRetainsAuthenticatedHeaderControls(t *testing.T) {
	// Source regression only: installed Chrome proves actual viewport geometry.
	rule := `@media(max-width:420px){body:has(.loop-detail-page) .topbar{gap:6px;padding:0 8px}body:has(.loop-detail-page) .topbar-status{white-space:normal;min-width:0;font-size:10px}body:has(.loop-detail-page) .topbar .ghost{white-space:normal;min-height:42px;padding:4px 7px}}`
	if !strings.Contains(string(CSS), rule) {
		t.Fatal("missing Loop-scoped narrow authenticated-header containment")
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

func TestLoopGeometryWideReverseDeclaration(t *testing.T) {
	for _, size := range []int{9, loop.MaxSteps} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			detail := &LoopDetailModel{}
			for i := size - 1; i >= 0; i-- {
				detail.Steps = append(detail.Steps, LoopStepModel{ID: fmt.Sprintf("step-%d", i), Entry: i == 0})
			}
			for i := 0; i < size-1; i++ {
				detail.Transitions = append(detail.Transitions, LoopTransitionModel{ID: fmt.Sprintf("edge-%d", i), FromStepID: fmt.Sprintf("step-%d", i), ToStepID: fmt.Sprintf("step-%d", i+1)})
			}
			topology := buildLoopTopology(detail)
			if err := loopGeometryCoverage(topology, detail); err != nil {
				t.Fatal(err)
			}
			if len(topology.Issues) != 0 || topology.Columns != size || topology.Rows != 1 {
				t.Fatalf("unexpected wide topology: columns=%d rows=%d issues=%v", topology.Columns, topology.Rows, topology.Issues)
			}
			for index, node := range topology.Nodes {
				if node.GridColumn != size-index || node.GridRow != 1 {
					t.Fatalf("node %s misplaced at %d,%d", node.Step.ID, node.GridColumn, node.GridRow)
				}
			}
		})
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
	if err := loopGeometryCoverage(topology, detail); err != nil {
		t.Fatal(err)
	}
	// Mutation control: a missing edge must fail coverage, including the
	// previously vacuous case where the projection returns no edges at all.
	for removed := range topology.Edges {
		mutated := topology
		mutated.Edges = append(append([]LoopLine(nil), topology.Edges[:removed]...), topology.Edges[removed+1:]...)
		if loopGeometryCoverage(mutated, detail) == nil {
			t.Fatalf("coverage accepted missing transition %d", removed)
		}
	}
	empty := topology
	empty.Edges = nil
	if loopGeometryCoverage(empty, detail) == nil {
		t.Fatal("coverage accepted zero edges")
	}
	duplicate := topology
	duplicate.Edges = append([]LoopLine(nil), topology.Edges...)
	duplicate.Edges[0] = duplicate.Edges[1]
	if loopGeometryCoverage(duplicate, detail) == nil {
		t.Fatal("coverage accepted duplicate transition with unchanged edge count")
	}
	missingNode := topology
	missingNode.Nodes = topology.Nodes[1:]
	if loopGeometryCoverage(missingNode, detail) == nil {
		t.Fatal("coverage accepted missing node")
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
