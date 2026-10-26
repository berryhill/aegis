package badger

import (
	"context"
	"github.com/berryhill/aegis/internal/delivery"
	"strings"
	"testing"
)

type v7HistoryAdapter struct {
	observation delivery.Observation
	writes      int
}

func (a *v7HistoryAdapter) Apply(ctx context.Context, i delivery.Intent, p delivery.Plan, g delivery.Guard) delivery.Observation {
	if g(ctx) != nil {
		return delivery.Observation{Outcome: delivery.Denied}
	}
	a.writes++
	return delivery.Observation{Outcome: delivery.Unknown, Code: "response_lost"}
}
func (a *v7HistoryAdapter) Reconcile(ctx context.Context, i delivery.Intent, p delivery.Plan, g delivery.Guard) delivery.Observation {
	if g(ctx) != nil {
		return delivery.Observation{Outcome: delivery.Denied}
	}
	return a.observation
}
func TestContributionDeliveryV7AdvancedBaseHistory(t *testing.T) {
	ctx := context.Background()
	s := newDeliveryStore(t)
	c, b, i := seedDelivery(t, s, "")
	auth := &deliveryTestAuthority{}
	r := deliveryRepo(t, s, b, auth)
	p := delivery.Plan{Reference: b.Reference, FenceEpoch: b.FenceEpoch, RoleBinding: b.RoleBinding, Intake: c, Actor: i.Actor, Repository: i.Repository, RepositoryID: i.RepositoryID, Base: i.Base, Branch: i.Branch, Head: i.Head, BaseHead: i.BaseHead, Policy: i.Policy, Delivery: c.Effect, PR: 7}
	p.Commit.Tree = strings.Repeat("d", 40)
	a := &v7HistoryAdapter{}
	e := delivery.NewEngine(r, v6Source{p}, auth, a)
	first, err := e.Execute(ctx, p.Reference, delivery.Merge)
	if err != nil {
		t.Fatal(err)
	}
	a.observation = delivery.Observation{Outcome: delivery.Unknown, Code: "merged_validation_unresolved", Merge: strings.Repeat("e", 40), Head: p.Head, Tree: p.Commit.Tree, PR: 7, BaseHead: strings.Repeat("e", 40)}
	partial, err := e.Execute(ctx, p.Reference, delivery.Merge)
	if err != nil {
		t.Fatal(err)
	}
	repeat, err := e.Execute(ctx, p.Reference, delivery.Merge)
	if err != nil || repeat.Outcome != delivery.Unknown || repeat != partial {
		t.Fatal(repeat, err)
	}
	// A newer containing base is independently supplied by the admitted adapter;
	// durable BaseHead explicitly retains the first historical observation.
	a.observation.BaseHead = strings.Repeat("f", 40)
	advanced, err := e.Execute(ctx, p.Reference, delivery.Merge)
	if err != nil || advanced != partial {
		t.Fatal(advanced, err)
	}
	a.observation.Outcome = delivery.Confirmed
	a.observation.Code = "merged_verified"
	// The bare Engine now correctly holds a known merge without budget.
	held, err := e.Execute(ctx, p.Reference, delivery.Merge)
	if err != nil || held != partial {
		t.Fatal(held, err)
	}
	// Qualification-only explicit single read admission; production Conductor
	// requires the persistent #377 ReadinessBudget before this read.
	guard := func(c context.Context) error {
		if err := r.Fence(c, first.Intent, false); err != nil {
			return err
		}
		return auth.Admit(c, first.Intent, false)
	}
	observed := a.Reconcile(ctx, first.Intent, p, guard)
	joined, err := delivery.RetainObservation(partial, observed)
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := r.Finish(ctx, partial, joined)
	if err != nil || confirmed.Outcome != delivery.Confirmed || confirmed.Intent != first.Intent || confirmed.Merge != partial.Merge || confirmed.Tree != partial.Tree || confirmed.Head != partial.Head || confirmed.PR != partial.PR || confirmed.BaseHead != partial.BaseHead || a.writes != 1 {
		t.Fatal(confirmed, err, a.writes)
	}
	root := s.root
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r = deliveryRepo(t, s, b, auth)
	h, err := r.History(ctx, first.Intent)
	if err != nil || len(h) != 4 || h[2].Record != partial || h[3].Record != confirmed || h[2].Digest != h[3].PreviousDigest {
		t.Fatal(h, err)
	}
	if h[1].Record.Merge != "" || h[2].Record.Outcome != delivery.Unknown {
		t.Fatal(h)
	}
}

func TestContributionDeliveryV7DirectFinishHistoricalBase(t *testing.T) {
	ctx := context.Background()
	s := newDeliveryStore(t)
	_, b, i := seedDelivery(t, s, "")
	r := deliveryRepo(t, s, b, &deliveryTestAuthority{})
	old, _, err := r.Reserve(ctx, i)
	if err != nil {
		t.Fatal(err)
	}
	o := delivery.Observation{Outcome: delivery.Unknown, Merge: strings.Repeat("e", 40), Head: i.Head, Tree: strings.Repeat("d", 40), PR: 7, BaseHead: strings.Repeat("e", 40)}
	partial, err := r.Finish(ctx, old, o)
	if err != nil {
		t.Fatal(err)
	}
	o.BaseHead = strings.Repeat("f", 40)
	o.Outcome = delivery.Confirmed
	done, err := r.Finish(ctx, partial, o)
	if err != nil || done.BaseHead != partial.BaseHead || done.Merge != partial.Merge {
		t.Fatal(done, err)
	}
	h, err := r.History(ctx, i)
	if err != nil || len(h) != 3 || h[1].Record != partial || h[2].Record != done {
		t.Fatal(h, err)
	}
}
