package api

import (
	"context"
	"github.com/berryhill/aegis/internal/app"
	"testing"
)

func TestContributionDecisionDeliveryComposition(t *testing.T) {
	for _, outcome := range []string{"approved", "unavailable", "timeout", "dialog-cancelled", "nonsense"} {
		t.Run(outcome, func(t *testing.T) {
			s, _, sub, ref, reader := decisionHTTPFixture(t)
			s.ContributionReader = reader
			ctx := context.Background()
			c, err := s.AdmitContributionAs(ctx, sub, "original", ref, reader.source.URL.URL, "composition", "local-only")
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			launch := func(_ context.Context, in app.ContributionDecisionInput) string {
				calls++
				_, state, _, err := s.ObserveContributionContinuityAs(ctx, sub, c.Key)
				if err != nil || state.Used.DecisionDeliveries != 1 {
					t.Fatal("not reserved before launch", state, err)
				}
				// Real native review reuses the exact reserved plan without spending twice.
				if _, err := s.PrepareContributionDecisionAs(ctx, sub, in); err != nil {
					t.Fatal(err)
				}
				return outcome
			}
			want := outcome
			if outcome == "approved" || outcome == "nonsense" {
				want = "unknown"
			}
			for i := 0; i < 2; i++ {
				got, err := s.DeliverContributionDecisionAs(ctx, sub, c.Key, "controller", launch)
				if err != nil || got != want {
					t.Fatal(got, err)
				}
			}
			if calls != 1 {
				t.Fatal("reprompt", calls)
			}
			_, state, _, err := s.ObserveContributionContinuityAs(ctx, sub, c.Key)
			if err != nil || state.Used.DecisionDeliveries != 1 || state.Used.QueueAttempts != 0 {
				t.Fatal(state, err)
			}
			approvals, _ := s.ListApprovals()
			receipts, _ := s.ListReceipts()
			if len(approvals) != 0 || len(receipts) != 0 {
				t.Fatal("claimed UI status authorized provisioning")
			}
			_, execution, err := s.ReadContributionAs(ctx, sub, c.Key)
			if err != nil || len(execution.Attempts) != 0 {
				t.Fatal("claimed execution", err)
			}
		})
	}
}

func TestContributionDecisionOwningPortUnavailable(t *testing.T) {
	s, _, sub, ref, reader := decisionHTTPFixture(t)
	s.ContributionReader = reader
	ctx := context.Background()
	c, err := s.AdmitContributionAs(ctx, sub, "original", ref, reader.source.URL.URL, "owning-port", "local-only")
	if err != nil {
		t.Fatal(err)
	}
	// Missing pinned transport must not create a human-pending or successful result.
	s.Config.API.UnixSocket = ""
	installContributionDecisionDelivery(s)
	for i := 0; i < 2; i++ {
		got, err := s.ContributionDecisionDelivery(ctx, sub, c.Key, "controller")
		if err != nil || got != "unavailable" {
			t.Fatal(got, err)
		}
	}
	_, state, _, err := s.ObserveContributionContinuityAs(ctx, sub, c.Key)
	if err != nil || state.Used.DecisionDeliveries != 1 || state.Used.QueueAttempts != 0 {
		t.Fatal(state, err)
	}
	approvals, _ := s.ListApprovals()
	receipts, _ := s.ListReceipts()
	if len(approvals) != 0 || len(receipts) != 0 {
		t.Fatal("unavailable provisioned")
	}
}
