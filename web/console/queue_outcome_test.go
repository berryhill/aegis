package consoleweb

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestQueueOutcomeObservationSemantics(t *testing.T) {
	for _, state := range []string{"queued", "claimed", "running", "succeeded", "failed"} {
		q := &QueueDetailModel{CurrentState: state}
		var b bytes.Buffer
		if err := QueueOutcome(q).Render(context.Background(), &b); err != nil {
			t.Fatal(err)
		}
		text := b.String()
		want := state
		if state == "succeeded" || state == "failed" {
			want = "unobserved"
		}
		if !strings.Contains(text, "Current execution outcome · "+want) || strings.Contains(text, "records success") || strings.Contains(text, "records execution failure") {
			t.Fatal(text)
		}
	}
	q := &QueueDetailModel{TerminalOutcome: "succeeded", DispositionReason: "evidence_satisfied"}
	for _, verified := range []bool{false, true} {
		q.RunResult = &QueueRunResultModel{Verified: verified}
		var b bytes.Buffer
		if err := QueueOutcome(q).Render(context.Background(), &b); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(b.String(), "verified selected-file evidence") != verified || strings.Contains(b.String(), "A more detailed cause") {
			t.Fatal(b.String())
		}
	}
}

func TestQueueOutcomeSafeDisposition(t *testing.T) {
	for _, tt := range []struct{ state, reason, want string }{
		{"failed", "implementation_failed", "A more detailed cause was not recorded"},
		{"failed", "secret stderr <script>", "unavailable"},
		{"succeeded", "evidence_satisfied", "records success"},
		{"cancelled", "", "cancelled"},
		{"denied", "", "denied"},
		{"expired", "", "expired"},
		{"revoked", "", "revoked"},
		{"claimed", "", "No authoritative terminal disposition"},
	} {
		t.Run(tt.state+tt.reason, func(t *testing.T) {
			q := &QueueDetailModel{TerminalOutcome: tt.state, DispositionReason: tt.reason}
			var b bytes.Buffer
			if err := QueueOutcome(q).Render(context.Background(), &b); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(b.String(), tt.want) {
				t.Fatalf("missing %q: %s", tt.want, b.String())
			}
			if strings.Contains(b.String(), "secret stderr") || strings.Contains(b.String(), "<script>") {
				t.Fatal("raw diagnostic leak")
			}
		})
	}
}
