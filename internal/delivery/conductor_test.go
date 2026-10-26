package delivery

import (
	"context"
	"reflect"
	"sync"
	"testing"
)

type sequenceAdapter struct {
	sync.Mutex
	ops     []Operation
	held    Operation
	outcome Outcome
	code    string
	lost    bool
}

func (a *sequenceAdapter) Apply(ctx context.Context, i Intent, p Plan, g Guard) Observation {
	if ctx.Value(sequenceKey{}) != true || g(ctx) != nil {
		return Observation{Outcome: Denied}
	}
	a.Lock()
	defer a.Unlock()
	a.ops = append(a.ops, i.Operation)
	o := Observation{Outcome: Confirmed, Head: i.Head}
	if i.Operation == PullRequest {
		o.PR = 7
	}
	if i.Operation == CheckDelivery || i.Operation == Merge {
		if i.PR != 7 {
			return Observation{Outcome: Denied}
		}
		o.PR = 7
	}
	if i.Operation == Merge {
		o.Merge, o.BaseHead = i.Head, i.BaseHead
	}
	if i.Operation == a.held {
		o.Outcome, o.Code = a.outcome, a.code
	}
	return o
}
func (a *sequenceAdapter) Reconcile(ctx context.Context, i Intent, p Plan, g Guard) Observation {
	if i.Operation == CheckDelivery {
		return a.Apply(ctx, i, p, g)
	}
	if g(ctx) != nil {
		return Observation{Outcome: Unknown}
	}
	o := Observation{Outcome: Confirmed, Head: i.Head}
	if i.Operation == PullRequest {
		o.PR = 7
	}
	if i.Operation == Merge {
		o.PR, o.Merge, o.BaseHead = 7, i.Head, i.BaseHead
	}
	return o
}

type sequenceKey struct{}
type sequenceSource struct {
	p Plan
	sync.Mutex
	calls int
}

func (s *sequenceSource) WithAccepted(ctx context.Context, _ Reference, f func(context.Context, Plan) error) error {
	s.Lock()
	s.calls++
	s.Unlock()
	ctx, cancel := context.WithCancel(context.WithValue(ctx, sequenceKey{}, true))
	defer cancel()
	return f(ctx, s.p)
}

type repairCapture struct{ failures []RepairFailure }

func (r *repairCapture) RequestRepair(ctx context.Context, f RepairFailure) error {
	if ctx.Value(sequenceKey{}) != true {
		return ErrDenied
	}
	r.failures = append(r.failures, f)
	return nil
}
func TestConductorSequenceRestrictionsReplayAndContext(t *testing.T) {
	for _, target := range []string{"local-only", "no-publish", "no-merge", "verified-protected-base-merge"} {
		t.Run(target, func(t *testing.T) {
			p := fixturePlan()
			p.Delivery = target
			p.PR = 999
			p.Completion = digest("untrusted")
			s := &sequenceSource{p: p}
			a := &sequenceAdapter{}
			c := NewConductor(NewEngine(&memoryOwner{}, s, &fixtureAuthority{}, a), nil, nil)
			r, err := c.Run(context.Background(), p.Reference)
			if err != nil {
				t.Fatal(err)
			}
			want := []Operation{Commit}
			if target == "no-merge" || target == "verified-protected-base-merge" {
				want = append(want, Publish, PullRequest, CheckDelivery)
			}
			if target == "verified-protected-base-merge" {
				want = append(want, Merge)
				if r.Held != ReconcileIssue || r.Completion != "" {
					t.Fatal(r)
				}
			}
			if !reflect.DeepEqual(a.ops, want) || s.calls != 1 {
				t.Fatalf("%v %+v calls=%d", a.ops, r, s.calls)
			}
			_, err = c.Run(context.Background(), p.Reference)
			if err != nil || !reflect.DeepEqual(a.ops, want) {
				t.Fatalf("replayed effects %v %v", a.ops, err)
			}
		})
	}
}
func TestConductorHoldsRepairAndUnknown(t *testing.T) {
	for _, tc := range []struct {
		op      Operation
		outcome Outcome
		code    string
	}{
		{CheckDelivery, Unknown, "ci_gate"}, {CheckDelivery, Unknown, "review_gate"}, {CheckDelivery, Failed, "ci_failed"}, {CheckDelivery, Failed, "merge_conflict"}, {Publish, Unknown, "remote_unknown"}, {Merge, Unknown, "merged_validation_unresolved"},
	} {
		t.Run(string(tc.op)+tc.code, func(t *testing.T) {
			p := fixturePlan()
			s := &sequenceSource{p: p}
			a := &sequenceAdapter{held: tc.op, outcome: tc.outcome, code: tc.code}
			owner := &memoryOwner{}
			repair := &repairCapture{}
			c := NewConductor(NewEngine(owner, s, &fixtureAuthority{}, a), repair, nil)
			r, err := c.Run(context.Background(), p.Reference)
			if err != nil || r.Held != tc.op {
				t.Fatalf("%+v %v", r, err)
			}
			if tc.outcome == Failed {
				if r.Reason != "repair_requested" || len(repair.failures) != 1 || repair.failures[0].Run != p.Run || repair.failures[0].Evidence == "" {
					t.Fatal(r, repair)
				}
			}
			if tc.op == Merge && (r.Merge == "" || r.Reason != "merged_unresolved") {
				t.Fatal(r)
			}
			if tc.op == CheckDelivery && tc.outcome == Unknown {
				before := len(a.ops)
				_, _ = c.Run(context.Background(), p.Reference)
				if len(a.ops) != before {
					t.Fatal("unbudgeted recheck")
				}
			}
		})
	}
}
func TestConductorConcurrentOnePRSameMerge(t *testing.T) {
	p := fixturePlan()
	a := &sequenceAdapter{}
	c := NewConductor(NewEngine(&memoryOwner{}, &sequenceSource{p: p}, &fixtureAuthority{}, a), nil, nil)
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() { _, _ = c.Run(context.Background(), p.Reference) })
	}
	wg.Wait()
	a.Lock()
	defer a.Unlock()
	counts := map[Operation]int{}
	for _, op := range a.ops {
		counts[op]++
	}
	if counts[PullRequest] != 1 || counts[Merge] != 1 {
		t.Fatal(counts)
	}
}

