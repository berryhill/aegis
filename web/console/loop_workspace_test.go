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
	if topology.ExhaustionDestination == "" || topology.ExhaustionDestination == "Not declared" {
		t.Fatal("expected exhaustion destination to be populated")
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
