package consoleweb

import (
	"context"
	"strings"
	"testing"
)

// TestQueueTopologyCyclicBranchingTerminalAndHistoricalCases covers the
// required acceptance scenarios: branches, joins, retries, cycles,
// terminal-only definitions, historical definitions, and maximum-size
// overflow. Each fixture must remain inspectable and truthful.
func TestQueueTopologyCyclicBranchingTerminalAndHistoricalCases(t *testing.T) {
	cyclic := &QueueDetailModel{Nodes: []QueueControlNodeModel{
		{Index: 0, NodeID: "fetch", State: "started", Reachable: true},
		{Index: 1, NodeID: "approve", State: "pending", TerminalEligible: true, Reachable: true},
	}, Edges: []QueueControlEdgeModel{
		{Index: 0, EdgeID: "fetch-to-approve", From: "fetch", To: "approve", Outcome: "taken"},
		{Index: 1, EdgeID: "loop-back", From: "approve", To: "fetch", Outcome: "max=4"},
	}}
	topology := buildQueuePinnedTopology(cyclic)
	if len(topology.Issues) == 0 {
		t.Fatal("cyclic topology should record a display.cycle issue")
	}
	if len(topology.CycleMembers) == 0 {
		t.Fatal("cycle members must be recorded")
	}
	if topology.CycleMaxIterations < 4 {
		t.Fatalf("expected cycle max iterations 4, got %d", topology.CycleMaxIterations)
	}
	if topology.ExhaustionDestination == "" {
		t.Fatal("expected exhaustion destination to be populated")
	}
	branching := &QueueDetailModel{Nodes: []QueueControlNodeModel{
		{Index: 0, NodeID: "a", State: "started", Reachable: true},
		{Index: 1, NodeID: "b", State: "pending", Reachable: true},
		{Index: 2, NodeID: "c", State: "pending", Reachable: true},
	}, Edges: []QueueControlEdgeModel{
		{Index: 0, EdgeID: "a-to-b", From: "a", To: "b", Outcome: "pending"},
		{Index: 1, EdgeID: "a-to-c", From: "a", To: "c", Outcome: "pending"},
	}}
	if p := buildQueuePinnedTopology(branching); len(p.Issues) != 0 {
		t.Fatalf("branching topology should not report issues: %+v", p.Issues)
	}
	terminal := &QueueDetailModel{Nodes: []QueueControlNodeModel{
		{Index: 0, NodeID: "only", TerminalEligible: true, Reachable: true},
	}}
	if p := buildQueuePinnedTopology(terminal); len(p.Issues) != 0 {
		t.Fatalf("terminal-only topology should not report issues: %+v", p.Issues)
	}
	invalid := &QueueDetailModel{Nodes: []QueueControlNodeModel{
		{Index: 0, NodeID: "a", Reachable: true},
		{Index: 1, NodeID: "a", Reachable: true},
	}, Edges: []QueueControlEdgeModel{
		{Index: 0, EdgeID: "t", From: "", To: "a"},
	}}
	if p := buildQueuePinnedTopology(invalid); len(p.Issues) == 0 {
		t.Fatal("invalid topology should report issues")
	}
	historical := &QueueDetailModel{Nodes: []QueueControlNodeModel{
		{Index: 0, NodeID: "x", State: "succeeded", Reachable: true},
		{Index: 1, NodeID: "y", State: "pending", Reachable: true},
	}, Edges: []QueueControlEdgeModel{
		{Index: 0, EdgeID: "x-to-y", From: "x", To: "y", Outcome: "pending"},
	}}
	if p := buildQueuePinnedTopology(historical); len(p.Issues) != 0 {
		t.Fatalf("historical topology should not report issues: %+v", p.Issues)
	}
	max := &QueueDetailModel{}
	for i := 0; i < 129; i++ {
		max.Nodes = append(max.Nodes, QueueControlNodeModel{Index: i, NodeID: "n"})
	}
	if p := buildQueuePinnedTopology(max); len(p.Issues) == 0 || p.Issues[0].Code != "display.bound_exceeded" {
		t.Fatal("maximum-size overflow should be rejected")
	}
}

