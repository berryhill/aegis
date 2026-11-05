package consoleweb

import (
	"context"
	"strings"
	"testing"
)

func TestLoopResidualCompleteSCC(t *testing.T) {
	steps := []LoopStepModel{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}, {ID: "exit"}}
	edges := []LoopTransitionModel{
		{ID: "ab", FromStepID: "a", ToStepID: "b"}, {ID: "ba", FromStepID: "b", ToStepID: "a"},
		{ID: "ac", FromStepID: "a", ToStepID: "c"}, {ID: "ca", FromStepID: "c", ToStepID: "a"},
		{ID: "dd", FromStepID: "d", ToStepID: "d"},
	}
	got := loopDetectFirstSCC(steps, edges, map[string]int{"a": 0, "b": 1, "c": 2, "d": 3, "exit": 4})
	if len(got) != 3 || !got[0] || !got[1] || !got[2] {
		t.Fatalf("incomplete SCC: %v", got)
	}
	topology := buildLoopTopology(&LoopDetailModel{Steps: steps, Transitions: edges})
	if len(topology.Edges) != len(edges) {
		t.Fatalf("lost self-edge: %d/%d", len(topology.Edges), len(edges))
	}
}

func TestLoopResidualOverflowTextAndContextMappings(t *testing.T) {
	detail := &LoopDetailModel{}
	for i := 0; i < 257; i++ {
		detail.Steps = append(detail.Steps, LoopStepModel{ID: "stored-step"})
	}
	record := &RecordModel{Label: "stable-id", Revision: "r1", Loop: detail}
	var out strings.Builder
	if err := LoopWorkspace(SurfaceModel{}, record, buildLoopTopology(detail)).Render(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	if strings.Count(out.String(), "Step ID") != 257 {
		t.Fatalf("overflow erased textual steps: %d", strings.Count(out.String(), "Step ID"))
	}
	out.Reset()
	node := LoopPosition{Step: LoopStepModel{ID: "a"}, Outgoing: []LoopTransitionModel{{ID: "edge", Mappings: []LoopPortMappingModel{{SourcePort: "source-value", TargetPort: "target-value"}}}}}
	if err := LoopNodeDetails(node).Render(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "source-value") || !strings.Contains(out.String(), "target-value") {
		t.Fatal("context omitted stored mapping")
	}
}
