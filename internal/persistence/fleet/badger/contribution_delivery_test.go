package badger

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	badgerdb "github.com/dgraph-io/badger/v4"
)

// Explicit synthetic authority, never installed as the #377 production port.
type deliveryTestAuthority struct{ deny atomic.Bool }

func (a *deliveryTestAuthority) Admit(context.Context, delivery.Intent, bool) error {
	if a.deny.Load() {
		return delivery.ErrDenied
	}
	return nil
}
func deliveryTestDigest(s string) string { return contribution.Digest([]byte(s)) }
func deliveryTestSlot(i delivery.Intent) string {
	b, _ := json.Marshal(struct {
		Run, Contract, Repository, Base, Branch string
		RepositoryID                            uint64
		Operation                               delivery.Operation
	}{i.Run, i.Contract, i.Repository, i.Base, i.Branch, i.RepositoryID, i.Operation})
	return contribution.Digest(b)
}

// Seed canonical, sealed admission records directly, explicitly standing in for
// authenticated intake only. All delivery operations use the real Store/Badger.
func seedDelivery(t *testing.T, s *Store, suffix string) (contribution.Contract, ContributionDeliveryBinding, delivery.Intent) {
	t.Helper()
	now := time.Now().UTC()
	ref := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "agent", Revision: 1, Digest: deliveryTestDigest("agent")}
	role := contribution.RoleBinding{Agent: ref, Charter: ref, Runtime: registry.RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent", Target: "profile/agent"}}
	url, _ := contribution.ParseIssueURL("https://github.com/owner/repo/issues/1")
	c, e := contribution.Seal(contribution.Contract{SchemaVersion: contribution.Schema, OwnerID: "owner", SessionID: "session", Key: "key" + suffix, PayloadDigest: deliveryTestDigest("payload"), QueueItemID: "q" + suffix, SubmissionID: "submission" + suffix, RunID: "run" + suffix, Loop: ref, Controller: ref, Builder: ref, Tester: ref, Source: contribution.Source{URL: url, RepositoryID: 7, IssueID: 9 + uint64(len(suffix)), Author: contribution.Author{ID: 1, Login: "owner"}, State: "open", CreatedAt: now, UpdatedAt: now, ObservedAt: now, ContentDigest: deliveryTestDigest("source"), Discussion: []contribution.Comment{}}, Outcome: "behavior", Acceptance: "real test", Invariants: []string{}, Exclusions: []string{}, Repository: "owner/repo", Base: strings.Repeat("b", 40), Worktree: "/owned/worktree", Effect: "verified-protected-base-merge", Roles: []contribution.RoleBinding{role, role, role}, TemplateDigest: deliveryTestDigest("template"), CandidateBudget: 2, AdmittedAt: now, Deadline: now.Add(time.Hour)})
	if e != nil {
		t.Fatal(e)
	}
	dr := func(id string) reference.DigestRef {
		return reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: id, Digest: deliveryTestDigest(id)}
	}
	item, e := queue.NewItem(queue.Item{ItemID: c.QueueItemID, Submission: dr(c.SubmissionID), Snapshot: dr("snapshot"), Authority: dr("authority"), GraphRunID: c.RunID, MaxAttempts: 1, EnqueuedAt: now, AvailableAt: now})
	if e != nil {
		t.Fatal(e)
	}
	run, e := execution.NewGraphRun(execution.GraphRun{GraphRunID: c.RunID, QueueItem: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: item.ItemID, Digest: item.Digest}, Snapshot: item.Snapshot, Authority: item.Authority, CreatedAt: now})
	if e != nil {
		t.Fatal(e)
	}
	cw, _ := contribution.Marshal(c)
	iw, _ := queue.MarshalItem(item)
	rw, _ := execution.MarshalGraphRun(run)
	e = s.update(context.Background(), func(txn *badgerdb.Txn) error {
		for _, v := range []struct{ k, v []byte }{{key(familyContribution, c.QueueItemID), cw}, {key(familyContributionRequest, c.OwnerID, c.Key), []byte(c.QueueItemID)}, {issueKey(c), []byte(c.QueueItemID)}, {key(familyQueueItem, item.ItemID), iw}, {key(familyGraphRun, run.GraphRunID), rw}} {
			if e := create(txn, v.k, v.v); e != nil {
				return e
			}
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	b := ContributionDeliveryBinding{QueueItemID: c.QueueItemID, OwnerID: c.OwnerID, Key: c.Key, Reference: delivery.Reference{Run: c.RunID, Contract: c.Digest, Candidate: deliveryTestDigest("candidate")}, FenceEpoch: 1, RoleBinding: deliveryTestDigest("role"), Base: "main"}
	i := delivery.Intent{Reference: b.Reference, FenceEpoch: b.FenceEpoch, RoleBinding: b.RoleBinding, Operation: delivery.Publish, Actor: "actor", Repository: c.Repository, RepositoryID: 7, Base: b.Base, Branch: "contribution/" + c.RunID, Head: strings.Repeat("a", 40), BaseHead: c.Base, Policy: "policy", Payload: deliveryTestDigest("semantic")}
	i.ID = deliveryTestSlot(i)
	return c, b, i
}
func newDeliveryStore(t *testing.T) *Store {
	t.Helper()
	s, e := Open(context.Background(), filepath.Join(t.TempDir(), "fleet-v1"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
func deliveryRepo(t *testing.T, s *Store, b ContributionDeliveryBinding, a delivery.Authority) *ContributionDeliveryRepository {
	t.Helper()
	r, e := s.ContributionDelivery(context.Background(), b, a)
	if e != nil {
		t.Fatal(e)
	}
	return r
}

func TestContributionDeliveryConcurrentReopenUnknown(t *testing.T) {
	ctx := context.Background()
	s := newDeliveryStore(t)
	_, b, i := seedDelivery(t, s, "")
	a := &deliveryTestAuthority{}
	var wg sync.WaitGroup
	var fresh atomic.Int32
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := s.ContributionDelivery(ctx, b, a)
			if e != nil {
				t.Error(e)
				return
			}
			_, f, e := r.Reserve(ctx, i)
			if e != nil {
				t.Error(e)
			}
			if f {
				fresh.Add(1)
			}
		}()
	}
	wg.Wait()
	if fresh.Load() != 1 {
		t.Fatalf("fresh=%d", fresh.Load())
	}
	root := s.root
	if e := s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e := Open(ctx, root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	r := deliveryRepo(t, s, b, a)
	old, f, e := r.Reserve(ctx, i)
	if e != nil || f || old.Outcome != delivery.Unknown {
		t.Fatalf("reopen fresh=%v record=%+v err=%v", f, old, e)
	}
	h, e := r.History(ctx, i)
	if e != nil || len(h) != 1 || h[0].Record != old {
		t.Fatalf("history %v %v", h, e)
	}
	changed := i
	changed.Payload = deliveryTestDigest("changed")
	if _, f, e = r.Reserve(ctx, changed); !errors.Is(e, delivery.ErrConflict) || f {
		t.Fatalf("payload conflict %v %v", f, e)
	}
	b.FenceEpoch++
	r2 := deliveryRepo(t, s, b, a)
	changed = i
	changed.FenceEpoch++
	if _, f, e = r2.Reserve(ctx, changed); e == nil || f {
		t.Fatalf("epoch fresh %v %v", f, e)
	}
}
func TestContributionDeliveryBindingDenials(t *testing.T) {
	ctx := context.Background()
	s := newDeliveryStore(t)
	_, b, i := seedDelivery(t, s, "")
	a := &deliveryTestAuthority{}
	if _, e := s.ContributionDelivery(ctx, b, nil); e == nil {
		t.Fatal("nil authority")
	}
	for _, mut := range []func(*ContributionDeliveryBinding){func(b *ContributionDeliveryBinding) { b.FenceEpoch = 0 }, func(b *ContributionDeliveryBinding) { b.OwnerID = "foreign" }, func(b *ContributionDeliveryBinding) { b.Key = "other" }, func(b *ContributionDeliveryBinding) { b.Run = "other" }, func(b *ContributionDeliveryBinding) { b.Contract = deliveryTestDigest("other") }} {
		x := b
		mut(&x)
		if _, e := s.ContributionDelivery(ctx, x, a); e == nil {
			t.Fatal("bad constructor")
		}
	}
	r := deliveryRepo(t, s, b, a)
	for _, mut := range []func(*delivery.Intent){func(i *delivery.Intent) { i.Repository = "other/repo" }, func(i *delivery.Intent) { i.RepositoryID++ }, func(i *delivery.Intent) { i.Run = "other" }, func(i *delivery.Intent) { i.Contract = deliveryTestDigest("other") }, func(i *delivery.Intent) { i.Candidate = deliveryTestDigest("other") }, func(i *delivery.Intent) { i.FenceEpoch++ }, func(i *delivery.Intent) { i.RoleBinding = deliveryTestDigest("other") }, func(i *delivery.Intent) { i.Base = "other" }, func(i *delivery.Intent) { i.BaseHead = strings.Repeat("c", 40) }, func(i *delivery.Intent) { i.ID = deliveryTestDigest("different slot") }} {
		x := i
		mut(&x)
		if _, f, e := r.Reserve(ctx, x); e == nil || f {
			t.Fatalf("substitution fresh=%v err=%v", f, e)
		}
	}
	a.deny.Store(true)
	if _, f, e := r.Reserve(ctx, i); e == nil || f {
		t.Fatal("unavailable authority allowed")
	}
}
func TestContributionDeliveryCASAndMergeFacts(t *testing.T) {
	ctx := context.Background()
	s := newDeliveryStore(t)
	_, b, i := seedDelivery(t, s, "")
	i.Operation = delivery.Merge
	i.ID = deliveryTestSlot(i)
	r := deliveryRepo(t, s, b, &deliveryTestAuthority{})
	old, f, e := r.Reserve(ctx, i)
	if e != nil || !f {
		t.Fatal(e)
	}
	positive := delivery.Observation{Outcome: delivery.Unknown, Merge: strings.Repeat("c", 40), Tree: strings.Repeat("d", 40), Code: "postmerge_unknown"}
	next, e := r.Finish(ctx, old, positive)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = r.Finish(ctx, old, delivery.Observation{Outcome: delivery.Confirmed}); !errors.Is(e, delivery.ErrConflict) {
		t.Fatalf("stale CAS %v", e)
	}
	for _, o := range []delivery.Observation{{Outcome: delivery.Unknown}, {Outcome: delivery.Denied}, {Outcome: delivery.Confirmed, Merge: strings.Repeat("e", 40), Tree: positive.Tree}} {
		if _, e = r.Finish(ctx, next, o); !errors.Is(e, delivery.ErrConflict) {
			t.Fatalf("erased merge %v", e)
		}
	}
	positive.Outcome = delivery.Confirmed
	done, e := r.Finish(ctx, next, positive)
	if e != nil {
		t.Fatal(e)
	}
	positive.Outcome = delivery.Denied
	if _, e = r.Finish(ctx, done, positive); !errors.Is(e, delivery.ErrConflict) {
		t.Fatalf("terminal rewrite %v", e)
	}
	h, e := r.History(ctx, i)
	if e != nil || len(h) != 3 || h[2].Record.Merge != done.Merge {
		t.Fatalf("history %v %v", h, e)
	}
	events, e := s.AuditEvents(ctx)
	if e != nil || len(events) != 3 {
		t.Fatalf("audit %d %v", len(events), e)
	}
}
func TestContributionDeliveryStopRetainsOwnerAndAllowsReconcile(t *testing.T) {
	ctx := context.Background()
	s := newDeliveryStore(t)
	c, b, i := seedDelivery(t, s, "")
	a := &deliveryTestAuthority{}
	r := deliveryRepo(t, s, b, a)
	old, _, e := r.Reserve(ctx, i)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.AppendContributionControl(ctx, contribution.Control{QueueItemID: c.QueueItemID, ContractDigest: c.Digest, OwnerID: c.OwnerID, SessionID: "stop-session", Sequence: 1, Stop: true, Delivery: c.Effect, At: time.Now().UTC()}); e != nil {
		t.Fatal(e)
	}
	if e = r.Fence(ctx, i, true); e == nil {
		t.Fatal("stopped write")
	}
	if e = r.Fence(ctx, i, false); e != nil {
		t.Fatal(e)
	}
	if _, f, e := r.Reserve(ctx, i); e != nil || f {
		t.Fatalf("stopped replay %v %v", f, e)
	}
	if _, e = r.Finish(ctx, old, delivery.Observation{Outcome: delivery.Confirmed, Head: i.Head}); e != nil {
		t.Fatal(e)
	}
	_, other, j := seedDelivery(t, s, "second")
	r2 := deliveryRepo(t, s, other, a)
	if _, f, e := r2.Reserve(ctx, j); !errors.Is(e, delivery.ErrConflict) || f {
		t.Fatalf("owner released %v %v", f, e)
	}
}
func TestContributionDeliveryCorruptHistoryHead(t *testing.T) {
	for _, mode := range []string{"history", "head", "missing-head", "missing-history", "duplicate", "unknown", "owner"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			s := newDeliveryStore(t)
			_, b, i := seedDelivery(t, s, "")
			r := deliveryRepo(t, s, b, &deliveryTestAuthority{})
			if _, _, e := r.Reserve(ctx, i); e != nil {
				t.Fatal(e)
			}
			e := s.update(ctx, func(txn *badgerdb.Txn) error {
				k := key(familyContributionDelivery, i.ID, "head")
				switch mode {
				case "missing-history":
					if e := txn.Delete(key(familyContributionDelivery, i.ID, revisionPart(1))); e != nil {
						return e
					}
					return txn.Delete(k)
				case "missing-head":
					return txn.Delete(k)
				case "history":
					k = key(familyContributionDelivery, i.ID, revisionPart(1))
				case "owner":
					k = key(familyContributionPublication, "7", "main")
				}
				wire, e := get(txn, k)
				if e != nil {
					return e
				}
				switch mode {
				case "duplicate":
					wire = append(wire[:len(wire)-1], []byte(",\"Digest\":\"bad\"}")...)
				case "unknown":
					wire = append(wire[:len(wire)-1], []byte(",\"Unexpected\":true}")...)
				default:
					wire = append([]byte(" "), wire...)
				}
				return txn.Set(k, wire)
			})
			if e != nil {
				t.Fatal(e)
			}
			if _, _, e = r.Reserve(ctx, i); !errors.Is(e, fleet.ErrCorrupt) {
				t.Fatalf("corruption accepted %v", e)
			}
		})
	}
}

// A lost post-transaction admission response cannot roll back intent custody.
type deliveryRevokingAuthority struct{ calls atomic.Int32 }

func (a *deliveryRevokingAuthority) Admit(_ context.Context, _ delivery.Intent, writing bool) error {
	if a.calls.Add(1) >= 3 && writing {
		return delivery.ErrDenied
	}
	return nil
}
func TestContributionDeliveryPostAdmissionDenialRetainsIntent(t *testing.T) {
	ctx := context.Background()
	s := newDeliveryStore(t)
	_, b, i := seedDelivery(t, s, "")
	r := deliveryRepo(t, s, b, &deliveryRevokingAuthority{})
	if _, fresh, e := r.Reserve(ctx, i); e == nil || fresh {
		t.Fatalf("post admission: %v %v", fresh, e)
	}
	r = deliveryRepo(t, s, b, &deliveryTestAuthority{})
	if old, fresh, e := r.Reserve(ctx, i); e != nil || fresh || old.Revision != 1 {
		t.Fatalf("lost intent: %+v %v %v", old, fresh, e)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, fresh, e := r.Reserve(cancelled, i); e == nil || fresh {
		t.Fatal("cancelled context admitted")
	}
}
func TestContributionDeliveryNarrowingAndBranchRotation(t *testing.T) {
	ctx := context.Background()
	s := newDeliveryStore(t)
	c, b, i := seedDelivery(t, s, "")
	r := deliveryRepo(t, s, b, &deliveryTestAuthority{})
	if _, _, e := r.Reserve(ctx, i); e != nil {
		t.Fatal(e)
	}
	changed := i
	changed.Branch = "different/branch"
	changed.ID = deliveryTestSlot(changed)
	if _, fresh, e := r.Reserve(ctx, changed); !errors.Is(e, delivery.ErrConflict) || fresh {
		t.Fatalf("branch rotation: %v %v", fresh, e)
	}
	if e := s.AppendContributionControl(ctx, contribution.Control{QueueItemID: c.QueueItemID, ContractDigest: c.Digest, OwnerID: c.OwnerID, SessionID: "narrow", Sequence: 1, Delivery: "local-only", At: time.Now().UTC()}); e != nil {
		t.Fatal(e)
	}
	changed = i
	changed.Operation = delivery.Merge
	changed.ID = deliveryTestSlot(changed)
	if _, fresh, e := r.Reserve(ctx, changed); !errors.Is(e, delivery.ErrDenied) || fresh {
		t.Fatalf("narrowing: %v %v", fresh, e)
	}
	if e := r.Fence(ctx, i, false); e != nil {
		t.Fatal(e)
	}
}
