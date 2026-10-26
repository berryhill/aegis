package badger

import (
	"context"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/delivery"
	badgerdb "github.com/dgraph-io/badger/v4"
	"strings"
	"testing"
	"time"
)

type v6Source struct{ p delivery.Plan }

func (s v6Source) WithAccepted(ctx context.Context, _ delivery.Reference, f func(context.Context, delivery.Plan) error) error {
	return f(ctx, s.p)
}

type v6PartialAdapter struct{ writes, reads int }

func (a *v6PartialAdapter) Apply(ctx context.Context, i delivery.Intent, p delivery.Plan, g delivery.Guard) delivery.Observation {
	if g(ctx) != nil {
		return delivery.Observation{Outcome: delivery.Denied}
	}
	a.writes++
	return delivery.Observation{Outcome: delivery.Unknown, Code: "response_lost"}
}
func (a *v6PartialAdapter) Reconcile(ctx context.Context, i delivery.Intent, p delivery.Plan, g delivery.Guard) delivery.Observation {
	if g(ctx) != nil {
		return delivery.Observation{Outcome: delivery.Denied}
	}
	a.reads++
	return delivery.Observation{Outcome: delivery.Unknown, Merge: strings.Repeat("e", 40), Code: "merged_validation_unresolved"}
}
func TestContributionDeliveryIndependentV6PartialMergeDurable(t *testing.T) {
	ctx := context.Background()
	s := newDeliveryStore(t)
	c, b, i := seedDelivery(t, s, "")
	auth := &deliveryTestAuthority{}
	r := deliveryRepo(t, s, b, auth)
	p := delivery.Plan{Reference: b.Reference, FenceEpoch: b.FenceEpoch, RoleBinding: b.RoleBinding, Intake: c, Actor: i.Actor, Repository: i.Repository, RepositoryID: i.RepositoryID, Base: i.Base, Branch: i.Branch, Head: i.Head, BaseHead: i.BaseHead, Policy: i.Policy, Delivery: c.Effect, PR: 7}
	a := &v6PartialAdapter{}
	e := delivery.NewEngine(r, v6Source{p}, auth, a)
	first, err := e.Execute(ctx, p.Reference, delivery.Merge)
	if err != nil || first.Revision == 0 || a.writes != 1 {
		t.Fatalf("positive effect not started: %+v %v writes=%d", first, err, a.writes)
	}
	next, err := e.Execute(ctx, p.Reference, delivery.Merge)
	if err != nil || a.reads != 1 || a.writes != 1 {
		t.Fatalf("boundary not reached: %+v %v", next, err)
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
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("after clean reopen: history=%+v", h)
	if next.Merge != strings.Repeat("e", 40) || h[len(h)-1].Record.Merge != strings.Repeat("e", 40) {
		t.Fatalf("partial merge lost durably: returned.Merge=%q persisted.Merge=%q history=%d writes=%d reads=%d", next.Merge, h[len(h)-1].Record.Merge, len(h), a.writes, a.reads)
	}
}
func TestContributionDeliveryIndependentV6FinishReopen(t *testing.T) {
	ctx := context.Background()
	s := newDeliveryStore(t)
	_, b, i := seedDelivery(t, s, "")
	r := deliveryRepo(t, s, b, &deliveryTestAuthority{})
	old, f, e := r.Reserve(ctx, i)
	if e != nil || !f {
		t.Fatal(e)
	}
	o := delivery.Observation{Outcome: delivery.Unknown, Merge: strings.Repeat("e", 40)}
	done, e := r.Finish(ctx, old, o)
	if e != nil {
		t.Fatal(e)
	}
	root := s.root
	s.Close()
	s, e = Open(ctx, root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	r = deliveryRepo(t, s, b, &deliveryTestAuthority{})
	got, f, e := r.Reserve(ctx, i)
	if e != nil || f || got != done {
		t.Fatalf("finish lost after clean reopen: %+v %v %v", got, f, e)
	}
	h, e := r.History(ctx, i)
	if e != nil || len(h) != 2 {
		t.Fatal(h, e)
	}
	audit, e := s.AuditEvents(ctx)
	if e != nil || len(audit) != 2 {
		t.Fatal(audit, e)
	}
}
func TestContributionDeliveryIndependentV6ExpiryReadFence(t *testing.T) {
	ctx := context.Background()
	s := newDeliveryStore(t)
	c, b, i := seedDelivery(t, s, "")
	// Allow real canonical fsync admission to finish under race instrumentation;
	// the original independent 150ms fixture remains immutable in evidence.
	c.Deadline = time.Now().UTC().Add(2 * time.Second)
	c, e := contribution.Seal(c)
	if e != nil {
		t.Fatal(e)
	}
	wire, e := contribution.Marshal(c)
	if e != nil {
		t.Fatal(e)
	}
	e = s.update(ctx, func(txn *badgerdb.Txn) error { return txn.Set(key(familyContribution, c.QueueItemID), wire) })
	if e != nil {
		t.Fatal(e)
	}
	b.Contract = c.Digest
	i.Contract = c.Digest
	i.ID = deliveryTestSlot(i)
	r := deliveryRepo(t, s, b, &deliveryTestAuthority{})
	old, fresh, e := r.Reserve(ctx, i)
	if e != nil || !fresh {
		t.Fatalf("positive control %v %v", fresh, e)
	}
	time.Sleep(time.Until(c.Deadline) + 10*time.Millisecond)
	if e = r.Fence(ctx, i, true); e == nil {
		t.Fatal("expired write fence allowed")
	}
	if e = r.Fence(ctx, i, false); e != nil {
		t.Fatalf("expired read fence denied %v", e)
	}
	if _, f, e := r.Reserve(ctx, i); e != nil || f {
		t.Fatalf("expired same intent reconcile %v %v", f, e)
	}
	if _, e = r.Finish(ctx, old, delivery.Observation{Outcome: delivery.Unknown, Merge: strings.Repeat("e", 40)}); e != nil {
		t.Fatal(e)
	}
}

func TestContributionDeliveryIndependentV6MissingRecords(t *testing.T) {
	for _, kind := range []string{"intake", "request", "issue", "queue", "run", "owner", "slot"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			s := newDeliveryStore(t)
			c, b, i := seedDelivery(t, s, "")
			r := deliveryRepo(t, s, b, &deliveryTestAuthority{})
			if _, fresh, e := r.Reserve(ctx, i); e != nil || !fresh {
				t.Fatalf("positive control: %v %v", fresh, e)
			}
			k := key(familyContribution, c.QueueItemID)
			switch kind {
			case "request":
				k = key(familyContributionRequest, c.OwnerID, c.Key)
			case "issue":
				k = issueKey(c)
			case "queue":
				k = key(familyQueueItem, c.QueueItemID)
			case "run":
				k = key(familyGraphRun, c.RunID)
			case "owner":
				k = key(familyContributionPublication, "7", "main")
			case "slot":
				k = key(familyContributionPublication, "7", "main", "slot", i.ID)
			}
			if e := s.update(ctx, func(txn *badgerdb.Txn) error { return txn.Delete(k) }); e != nil {
				t.Fatal(e)
			}
			if _, fresh, e := r.Reserve(ctx, i); e == nil || fresh {
				t.Fatalf("missing %s silently repaired: fresh=%v err=%v", kind, fresh, e)
			}
			if e := r.Fence(ctx, i, true); e == nil {
				t.Fatalf("missing %s write fence admitted", kind)
			}
		})
	}
}