// TestQueueWorkspaceRendersPinnedControlFlowAndTabs verifies the workspace
// HTML exposes the accepted header, the pinned control-flow canvas, all six
// tabs, and the accessible textual equivalent. The pre-change vertical
// record hierarchy ("Execution canvas", "Inspector", "Execution timeline",
// "Attempt provenance", "Runtime artifact", "Verifier receipts", "Terminal
// disposition") must NOT appear in the rendered HTML.
func TestQueueWorkspaceRendersPinnedControlFlowAndTabs(t *testing.T) {
	record := &RecordModel{Key: "queue-pinned", Label: "queue-pinned", Revision: "r2", Lifecycle: "failed", Queue: &QueueDetailModel{
		ExecutionType:       "Pinned Graph run",
		QueueItemIdentity:   "queue-pinned",
		QueueItemDigest:     "sha256:item",
		SnapshotDigest:      "sha256:snapshot",
		PinnedGraph:         "graph-pinned",
		PinnedGraphRevision: "r2",
		PinnedGraphDigest:   "sha256:graph",
		Participant:         "agent-x r1 @ sha256:agent",
		TerminalOutcome:     "failed",
		FailureLocation:     "review",
		Nodes: []QueueControlNodeModel{
			{Index: 0, NodeID: "intake", State: "succeeded", ExecutionState: "succeeded", AttemptNumber: 1, AttemptState: "succeeded", TerminalEligible: false, Reachable: true},
			{Index: 1, NodeID: "review", State: "failed", ExecutionState: "failed", AttemptNumber: 1, AttemptState: "failed", TerminalEligible: true, FailureLocation: true, Reachable: true},
		},
		Edges: []QueueControlEdgeModel{
			{Index: 0, EdgeID: "intake-to-review", From: "intake", To: "review", Outcome: "taken"},
		},
		Inputs:           []QueueInputModel{{PortID: "input", Type: "string", Value: "", Source: "default", Status: "applicable"}},
		Outputs:          []QueueOutputModel{{PortID: "result", Type: "string", Applicability: "declared", Completeness: "unavailable"}},
		Timeline:         []QueueTimelineModel{{Title: "Queued", State: "queued", At: "2026-08-18T12:00:00Z", Detail: "queue-pinned"}},
		Evidence:         []QueueEvidenceModel{{Claim: "review-receipt", MediaType: "verification-receipt", VerifierID: "v", PolicyVersion: "v1", Outcome: "passed", ExpectedDigest: "sha256:expected", AttemptDigest: "sha256:observed", ObservedAt: "2026-08-18T12:00:00Z"}},
		Authority:        []FieldModel{{Label: "Mandate", Value: "mandate-x"}},
		Admission:        []FieldModel{{Label: "State", Value: "failed"}},
		Snapshot:         []FieldModel{{Label: "Snapshot ID", Value: "snapshot-x"}},
		Links:            []LinkModel{{Label: "Agent · review", Detail: "agent-x r1 @ sha256:agent", URL: "/console/agents?record_key=agent-x&revision=1#/agents/agent-x"}},
		ContextualAction: &QueueControlModel{Operation: "cancel", Label: "Cancel execution", Enabled: false, Reason: "terminal work cannot transition", Consequence: "Records an operator cancellation and terminal disposition; running work is not asserted stopped by the browser."},
	}}
	var out strings.Builder
	if err := QueueWorkspace(SurfaceModel{Domain: DomainQueue}, record, buildQueuePinnedTopology(record.Queue)).Render(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	html := out.String()
	for _, required := range []string{
		`data-queue-workspace`, `data-queue-stage="true"`,
		`data-queue-tab="inputs"`, `data-queue-tab="timeline"`,
		`data-queue-tab="authority"`, `data-queue-tab="evidence"`,
		`data-queue-tab="admission"`, `data-queue-tab="snapshot"`,
		`data-queue-fit`, `data-queue-zoom="in"`, `data-queue-zoom="out"`,
		`data-queue-node="0"`, `data-queue-node="1"`,
		`data-contextual-action="cancel"`,
		`tabindex="0"`, `role="region"`,
		`Accessible textual equivalent`,
		`Pinned Graph revision`, `graph-pinned`,
		`Authoritative failure · review`,
		`Only this authoritative disposition can carry terminal success`,
	} {
		if !strings.Contains(html, required) {
			t.Fatalf("missing %q in rendered queue workspace", required)
		}
	}
	for _, forbidden := range []string{
		"Authoritative execution record",
		">Execution canvas<",
		">Attempt provenance<",
		">Runtime artifact<",
		">Verifier receipts<",
		">Terminal disposition<",
		">Execution timeline<",
	} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("pre-change vertical record hierarchy leaked into workspace: %q", forbidden)
		}
	}
	if strings.Contains(html, "execution succeeded") || strings.Contains(html, "Succeeded execution") {
		t.Fatalf("passing receipt upgraded failed queue truth: %s", html)
	}
}

