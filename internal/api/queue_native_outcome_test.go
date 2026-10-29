package api

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/orchestration"
	consoleweb "github.com/berryhill/aegis/web/console"
)

// Projection-only controlled evidence; actual native/controller acceptance is
// exercised separately by TestAuthenticatedCoding*CorrectionRequeue.
func TestQueueNativeOutcomeProjection(t *testing.T) {
	for _, success := range []bool{false, true} {
		state, reason := "failed", "implementation_failed"
		p := orchestration.CodingPassView{ID: "queue/pass/1", Failure: &orchestration.CodingFailureView{State: "failed", Code: "runtime_provider_timeout"}}
		if success {
			state, reason = "succeeded", "implementation_verified"
			p.Passed = true
			p.Failure = nil
		}
		h := &orchestration.CodingHistoryView{RunID: "queue", State: state, Digest: "history", ContractDigest: "contract", ReservedPasses: 1, CumulativeConsumed: 1, RemainingBudget: 1, Passes: []orchestration.CodingPassView{p}}
		d := app.ExecutionDebugger{QueueID: "queue", GraphRunID: "run", Direct: true, State: state, Terminal: true, DispositionID: "disposition", DispositionReason: reason, DispositionAttemptID: "attempt", DispositionExecutionID: "execution", ArtifactIDs: []string{"artifact"}, ReceiptIDs: []string{"receipt"}, Nodes: []app.DebuggerLoop{{ExecutionID: "execution", Attempts: []app.DebuggerAttempt{{ID: "attempt", Coding: h}}}}}
		render := func(v app.ExecutionDebugger) string {
			t.Helper()
			q := &consoleweb.QueueDetailModel{TerminalOutcome: state, DispositionReason: reason}
			applyQueueDebugger(q, v)
			var b bytes.Buffer
			if err := consoleweb.QueueOutcome(q).Render(context.Background(), &b); err != nil {
				t.Fatal(err)
			}
			return b.String()
		}
		d.NativeEvidenceVerified = success
		html := render(d)
		want := "runtime_provider_timeout"
		if success {
			want = "trusted offline go-test.v1"
		}
		if !strings.Contains(html, want) || !strings.Contains(html, "queue/pass/1") || !strings.Contains(html, "attempt") {
			t.Fatalf("native outcome missing: %s", html)
		}
		if strings.Contains(html, "verified selected-file evidence") || (!success && strings.Contains(html, "A more detailed cause was not recorded")) {
			t.Fatal(html)
		}
		for _, mutate := range []func(*app.ExecutionDebugger){
			func(v *app.ExecutionDebugger) { v.DispositionAttemptID = "other" },
			func(v *app.ExecutionDebugger) { v.DispositionExecutionID = "other" },
			func(v *app.ExecutionDebugger) { v.QueueID = "other" },
			func(v *app.ExecutionDebugger) { v.Terminal = false },
		} {
			v := d
			mutate(&v)
			if strings.Contains(render(v), want) {
				t.Fatal("foreign/nonterminal native evidence accepted")
			}
		}
		if !success {
			h.Passes[0].OutputDigest = "output-present"
			h.Passes[0].WorkspaceDigest = "workspace-present"
			h.Passes[0].Failure.Code = "secret-error-canary"
			h.Passes[0].Failure.Reason = "secret-reason-canary"
			h.Passes[0].Failure.State = "secret-state-canary"
			html = render(d)
			if strings.Contains(html, "secret-") || !strings.Contains(html, "A more detailed cause was not recorded") {
				t.Fatal(html)
			}
		}
	}
}
