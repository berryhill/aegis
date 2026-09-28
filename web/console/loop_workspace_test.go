package consoleweb

import (
	"context"
	"strings"
	"testing"
)

func TestLoopTopologyCyclicBranchingTerminalAndHistoricalCases(t *testing.T) {
	cyclic := &LoopDetailModel{Steps: []LoopStepModel{
		{ID: "fetch", Kind: "action", Entry: true, MaxAttempts: 1},
		{ID: "check", Kind: "gate", MaxAttempts: 1},
		{ID: "approve", Kind: "terminal", TerminalOutcome: "succeeded", MaxAttempts: 1},
		{ID: "reject", Kind: "terminal", TerminalOutcome: "failed", MaxAttempts: 1},
	}, Transitions: []LoopTransitionModel{
		{ID: "fetch-to-check", FromStepID: "fetch", ToStepID: "check", MaxTraversals: 4},
		{ID: "loop-back", FromStepID: "check", ToStepID: "fetch", MaxTraversals: 5},
		{ID: "approve-edge", FromStepID: "check", ToStepID: "approve", Condition: "approved"},
		{ID: "reject-edge", FromStepID: "check", ToStepID: "reject", Condition: "rejected"},
	}}
	topology := buildLoopTopology(cyclic)
	if len(topology.Issues) == 0 {
		t.Fatal("cyclic topology should record a display.cycle issue")
	}
	if topology.CycleMaxIterations < 5 {
		t.Fatalf("expected cycle max iterations 5, got %d", topology.CycleMaxIterations)
	}
	if topology.CycleExitCondition != "approved" {
		t.Fatalf("expected exit condition approved, got %q", topology.CycleExitCondition)
	}
	if len(topology.CycleMembers) == 0 {
		t.Fatal("expected at least one cycle member")
	}
	if topology.ExhaustionDestination != "Not declared" {
		t.Fatalf("an ordinary approved/rejected exit is not an exhaustion destination: %q", topology.ExhaustionDestination)
	}
	branching := &LoopDetailModel{Steps: []LoopStepModel{
		{ID: "a", Kind: "action", Entry: true, MaxAttempts: 1},
		{ID: "b", Kind: "action", MaxAttempts: 1},
		{ID: "c", Kind: "action", MaxAttempts: 1},
	}, Transitions: []LoopTransitionModel{
		{ID: "a-to-b", FromStepID: "a", ToStepID: "b", Condition: "ok"},
		{ID: "a-to-c", FromStepID: "a", ToStepID: "c", Condition: "no"},
	}}
	if p := buildLoopTopology(branching); len(p.Issues) != 0 {
		t.Fatalf("branching topology should not report issues: %+v", p.Issues)
	}
	terminalOnly := &LoopDetailModel{Steps: []LoopStepModel{
		{ID: "only", Kind: "terminal", TerminalOutcome: "succeeded", MaxAttempts: 1, Entry: true},
	}, Transitions: nil}
	if p := buildLoopTopology(terminalOnly); len(p.Issues) != 0 {
		t.Fatalf("terminal-only topology should not report issues: %+v", p.Issues)
	}
	invalid := &LoopDetailModel{Steps: []LoopStepModel{
		{ID: "a", Kind: "action", Entry: true, MaxAttempts: 1},
		{ID: "a", Kind: "action", MaxAttempts: 1},
	}, Transitions: []LoopTransitionModel{
		{ID: "t", FromStepID: "", ToStepID: "a"},
	}}
	if p := buildLoopTopology(invalid); len(p.Issues) == 0 {
		t.Fatal("invalid topology should report issues")
	}
	historical := &LoopDetailModel{Steps: []LoopStepModel{
		{ID: "x", Kind: "action", Entry: true, MaxAttempts: 1},
		{ID: "y", Kind: "action", MaxAttempts: 1},
	}, Transitions: []LoopTransitionModel{
		{ID: "x-to-y", FromStepID: "x", ToStepID: "y"},
	}}
	if p := buildLoopTopology(historical); len(p.Issues) != 0 {
		t.Fatalf("historical topology should not report issues: %+v", p.Issues)
	}
	max := &LoopDetailModel{}
	for i := 0; i < 257; i++ {
		max.Steps = append(max.Steps, LoopStepModel{ID: "s"})
	}
	if p := buildLoopTopology(max); len(p.Issues) == 0 || p.Issues[0].Code != "display.bound_exceeded" {
		t.Fatal("maximum-size overflow should be rejected")
	}
}