// syntheticHTTP: actual closed GitHub HTTP operations, NOT realGitHub.
// Local commit is supplied by the fixture port; HTTP publication/PR/check/merge
// are the production adapter, not stage-only labels.
type syntheticHTTPPipeline struct{ remote, objects *GitHub }

func (a syntheticHTTPPipeline) Apply(ctx context.Context, i Intent, p Plan, g Guard) Observation {
	if i.Operation == Commit {
		if g(ctx) != nil {
			return Observation{Outcome: Denied}
		}
		return Observation{Outcome: Confirmed, Head: i.Head}
	}
	if i.Operation == Publish || i.Operation == TransferGraph || i.Operation == UploadBlob || i.Operation == UploadTree || i.Operation == UploadCommit {
		return a.objects.Apply(ctx, i, p, g)
	}
	return a.remote.Apply(ctx, i, p, g)
}
func (a syntheticHTTPPipeline) Reconcile(ctx context.Context, i Intent, p Plan, g Guard) Observation {
	if i.Operation == Commit {
		return Observation{Outcome: Confirmed, Head: i.Head}
	}
	if i.Operation == Publish || i.Operation == TransferGraph || i.Operation == UploadBlob || i.Operation == UploadTree || i.Operation == UploadCommit {
		return a.objects.Reconcile(ctx, i, p, g)
	}
	return a.remote.Reconcile(ctx, i, p, g)
}
func TestConductorSyntheticHTTPPipeline(t *testing.T) {
	for _, postFail := range []bool{false, true} {
		t.Run(map[bool]string{false: "merged", true: "merged-unresolved"}[postFail], func(t *testing.T) {
			g, f, p := newGithubFixture(t, objectPlan(t))
			objects := serveObjects(t, p, &objectServer{})
			f.postFail = postFail
			p.PR = 999
			c := NewConductor(NewEngine(&memoryOwner{}, fixtureSource{p}, &fixtureAuthority{}, syntheticHTTPPipeline{g, objects}), nil, nil)
			r, err := c.Run(context.Background(), p.Reference)
			if err != nil || r.PR != 7 || r.Merge == "" || !f.merged || f.writes != 2 {
				t.Fatalf("%+v %v writes=%d", r, err, f.writes)
			}
			if postFail && r.Reason != "merged_unresolved" {
				t.Fatal(r)
			}
			if !postFail && r.Held != ReconcileIssue {
				t.Fatal(r)
			}
			_, err = c.Run(context.Background(), p.Reference)
			if err != nil || f.writes != 2 {
				t.Fatalf("replay: %v writes=%d", err, f.writes)
			}
		})
	}
}