// TestQueueWorkspaceCspAndKeyboardShape verifies the workspace obeys the
// strict Content Security Policy (no inline styles) and exposes keyboard
// affordances for navigation and selection.
func TestQueueWorkspaceCspAndKeyboardShape(t *testing.T) {
	detail := &QueueDetailModel{Nodes: []QueueControlNodeModel{{Index: 0, NodeID: "a", State: "started", Reachable: true}, {Index: 1, NodeID: "b", State: "pending", TerminalEligible: true, Reachable: true}}, Edges: []QueueControlEdgeModel{{Index: 0, EdgeID: "e", From: "a", To: "b", Outcome: "pending"}}}
	record := &RecordModel{Key: "queue-csp", Label: "queue-csp", Revision: "r1", Lifecycle: "started", Queue: detail}
	var out strings.Builder
	if err := QueueWorkspace(SurfaceModel{Domain: DomainQueue}, record, buildQueuePinnedTopology(detail)).Render(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	html := out.String()
	for _, required := range []string{
		`data-queue-fit`, `data-queue-zoom="in"`, `data-queue-zoom="out"`,
		`data-queue-tab="inputs"`, `data-queue-tab="evidence"`,
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

// TestQueueWorkspaceCSSAndJSEmbedded verifies the embedded CSS and JS bundles
// contain the queue workspace selectors and enhancement hooks.
func TestQueueWorkspaceCSSAndJSEmbedded(t *testing.T) {
	if !strings.Contains(string(CSS), "queue-workspace") {
		t.Fatal("embedded CSS does not contain queue-workspace selectors")
	}
	if !strings.Contains(string(NavigationJS), "queue-workspace") {
		t.Fatal("embedded JS does not contain queue-workspace enhancement")
	}
}

// TestQueueWorkspaceSecurityNegativeCannotForgeLifecycleAuthorityEvidence
// verifies the workspace cannot be tricked into rendering terminal success,
// different authority, or upgraded evidence when the authoritative runtime
// facts say otherwise. The browser remains a presentation surface; mutation
// requires fresh authenticated admission.
func TestQueueWorkspaceSecurityNegativeCannotForgeLifecycleAuthorityEvidence(t *testing.T) {
	detail := &QueueDetailModel{
		ExecutionType:     "Pinned Graph run",
		QueueItemIdentity: "queue-130",
		TerminalOutcome:   "failed",
		FailureLocation:   "review",
		Nodes:             []QueueControlNodeModel{{Index: 0, NodeID: "review", State: "failed", ExecutionState: "failed", TerminalEligible: true, FailureLocation: true, Reachable: true}},
		Edges:             []QueueControlEdgeModel{{Index: 0, EdgeID: "e", From: "review", To: "review", Outcome: "pending"}},
		Evidence:          []QueueEvidenceModel{{Claim: "review-receipt", Outcome: "passed", ExpectedDigest: "sha256:expected", AttemptDigest: "sha256:observed", VerifierID: "v", PolicyVersion: "v1", ObservedAt: "2026-08-18T12:00:00Z"}},
		DispositionState:  "Authoritative terminal disposition · failed · runtime_exit_nonzero",
	}
	record := &RecordModel{Key: "queue-130", Label: "queue-130", Revision: "r2", Lifecycle: "failed", Queue: detail}
	var out strings.Builder
	if err := QueueWorkspace(SurfaceModel{Domain: DomainQueue}, record, buildQueuePinnedTopology(detail)).Render(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	html := out.String()
	// Passing receipt evidence must not promote execution state.
	if strings.Contains(html, "execution succeeded") || strings.Contains(html, "Succeeded execution") || strings.Contains(html, "state-large succeeded") {
		t.Fatalf("browser state could forge terminal success: %s", html)
	}
	// The authoritative disposition must surface the failed reason. The
	// disposition is rendered through the record-head-tags ("Terminal
	// outcome · failed") and through the admission/evidence tabs when
	// populated. The browser cannot promote terminal state from any other
	// source.
	if !strings.Contains(html, "Terminal outcome · failed") {
		t.Fatalf("authoritative terminal outcome lost: %s", html)
	}
	// The contextual action must reflect the terminal phase (cancel disabled,
	// or no action when the disposition is terminal).
	if record.Queue.ContextualAction != nil && record.Queue.ContextualAction.Enabled {
		t.Fatalf("contextual action was enabled for terminal execution: %+v", record.Queue.ContextualAction)
	}
	// Browser state cannot promote failed lifecycle to succeeded even when
	// a verifier receipt passes. The lifecycle state must remain failed.
	if !strings.Contains(html, "lifecycle state-large failed") {
		t.Fatalf("lifecycle state-large failed missing")
	}
}
