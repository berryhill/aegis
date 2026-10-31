package consoleweb

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestQueueOutcomeRecordedProductionReasons(t *testing.T) {
	// Exact terminal disposition codes from doer_runtime.go and queue_lifecycle.go.
	for _, tt := range []struct{ state, reason string }{
		{"failed", "doer_loop_failed"},
		{"succeeded", "doer_verified"},
		{"cancelled", "operator_cancelled"},
		{"expired", "execution_expired"},
		{"revoked", "authority_revoked"},
	} {
		t.Run(tt.reason, func(t *testing.T) {
			if got := SafeQueueOutcomeReason(tt.reason); got != tt.reason {
				t.Errorf("recorded reason = %q, want exact %q", got, tt.reason)
			}
			var b bytes.Buffer
			q := &QueueDetailModel{TerminalOutcome: tt.state, DispositionReason: tt.reason}
			if err := QueueOutcome(q).Render(context.Background(), &b); err != nil {
				t.Fatal(err)
			}
			text := b.String()
			if !strings.Contains(text, "Recorded execution reason · <code>"+tt.reason+"</code>") {
				t.Errorf("missing exact recorded reason: %s", text)
			}
			if strings.Contains(text, "verified selected-file evidence") {
				t.Error("reason alone established verification")
			}
			if tt.state == "failed" && !strings.Contains(text, "A more detailed cause was not recorded or is unavailable") {
				t.Error("root reason incorrectly replaced unavailable underlying detail")
			}
		})
	}
}

func TestQueueOutcomeReasonRejectsUnreviewedStrings(t *testing.T) {
	for _, reason := range []string{
		"", "SECRET backend stderr <script>", "doer_future_failure", "doer_verified SECRET",
		"doer_loop_failed\nSECRET", " doer_verified", "DOER_VERIFIED", "operator_cancelled SECRET",
		"runtime_retry", "lease_reclaimed", "worker lease acquired",
	} {
		t.Run(reason, func(t *testing.T) {
			if got := SafeQueueOutcomeReason(reason); got != "unavailable" {
				t.Fatalf("unreviewed reason exposed: %q", got)
			}
			var b bytes.Buffer
			if err := QueueOutcome(&QueueDetailModel{TerminalOutcome: "failed", DispositionReason: reason}).Render(context.Background(), &b); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(b.String(), "Recorded execution reason · <code>unavailable</code>") || strings.Contains(b.String(), "SECRET") {
				t.Fatal("unknown/backend reason was not withheld")
			}
		})
	}
}
