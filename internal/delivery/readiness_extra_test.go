package delivery

import (
	"context"
	"testing"
	"time"
)

func TestReadinessReviewPendingAndFailureForwarding(t *testing.T) {
	for _, review := range []bool{false, true} {
		t.Run(map[bool]string{false: "hard-CI-failure", true: "review-pending"}[review], func(t *testing.T) {
			g, f, p := newGithubFixture(t, objectPlan(t))
			objects := serveObjects(t, p, &objectServer{})
			if review {
				f.review = "COMMENTED"
			} else {
				f.check = "failure"
			}
			p.Intake.Deadline = time.Now().Add(time.Minute)
			s := &sequenceSource{p: p}
			b := &qualificationReadinessBudget{requirePrivate: true}
			repair := &repairCapture{}
			out, e := NewConductor(NewEngine(&memoryOwner{}, s, &fixtureAuthority{}, syntheticHTTPPipeline{g, objects}), repair, nil, WithReadiness(readinessPolicy(t, time.Millisecond), b)).Run(context.Background(), p.Reference)
			if e != nil || out.Held != CheckDelivery || f.mergeWrites != 0 || out.Completion != "" {
				t.Fatal(out, e)
			}
			if review {
				if out.Reason != "readiness_budget_exhausted" || b.used[p.Run] != 2 || len(repair.failures) != 0 {
					t.Fatal(out, b.used, repair)
				}
			} else {
				if out.Reason != "repair_requested" || len(repair.failures) != 1 || b.used[p.Run] != 0 {
					t.Fatal(out, repair)
				}
			}
		})
	}
}
func TestReadinessMissingOwnerAndDeadline(t *testing.T) {
	for _, kind := range []string{"missing-budget", "expired", "delay-beyond-deadline"} {
		t.Run(kind, func(t *testing.T) {
			p := fixturePlan()
			p.Intake.Deadline = time.Now().Add(time.Minute)
			delay := time.Duration(0)
			b := &qualificationReadinessBudget{}
			var budget ReadinessBudget = b
			want := "readiness_budget_unavailable"
			switch kind {
			case "missing-budget":
				budget = nil
			case "expired":
				p.Intake.Deadline = time.Now().Add(-time.Second)
				want = "readiness_deadline"
			case "delay-beyond-deadline":
				p.Intake.Deadline = time.Now().Add(20 * time.Millisecond)
				delay = time.Second
				want = "readiness_deadline"
			}
			a := &sequenceAdapter{held: CheckDelivery, outcome: Unknown, code: "ci_gate"}
			owner := &memoryOwner{}
			s := &sequenceSource{p: p}
			c := NewConductor(NewEngine(owner, s, &fixtureAuthority{}, a), nil, nil, WithReadiness(readinessPolicy(t, delay), budget))
			_, e := c.Run(context.Background(), p.Reference)
			if e != nil {
				t.Fatal(e)
			}
			before := len(a.ops)
			out, e := c.Run(context.Background(), p.Reference)
			if e != nil || out.Reason != want || len(a.ops) != before || len(b.intents) != 0 {
				t.Fatal(out, e, a.ops)
			}
		})
	}
}
func TestReadinessUnknownMergeWriteSameIntent(t *testing.T) {
	g, f, p := newGithubFixture(t, objectPlan(t))
	objects := serveObjects(t, p, &objectServer{})
	f.lost = true
	p.Intake.Deadline = time.Now().Add(time.Minute)
	owner := &memoryOwner{}
	c := NewConductor(NewEngine(owner, &sequenceSource{p: p}, &fixtureAuthority{}, syntheticHTTPPipeline{g, objects}), nil, nil)
	first, e := c.Run(context.Background(), p.Reference)
	f.Lock()
	firstWrites := f.mergeWrites
	f.Unlock()
	if e != nil || first.Held != Merge || first.Merge != "" || firstWrites != 1 {
		t.Fatal(first, e)
	}
	second, e := c.Run(context.Background(), p.Reference)
	if e != nil || second.Held != ReconcileIssue || second.Merge == "" || f.mergeWrites != 1 {
		t.Fatal(second, e)
	}
	var a, b Intent
	for _, r := range first.Effects {
		if r.Intent.Operation == Merge {
			a = r.Intent
		}
	}
	for _, r := range second.Effects {
		if r.Intent.Operation == Merge {
			b = r.Intent
		}
	}
	if a != b || a.ID == "" {
		t.Fatal("replaced unknown write intent", a, b)
	}
}
