package api

import (
	"context"
	"encoding/json"
	"testing"
)

// This trusted regression consumes the real pinned durable fixture, which has
// already published revision 2. It is outside the managed writer allowlist.
func TestIssue330PinnedTransitionPresentation(t *testing.T) {
	for _, graph := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct", true: "graph-child"}[graph], func(t *testing.T) {
			svc, subject, rev, executor, done := debuggerFixtureKind(t, graph)
			first, err := svc.ExecutionDebuggerAs(context.Background(), subject, "debug-queue", "")
			if err != nil {
				t.Fatal(err)
			}
			check := func(v any) []map[string]any {
				t.Helper()
				raw, err := json.Marshal(v)
				if err != nil {
					t.Fatal(err)
				}
				var body map[string]any
				if err = json.Unmarshal(raw, &body); err != nil {
					t.Fatal(err)
				}
				n := body["nodes"].([]any)[0].(map[string]any)
				pinned := n["loop"].(map[string]any)
				if pinned["digest"] != rev.Digest || pinned["revision"] != float64(1) {
					t.Fatalf("not pinned: %v", pinned)
				}
				edges := n["edges"].([]any)
				out := make([]map[string]any, len(edges))
				for i, value := range edges {
					out[i] = value.(map[string]any)
				}
				return out
			}
			before := check(first)
			// Conditions and limits are definition context, not inferred observations.
			for _, want := range rev.Transitions {
				found := false
				for _, got := range before {
					if got["id"] == want.ID {
						found = true
						if want.Condition != "" && got["condition"] != want.Condition {
							t.Errorf("%s condition lost: %v want %q", want.ID, got, want.Condition)
						}
						if want.MaxTraversals > 0 && got["limit"] != float64(want.MaxTraversals) {
							t.Errorf("%s bound lost: %v", want.ID, got)
						}
					}
				}
				if !found {
					t.Errorf("missing transition %s", want.ID)
				}
			}
			close(executor.release)
			if err = <-done; err != nil {
				t.Fatal(err)
			}
			later, err := svc.ExecutionDebuggerAs(context.Background(), subject, "debug-queue", first.Watermark)
			if err != nil {
				t.Fatal(err)
			}
			a, _ := json.Marshal(before)
			b, _ := json.Marshal(check(later))
			if string(a) != string(b) {
				t.Fatal("observations changed pinned transitions")
			}
			if later.Terminal {
				t.Fatal("interpreter terminal upgraded Queue disposition")
			}
		})
	}
}
