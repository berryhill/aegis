package api

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/orchestration"
	consoleweb "github.com/berryhill/aegis/web/console"
)

func TestIssue382GeneralOutcomeReferences(t *testing.T) {
	for _, direct := range []bool{false, true} {
		d := app.ExecutionDebugger{QueueID: "queue", Direct: direct, Terminal: true, State: "succeeded", DispositionID: "exact-disposition", ArtifactIDs: []string{"exact-artifact-a", "exact-artifact-b"}, ReceiptIDs: []string{"exact-receipt-a", "exact-receipt-b"}}
		q := &consoleweb.QueueDetailModel{TerminalOutcome: d.State, DispositionID: d.DispositionID}
		applyQueueDebugger(q, d)
		var b bytes.Buffer
		if err := consoleweb.QueueCurrentOutcome(q).Render(context.Background(), &b); err != nil {
			t.Fatal(err)
		}
		html := b.String()
		// Same server observation is delivered even when the client pauses history replay.
		wire, _ := json.Marshal(debuggerPresentation(d, html))
		for _, ref := range append(d.ArtifactIDs, d.ReceiptIDs...) {
			if !strings.Contains(html, ref) || !strings.Contains(string(wire), ref) {
				t.Fatalf("missing refreshed outcome reference %s: %s", ref, html)
			}
		}
	}
}

func TestIssue382RecordedCheckProjection(t *testing.T) {
	for _, passed := range []bool{false, true} {
		h := &orchestration.CodingHistoryView{RunID: "queue", State: "failed", Digest: "history", ContractDigest: "contract", ReservedPasses: 1, Passes: []orchestration.CodingPassView{{ID: "queue/pass/1", OutputDigest: "output"}}}
		// Decode the additive public field so the baseline compiles and fails behaviorally.
		payload := `[{"pass_id":"queue/pass/1","name":"check","digest":"stage","check_outcome":"failed","output_digest":"output"}]`
		want := "failed"
		if passed {
			payload = strings.Replace(payload, `"failed"`, `"passed"`, 1)
			want = "passed"
		}
		if err := json.Unmarshal([]byte(payload), &h.Stages); err != nil {
			t.Fatal(err)
		}
		d := app.ExecutionDebugger{QueueID: "queue", Direct: true, Terminal: true, State: "failed", DispositionID: "disp", DispositionReason: "implementation_failed", DispositionAttemptID: "attempt", DispositionExecutionID: "execution", Nodes: []app.DebuggerLoop{{ExecutionID: "execution", Attempts: []app.DebuggerAttempt{{ID: "attempt", Coding: h}}}}}
		q := &consoleweb.QueueDetailModel{TerminalOutcome: "failed"}
		applyQueueDebugger(q, d)
		var b bytes.Buffer
		if err := consoleweb.QueueOutcome(q).Render(context.Background(), &b); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(b.String(), "Recorded last-pass check · "+want) || !strings.Contains(b.String(), "not a terminal cause") {
			t.Fatal(b.String())
		}
		original := h.Stages[0]
		for _, mutate := range []func(*orchestration.CodingStageView){
			func(s *orchestration.CodingStageView) { s.PassID = "foreign/pass/1" },
			func(s *orchestration.CodingStageView) { s.OutputDigest = "foreign" },
			func(s *orchestration.CodingStageView) { s.CheckOutcome = "secret-canary" },
			func(s *orchestration.CodingStageView) { s.Name = "unknown" },
			func(s *orchestration.CodingStageView) { s.Digest = "" },
		} {
			h.Stages[0] = original
			mutate(&h.Stages[0])
			applyQueueDebugger(q, d)
			b.Reset()
			if err := consoleweb.QueueOutcome(q).Render(context.Background(), &b); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(b.String(), "Recorded last-pass check · unavailable") || strings.Contains(b.String(), "secret-canary") {
				t.Fatal(b.String())
			}
		}
		h.Stages[0] = original
	}
}
