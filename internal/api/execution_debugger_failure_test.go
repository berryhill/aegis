package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/looprun"
)

// Synthetic append-only cursor histories test the production authenticated
// application projection, not a real provider failure or Queue disposition.
func TestExecutionDebuggerFailureReasonsStayOnExactBoundary(t *testing.T) {
	svc, subject, rev, executor, done := debuggerFixture(t)
	close(executor.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	custody := svc.FleetRepository.(interface{ ImplementationStore() implementation.Store })
	store := implementation.StepCheckpointStore{DB: custody.ImplementationStore(), RunID: "debug-attempt", RevisionDigest: rev.Digest}
	ctx := context.Background()
	history, err := store.History(ctx)
	if err != nil {
		t.Fatal(err)
	}
	previous := history[len(history)-1].Digest
	failures := []looprun.Failure{}
	for i, tc := range []struct {
		step, outcome, code, want string
		pass                      uint16
	}{
		{"implement", "failed", "doer_proposal_json_invalid", "doer_proposal_json_invalid", 1},
		{"implement", "succeeded", "", "", 2},
		{"verify", "succeeded", "", "", 2},
		{"implement", "failed", "private-secret-canary", "step_error", 3},
	} {
		if tc.code != "" {
			failures = append(failures, looprun.Failure{Code: tc.code, Message: "private-secret-canary"})
		}
		observation := looprun.Observation{Kind: "step_finish", StepID: tc.step, Visit: uint32(i + 1), Retry: 1, Pass: tc.pass, At: time.Now().UTC(), Outcome: tc.outcome}
		wire, _ := json.Marshal(map[string]any{"step_id": tc.step, "visits": map[string]uint32{tc.step: uint32(i + 1)}, "observations": []looprun.Observation{observation}, "failures": failures})
		checkpoint, err := store.Append(ctx, previous, wire)
		if err != nil {
			t.Fatal(err)
		}
		previous = checkpoint.Digest
		view, err := svc.ExecutionDebuggerAs(ctx, subject, "debug-queue", "")
		if err != nil {
			t.Fatal(err)
		}
		facts := view.Nodes[0].Attempts[0].Facts
		last := facts[len(facts)-1]
		if last.FailureCode != tc.want || last.StepID != tc.step || last.Pass != tc.pass || last.Sequence != checkpoint.Sequence {
			t.Fatalf("failure attribution lost at %d: %+v want %s", i, last, tc.want)
		}
		projected, _ := json.Marshal(view)
		if strings.Contains(string(projected), "private-secret-canary") {
			t.Fatal("private diagnostic disclosed")
		}
	}
}
