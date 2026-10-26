package delivery

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// These are controlled in-memory port qualification fixtures, NOT durable owner integration.
type memoryOwner struct {
	sync.Mutex
	records   map[string]Record
	owner     string
	cancelled bool
}

func (m *memoryOwner) Reserve(_ context.Context, i Intent) (Record, bool, error) {
	m.Lock()
	defer m.Unlock()
	if m.records == nil {
		m.records = map[string]Record{}
	}
	if r, ok := m.records[i.ID]; ok {
		return r, false, nil
	}
	if m.owner != "" && m.owner != i.Run {
		return Record{}, false, ErrDenied
	}
	m.owner = i.Run
	r := Record{Intent: i, Outcome: Unknown, Revision: 1}
	m.records[i.ID] = r
	return r, true, nil
}
func (m *memoryOwner) Finish(_ context.Context, old Record, o Observation) (Record, error) {
	m.Lock()
	defer m.Unlock()
	r := m.records[old.Intent.ID]
	if r.Revision != old.Revision {
		return r, ErrConflict
	}
	r.Observation = o
	r.Outcome = o.Outcome
	r.Revision++
	m.records[r.Intent.ID] = r
	return r, nil
}
func (m *memoryOwner) Fence(_ context.Context, i Intent, writing bool) error {
	m.Lock()
	defer m.Unlock()
	if m.owner != i.Run || (writing && m.cancelled) {
		return ErrDenied
	}
	return nil
}

type fixtureAdapter struct {
	sync.Mutex
	writes    int
	lost      bool
	confirmed bool
}

func (a *fixtureAdapter) Apply(ctx context.Context, i Intent, p Plan, g Guard) Observation {
	if g(ctx) != nil {
		return Observation{Outcome: Denied, Code: "admission_denied"}
	}
	a.Lock()
	defer a.Unlock()
	a.writes++
	a.confirmed = true
	if a.lost {
		return Observation{Outcome: Unknown, Code: "response_unknown"}
	}
	return Observation{Outcome: Confirmed, Head: i.Head}
}
func (a *fixtureAdapter) Reconcile(_ context.Context, i Intent, _ Plan, _ Guard) Observation {
	a.Lock()
	defer a.Unlock()
	if a.confirmed {
		return Observation{Outcome: Confirmed, Head: i.Head}
	}
	return Observation{Outcome: Unknown}
}

type fixtureSource struct{ plan Plan }

func (s fixtureSource) WithAccepted(ctx context.Context, _ Reference, fn func(context.Context, Plan) error) error {
	return fn(ctx, s.plan)
}

type fixtureAuthority struct{ denied bool }

func (a *fixtureAuthority) Admit(context.Context, Intent, bool) error {
	if a.denied {
		return errors.New("private canary must not escape")
	}
	return nil
}
func fixturePlan() Plan {
	return Plan{FenceEpoch: 1, RoleBinding: digest("synthetic-admitted-role-binding-not-real-authority"), Reference: Reference{Run: "run", Contract: "contract", Candidate: "candidate"}, Actor: "actor", RepositoryID: 1, Repository: "owner/repo", Base: "main", Branch: "contribution/run", Head: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", BaseHead: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Delivery: "verified-protected-base-merge", Policy: "policy"}
}
func TestUnknownReconcilesWithoutResend(t *testing.T) {
	p := fixturePlan()
	m := &memoryOwner{}
	a := &fixtureAdapter{lost: true}
	e := NewEngine(m, fixtureSource{p}, &fixtureAuthority{}, a)
	r, err := e.Execute(context.Background(), p.Reference, Publish)
	if err != nil || r.Outcome != Unknown {
		t.Fatalf("first: %+v %v", r, err)
	}
	r, err = e.Execute(context.Background(), p.Reference, Publish)
	if err != nil || r.Outcome != Confirmed || a.writes != 1 {
		t.Fatalf("reconcile: %+v %v writes=%d", r, err, a.writes)
	}
}
func TestConcurrentReplayOneEffect(t *testing.T) {
	p := fixturePlan()
	m := &memoryOwner{}
	a := &fixtureAdapter{}
	e := NewEngine(m, fixtureSource{p}, &fixtureAuthority{}, a)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { _, _ = e.Execute(context.Background(), p.Reference, Publish) })
	}
	wg.Wait()
	if a.writes != 1 {
		t.Fatalf("writes=%d", a.writes)
	}
}
func TestCancelledStillReconcilesConfirmedMerge(t *testing.T) {
	p := fixturePlan()
	m := &memoryOwner{}
	a := &fixtureAdapter{lost: true}
	e := NewEngine(m, fixtureSource{p}, &fixtureAuthority{}, a)
	_, _ = e.Execute(context.Background(), p.Reference, Merge)
	m.cancelled = true
	r, err := e.Execute(context.Background(), p.Reference, Merge)
	if err != nil || r.Outcome != Confirmed {
		t.Fatalf("merge history lost %+v %v", r, err)
	}
}
func TestFreshAdmissionAndNarrowing(t *testing.T) {
	for _, target := range []string{"local-only", "no-publish", "no-merge"} {
		p := fixturePlan()
		p.Delivery = target
		a := &fixtureAdapter{}
		e := NewEngine(&memoryOwner{}, fixtureSource{p}, &fixtureAuthority{}, a)
		r, _ := e.Execute(context.Background(), p.Reference, Merge)
		if r.Outcome != Denied || a.writes != 0 {
			t.Fatalf("target %s: %+v", target, r)
		}
	}
	p := fixturePlan()
	a := &fixtureAdapter{}
	e := NewEngine(&memoryOwner{}, fixtureSource{p}, &fixtureAuthority{denied: true}, a)
	r, _ := e.Execute(context.Background(), p.Reference, Publish)
	if r.Outcome != Denied || r.Code != "admission_denied" || a.writes != 0 {
		t.Fatalf("denial %+v", r)
	}
}
func TestIntentBindsSemanticInputs(t *testing.T) {
	p := fixturePlan()
	a, _ := intentFor(p, Publish)
	p.Head = "cccccccccccccccccccccccccccccccccccccccc"
	b, _ := intentFor(p, Publish)
	if a.ID != b.ID || a.Payload == b.Payload {
		t.Fatal("head must conflict in stable slot")
	}
	p = fixturePlan()
	p.PRBody = "different"
	b, _ = intentFor(p, Publish)
	if a.ID != b.ID {
		t.Fatal("unrelated PR text rotated publication")
	}
}