func TestLoopTopologyPublishedDoerCycleDrawsExactTransitions(t *testing.T) {
	// Match the owning instance's v4 nine-step/ten-transition shape.
	ids := []string{"completion", "diagnosis", "done", "eligibility", "failed", "implement", "judgment", "missing-input", "verify"}
	model := &LoopDetailModel{}
	for _, id := range ids {
		model.Steps = append(model.Steps, LoopStepModel{ID: id, Kind: "action", MaxAttempts: 1})
	}
	model.Steps[3].Entry = true
	model.Steps[2].Kind, model.Steps[2].TerminalOutcome = "terminal", "succeeded"
	model.Steps[4].Kind, model.Steps[4].TerminalOutcome = "terminal", "failed"
	model.Transitions = []LoopTransitionModel{
		{ID: "completed", FromStepID: "completion", ToStepID: "done"},
		{ID: "diagnosed", FromStepID: "diagnosis", ToStepID: "implement", MaxTraversals: 3},
		{ID: "eligible", FromStepID: "eligibility", ToStepID: "implement", Condition: "eligible"},
		{ID: "implemented", FromStepID: "implement", ToStepID: "judgment", MaxTraversals: 3},
		{ID: "judged", FromStepID: "judgment", ToStepID: "verify", MaxTraversals: 3},
		{ID: "missing-input-failed", FromStepID: "missing-input", ToStepID: "failed"},
		{ID: "needs-input", FromStepID: "eligibility", ToStepID: "missing-input", Condition: "needs_input"},
		{ID: "verification-exhausted", FromStepID: "verify", ToStepID: "failed", Condition: "exhausted"},
		{ID: "verification-retry", FromStepID: "verify", ToStepID: "diagnosis", Condition: "retry", MaxTraversals: 3},
		{ID: "verified", FromStepID: "verify", ToStepID: "completion", Condition: "verified"},
	}
	topology := buildLoopTopology(model)
	if len(topology.Edges) != len(model.Transitions) {
		t.Fatalf("cycle lost stored edges: got %d want %d", len(topology.Edges), len(model.Transitions))
	}
	for i, e := range topology.Edges {
		if e.Index != i || e.Path == "" {
			t.Fatalf("transition %d not drawn at stored index: %+v", i, e)
		}
	}
	if !topology.Edges[8].Back {
		t.Fatalf("verified retry must return through the feedback gutter: %+v", topology.Edges[8])
	}
	if topology.Edges[8].LabelY >= 32 || !strings.Contains(topology.Edges[8].Path, " 13.00") {
		t.Fatalf("retry label and path must stay above first-row nodes: %+v", topology.Edges[8])
	}
	if topology.CycleExhaustionToID != "failed" {
		t.Fatalf("ordinary success exit mistaken for exhaustion: %q", topology.CycleExhaustionToID)
	}
	var out strings.Builder
	record := &RecordModel{Key: "xander-doer:1", Label: "xander-doer", Revision: "1", Lifecycle: "draft", Loop: model}
	if err := LoopWorkspace(SurfaceModel{Domain: DomainLoops}, record, topology).Render(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`data-transition-id="verification-retry"`, `retry · max 3`, `data-transition-id="verified"`, `data-transition-id="verification-exhausted"`} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("rendered cycle missing %q", want)
		}
	}
	model.DoerV5 = true
	out.Reset()
	if err := LoopWorkspace(SurfaceModel{Domain: DomainLoops}, record, buildLoopTopology(model)).Render(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	if strings.Count(out.String(), `<path class="loop-edge-path`) != 10 || !strings.Contains(out.String(), `data-transition-id="verification-retry"`) {
		t.Fatal("v5 typed-input Loop lost its same immutable cyclic topology")
	}
}

func TestLoopTopologyMalformedParallelSelfEdgeAndAcyclicPreservation(t *testing.T) {
	model := &LoopDetailModel{Steps: []LoopStepModel{
		{ID: "entry", Entry: true}, {ID: "check"}, {ID: "exit", Kind: "terminal", TerminalOutcome: "succeeded"},
	}, Transitions: []LoopTransitionModel{
		{ID: "forward", FromStepID: "entry", ToStepID: "check"},
		{ID: "parallel", FromStepID: "entry", ToStepID: "check", Condition: "other"},
		{ID: "end", FromStepID: "check", ToStepID: "exit"},
		{ID: "self", FromStepID: "check", ToStepID: "check"},
		{ID: "missing", FromStepID: "absent", ToStepID: "check"},
		{ID: "duplicate", FromStepID: "entry", ToStepID: "exit"},
		{ID: "duplicate", FromStepID: "entry", ToStepID: "exit"},
	}}
	topology := buildLoopTopology(model)
	if len(topology.Edges) != 3 || topology.Edges[0].Index != 0 || topology.Edges[1].Index != 1 || topology.Edges[2].Index != 2 {
		t.Fatalf("malformed edges drawn or parallel identities dropped: %+v", topology.Edges)
	}
	if topology.Edges[0].Path == topology.Edges[1].Path {
		t.Fatal("parallel edges are geometrically indistinguishable")
	}
	for _, edge := range topology.Edges {
		if edge.Back {
			t.Fatalf("acyclic edge classified as feedback: %+v", edge)
		}
	}
	if len(topology.Issues) != 4 {
		t.Fatalf("expected one safe warning per invalid edge: %+v", topology.Issues)
	}
}

