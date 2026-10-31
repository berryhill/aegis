package app

import (
	"context"
	"testing"
	"time"
)

func TestContributionInvocationRetainsOriginalDeadlineAfterDisconnect(t *testing.T) {
	now := time.Now().UTC()
	s, _, sub, ref, reader := contributionFixtureAt(t, now)
	c, err := s.admitContributionAs(context.Background(), sub, "original", ref, reader.source.URL.URL, "invocation-deadline", "local-only", reader)
	if err != nil {
		t.Fatal(err)
	}
	short := time.Now().Add(time.Until(c.Deadline) / 2)
	for _, tc := range []struct {
		name  string
		bound time.Time
		want  time.Time
	}{
		{"original-manager-bound", short, short},
		{"contract-bound", c.Deadline.Add(time.Minute), c.Deadline},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caller, abort := context.WithDeadline(context.Background(), tc.bound)
			defer abort()
			run, finish, err := s.beginContributionRun(caller, c)
			if err != nil {
				t.Fatal(err)
			}
			defer finish()
			bound, ok := run.Deadline()
			if !ok || !bound.Equal(tc.want) {
				t.Fatalf("invocation widened original deadline: got %s, want %s", bound, tc.want)
			}
			abort()
			if run.Err() != nil {
				t.Fatal("transport cancellation ended still-authorized invocation", run.Err())
			}
		})
	}
}
