package evidence

import "testing"

func TestContributionTerminalOutcomeMapping(t *testing.T) {
	cases := map[string]string{"succeeded": "succeeded", "narrower-target-completed": "succeeded", "failed": "failed", "denied": "denied", "cancelled": "cancelled", "expired": "expired", "exhausted": "failed", "unknown": "failed", "merged-unresolved": "failed"}
	for outcome, want := range cases {
		state, reason, ok := ContributionTerminalMapping(outcome)
		if !ok || state != want || reason == "" {
			t.Fatal(outcome, state, reason, ok)
		}
		if outcome != "succeeded" && outcome != "narrower-target-completed" && state == "succeeded" {
			t.Fatal("non-success promoted")
		}
	}
	for _, unknown := range []string{"PASS", "pending", "completed", ""} {
		if _, _, ok := ContributionTerminalMapping(unknown); ok {
			t.Fatal("unknown terminal mapping", unknown)
		}
	}
}
