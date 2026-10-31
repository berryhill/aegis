package delivery

import (
	"context"
	"testing"
)

// These are controlled orchestration regressions, not managed-provider or GitHub
// acceptance. A successful handoff must survive release of accepted-source
// custody: a terminal Queue boundary would make the repair impossible.
type repairBoundaryCompletion struct{ boundaries int }

type heldRepair struct{}

func (heldRepair) RequestRepair(context.Context, RepairFailure) error { return ErrDenied }

func (c *repairBoundaryCompletion) VerifyContributionCompletion(context.Context, Reference) (string, error) {
	return "", ErrDenied
}
func (c *repairBoundaryCompletion) FinalizeContributionBoundary(context.Context, Reference) error {
	c.boundaries++
	return nil
}

func TestConductorRepairHandoffDoesNotTerminalize(t *testing.T) {
	for _, code := range []string{"ci_failed", "merge_conflict"} {
		t.Run(code, func(t *testing.T) {
			p := fixturePlan()
			completion := &repairBoundaryCompletion{}
			repair := &repairCapture{}
			adapter := &sequenceAdapter{held: CheckDelivery, outcome: Failed, code: code}
			c := NewConductor(NewEngine(&memoryOwner{}, &sequenceSource{p: p}, &fixtureAuthority{}, adapter), repair, completion)
			result, err := c.Run(context.Background(), p.Reference)
			if err != nil || result.Reason != "repair_requested" || len(repair.failures) != 1 {
				t.Fatalf("handoff: result=%+v err=%v failures=%+v", result, err, repair.failures)
			}
			if completion.boundaries != 0 {
				t.Fatalf("repair handoff terminalized the retained Queue: boundaries=%d", completion.boundaries)
			}
		})
	}
}

func TestConductorMergedFailureCannotRequestRepair(t *testing.T) {
	p := fixturePlan()
	repair := &repairCapture{}
	adapter := &sequenceAdapter{held: Merge, outcome: Failed, code: "merge_conflict"}
	c := NewConductor(NewEngine(&memoryOwner{}, &sequenceSource{p: p}, &fixtureAuthority{}, adapter), repair, nil)
	result, err := c.Run(context.Background(), p.Reference)
	if err != nil || result.Merge == "" || result.Reason != "merged_unresolved" || len(repair.failures) != 0 {
		t.Fatalf("irreversible merge escaped into repair: result=%+v err=%v failures=%+v", result, err, repair.failures)
	}
}

func TestConductorUnadmittedRepairKeepsBoundaryDisposition(t *testing.T) {
	for _, tc := range []struct {
		name   string
		owner  RepairOwner
		reason string
	}{
		{"missing", nil, "repair_owner_unavailable"},
		{"held", heldRepair{}, "repair_held"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := fixturePlan()
			completion := &repairBoundaryCompletion{}
			adapter := &sequenceAdapter{held: CheckDelivery, outcome: Failed, code: "ci_failed"}
			c := NewConductor(NewEngine(&memoryOwner{}, &sequenceSource{p: p}, &fixtureAuthority{}, adapter), tc.owner, completion)
			result, err := c.Run(context.Background(), p.Reference)
			if err != nil || result.Reason != tc.reason || completion.boundaries != 1 {
				t.Fatalf("unadmitted repair changed disposition: result=%+v err=%v boundaries=%d", result, err, completion.boundaries)
			}
		})
	}
}

func TestForwardRepairRejectsUnliveContextAndInconsistentOutcome(t *testing.T) {
	p := fixturePlan()
	i, err := intentFor(p, CheckDelivery)
	if err != nil {
		t.Fatal(err)
	}
	r := Record{Intent: i, Outcome: Failed, Observation: Observation{Outcome: Failed, Code: "ci_failed"}, Revision: 2}
	owner := &fixtureRepair{}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, ctx := range []context.Context{nil, cancelled} {
		if err := ForwardRepair(ctx, owner, r); err == nil || owner.calls != 0 {
			t.Fatalf("unlive repair context admitted: err=%v calls=%d", err, owner.calls)
		}
	}
	r.Observation.Outcome = Unknown
	if err := ForwardRepair(context.Background(), owner, r); err == nil || owner.calls != 0 {
		t.Fatalf("inconsistent failure admitted: err=%v calls=%d", err, owner.calls)
	}
}

func TestForwardRepairRequiresPreMergeCheckFailure(t *testing.T) {
	p := fixturePlan()
	for _, tc := range []struct {
		name    string
		op      Operation
		outcome Outcome
		code    string
		merge   string
		allowed bool
	}{
		{"ci", CheckDelivery, Failed, "ci_failed", "", true},
		{"conflict", CheckDelivery, Failed, "merge_conflict", "", true},
		{"merge_conflict", Merge, Failed, "merge_conflict", "", true},
		{"unknown", CheckDelivery, Unknown, "ci_failed", "", false},
		{"denied", CheckDelivery, Denied, "ci_failed", "", false},
		{"post_merge", Merge, Failed, "merge_conflict", p.Head, false},
		{"commit", Commit, Failed, "ci_failed", "", false},
		{"publish", Publish, Failed, "ci_failed", "", false},
		{"pr", PullRequest, Failed, "ci_failed", "", false},
		{"issue", ReconcileIssue, Failed, "ci_failed", "", false},
		{"infrastructure", CheckDelivery, Failed, "remote_unavailable", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i, err := intentFor(p, tc.op)
			if err != nil {
				t.Fatal(err)
			}
			r := Record{Intent: i, Outcome: tc.outcome, Observation: Observation{Outcome: tc.outcome, Code: tc.code, Merge: tc.merge}, Revision: 2}
			owner := &repairCapture{}
			ctx := context.WithValue(context.Background(), sequenceKey{}, true)
			err = ForwardRepair(ctx, owner, r)
			if (err == nil) != tc.allowed || (len(owner.failures) == 1) != tc.allowed {
				t.Fatalf("allowed=%t err=%v failures=%+v", tc.allowed, err, owner.failures)
			}
		})
	}
}
