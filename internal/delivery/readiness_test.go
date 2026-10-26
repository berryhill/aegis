package delivery

import (
	"context"
	"sync"
	"testing"
	"time"
)

// Qualification ONLY: finite state retained outside the Conductor, shared across
// phases/candidates/reruns. This is NOT the actual #377 durable infrastructure owner.
type qualificationReadinessBudget struct {
	sync.Mutex
	used           map[string]uint8
	intents        []Intent
	err            error
	requirePrivate bool
}

func (b *qualificationReadinessBudget) ReserveReadiness(ctx context.Context, i Intent, max uint8) error {
	b.Lock()
	defer b.Unlock()
	if ctx.Err() != nil || b.requirePrivate && ctx.Value(sequenceKey{}) != true {
		return ErrDenied
	}
	if b.err != nil {
		return b.err
	}
	if b.used == nil {
		b.used = map[string]uint8{}
	}
	if b.used[i.Run] >= max {
		return ErrReadinessExhausted
	}
	b.used[i.Run]++
	b.intents = append(b.intents, i)
	return nil
}
func readinessPolicy(t *testing.T, d time.Duration) RecheckPolicy {
	t.Helper()
	p, e := NewRecheckPolicy(2, d)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func TestReadinessHTTPBothPhases(t *testing.T) {
	g, f, p := newGithubFixture(t, objectPlan(t))
	objects := serveObjects(t, p, &objectServer{})
	f.initialPending = 1
	f.postPending = 1
	p.Intake.Deadline = time.Now().Add(time.Minute)
	s := &sequenceSource{p: p}
	b := &qualificationReadinessBudget{requirePrivate: true}
	c := NewConductor(NewEngine(&memoryOwner{}, s, &fixtureAuthority{}, syntheticHTTPPipeline{g, objects}), nil, nil, WithReadiness(readinessPolicy(t, time.Millisecond), b))
	out, e := c.Run(context.Background(), p.Reference)
	if e != nil || out.Held != ReconcileIssue || out.Merge == "" || f.mergeWrites != 1 || s.calls != 1 || len(b.intents) != 2 {
		t.Fatalf("%+v %v budget=%+v calls=%d merges=%d", out, e, b.intents, s.calls, f.mergeWrites)
	}
	if b.intents[0].Reference != p.Reference || b.intents[0].Operation != CheckDelivery || b.intents[1].Operation != Merge {
		t.Fatal(b.intents)
	}
	before := f.reads
	_, e = c.Run(context.Background(), p.Reference)
	if e != nil || f.reads != before || f.mergeWrites != 1 {
		t.Fatal("terminal replay performed HTTP", e, f.reads, before)
	}
}
func TestReadinessNilAndPersistentRootExhaustion(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "nil", true: "finite"}[enabled], func(t *testing.T) {
			g, f, p := newGithubFixture(t, objectPlan(t))
			objects := serveObjects(t, p, &objectServer{})
			f.initialPending = 20
			p.Intake.Deadline = time.Now().Add(time.Minute)
			b := &qualificationReadinessBudget{requirePrivate: true}
			owner := &memoryOwner{}
			s := &sequenceSource{p: p}
			var opts []ReadinessOption
			if enabled {
				opts = []ReadinessOption{WithReadiness(readinessPolicy(t, 0), b)}
			}
			c := NewConductor(NewEngine(owner, s, &fixtureAuthority{}, syntheticHTTPPipeline{g, objects}), nil, nil, opts...)
			out, e := c.Run(context.Background(), p.Reference)
			if e != nil || out.Held != CheckDelivery || f.merged {
				t.Fatal(out, e)
			}
			before := f.reads
			// Construct a NEW conductor with the SAME persisted root budget, not a local reset.
			c = NewConductor(NewEngine(owner, s, &fixtureAuthority{}, syntheticHTTPPipeline{g, objects}), nil, nil, opts...)
			out, e = c.Run(context.Background(), p.Reference)
			if e != nil || f.reads != before || f.mergeWrites != 0 {
				t.Fatal(out, e, "unbudgeted HTTP")
			}
			if enabled && (b.used[p.Run] != 2 || out.Reason != "readiness_budget_exhausted") {
				t.Fatal(out, b.used)
			}
			if !enabled && out.Reason != "readiness_policy_unavailable" {
				t.Fatal(out)
			}
		})
	}
}
func TestReadinessCancelTimerAndClosedPolicy(t *testing.T) {
	for _, v := range []struct {
		n uint8
		d time.Duration
	}{{0, 0}, {3, 0}, {1, -1}, {1, 31 * time.Second}} {
		if _, e := NewRecheckPolicy(v.n, v.d); e == nil {
			t.Fatal(v)
		}
	}
	p := fixturePlan()
	p.Intake.Deadline = time.Now().Add(time.Minute)
	s := &sequenceSource{p: p}
	a := &sequenceAdapter{held: CheckDelivery, outcome: Unknown, code: "review_gate"}
	b := &qualificationReadinessBudget{requirePrivate: true}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	defer cancel()
	out, e := NewConductor(NewEngine(&memoryOwner{}, s, &fixtureAuthority{}, a), nil, nil, WithReadiness(readinessPolicy(t, time.Second), b)).Run(ctx, p.Reference)
	if e != nil || out.Reason != "readiness_cancelled" || len(a.ops) != 4 || len(b.intents) != 0 || s.calls != 1 {
		t.Fatal(out, e, a.ops, b.intents)
	}
}
func TestReadinessBudgetErrorsPreserveKnownMerge(t *testing.T) {
	for _, problem := range []error{ErrReadinessExhausted, ErrDenied} {
		p := fixturePlan()
		p.Intake.Deadline = time.Now().Add(time.Minute)
		a := &sequenceAdapter{held: Merge, outcome: Unknown, code: "merged_validation_unresolved"}
		b := &qualificationReadinessBudget{err: problem}
		c := NewConductor(NewEngine(&memoryOwner{}, &sequenceSource{p: p}, &fixtureAuthority{}, a), nil, nil, WithReadiness(readinessPolicy(t, 0), b))
		out, e := c.Run(context.Background(), p.Reference)
		if e != nil || out.Merge == "" || out.Held != Merge || len(out.Effects) == 0 || out.Completion != "" {
			t.Fatal(out, e)
		}
		want := "readiness_budget_unavailable"
		if problem == ErrReadinessExhausted {
			want = "readiness_budget_exhausted"
		}
		if out.Reason != want {
			t.Fatal(out)
		}
	}
}