func TestLoopWorkspaceCycleBranchTerminalInvalidAndMaximumFixtures(t *testing.T) {
	cases := map[string]*LoopDetailModel{
		"cyclic":     {Steps: []LoopStepModel{{ID: "a", Kind: "action", Entry: true, MaxAttempts: 1}, {ID: "b", Kind: "terminal", TerminalOutcome: "succeeded", MaxAttempts: 1}}, Transitions: []LoopTransitionModel{{ID: "t", FromStepID: "a", ToStepID: "b", MaxTraversals: 4}, {ID: "back", FromStepID: "b", ToStepID: "a", MaxTraversals: 4}}},
		"branch":     {Steps: []LoopStepModel{{ID: "a", Kind: "action", Entry: true, MaxAttempts: 1}, {ID: "b", Kind: "action", MaxAttempts: 1}, {ID: "c", Kind: "action", MaxAttempts: 1}}, Transitions: []LoopTransitionModel{{ID: "ab", FromStepID: "a", ToStepID: "b", Condition: "ok"}, {ID: "ac", FromStepID: "a", ToStepID: "c", Condition: "no"}}},
		"invalid":    {Steps: []LoopStepModel{{ID: "a", Kind: "action", Entry: true, MaxAttempts: 1}}, Transitions: []LoopTransitionModel{{FromStepID: "missing", ToStepID: "a"}}},
		"terminal":   {Steps: []LoopStepModel{{ID: "a", Kind: "terminal", TerminalOutcome: "succeeded", MaxAttempts: 1, Entry: true}}, Transitions: nil},
		"historical": {Steps: []LoopStepModel{{ID: "a", Kind: "action", Entry: true, MaxAttempts: 1}, {ID: "b", Kind: "action", MaxAttempts: 1}}, Transitions: []LoopTransitionModel{{ID: "ab", FromStepID: "a", ToStepID: "b"}}},
	}
	for name, detail := range cases {
		t.Run(name, func(t *testing.T) {
			record := &RecordModel{Key: name, Label: name, Revision: "r1", Lifecycle: "draft", Loop: detail}
			topology := buildLoopTopology(detail)
			var out strings.Builder
			if err := LoopWorkspace(SurfaceModel{Domain: DomainLoops}, record, topology).Render(context.Background(), &out); err != nil {
				t.Fatal(err)
			}
			html := out.String()
			if !strings.Contains(html, "data-loop-workspace") {
				t.Fatal("missing workspace root")
			}
			if !strings.Contains(html, "Required inputs") || !strings.Contains(html, "Evidence and outcomes") {
				t.Fatal("missing tabs")
			}
			if !strings.Contains(html, "Accessible textual equivalent") {
				t.Fatal("missing textual fallback")
			}
		})
	}
}

func TestLoopWorkspaceCspAndKeyboardShape(t *testing.T) {
	detail := &LoopDetailModel{Steps: []LoopStepModel{{ID: "intake", Kind: "action", Entry: true, MaxAttempts: 2}, {ID: "publish", Kind: "terminal", TerminalOutcome: "succeeded", MaxAttempts: 1}}, Transitions: []LoopTransitionModel{{ID: "next", FromStepID: "intake", ToStepID: "publish", Condition: "ok", MaxTraversals: 1}}}
	record := &RecordModel{Key: "loop-csp", Label: "loop-csp", Revision: "r1", Lifecycle: "active", Loop: detail}
	var out strings.Builder
	if err := LoopWorkspace(SurfaceModel{Domain: DomainLoops}, record, buildLoopTopology(detail)).Render(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	html := out.String()
	for _, required := range []string{
		`data-loop-fit`, `data-loop-zoom="in"`, `data-loop-zoom="out"`,
		`data-loop-tab="inputs"`, `data-loop-tab="evidence"`,
		`tabindex="0"`, `role="region"`,
	} {
		if !strings.Contains(html, required) {
			t.Fatalf("missing CSP/keyboard affordance %q", required)
		}
	}
	if strings.Contains(html, "style=") && !strings.Contains(html, "style=\"display:") {
		t.Fatalf("unexpected inline style attribute: %s", html)
	}
}

func TestLoopWorkspaceCSSAndJSEmbedded(t *testing.T) {
	if !strings.Contains(string(CSS), "loop-workspace") {
		t.Fatal("embedded CSS does not contain loop-workspace selectors")
	}
	if !strings.Contains(string(NavigationJS), "loop-workspace") {
		t.Fatal("embedded JS does not contain loop-workspace enhancement")
	}
}
