package badger

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/berryhill/aegis/internal/delivery"
)

// capacityReadbackAdapter uses the existing admitted synthetic remote-readback
// fixture without its optional lost-response injection. Actual object HTTP writes
// are separately exercised by delivery.TestPublicationCapacityBoundaryActualUpload.
type capacityReadbackAdapter struct{ *v7HistoryAdapter }

func (a capacityReadbackAdapter) Apply(ctx context.Context, i delivery.Intent, p delivery.Plan, g delivery.Guard) delivery.Observation {
	return a.Reconcile(ctx, i, p, g)
}

// This qualifies the real Engine/Badger history boundary with the existing
// synthetic admitted-source and remote-readback adapters, not live GitHub.
func TestPublicationCapacityDurableSubsequentOperations(t *testing.T) {
	ctx := context.Background()
	s := newDeliveryStore(t)
	c, b, i := seedDelivery(t, s, "")
	auth := &deliveryTestAuthority{}
	p := delivery.Plan{Reference: b.Reference, FenceEpoch: b.FenceEpoch, RoleBinding: b.RoleBinding, Intake: c, Actor: i.Actor, Repository: i.Repository, RepositoryID: i.RepositoryID, Base: i.Base, Branch: i.Branch, BaseHead: i.BaseHead, Policy: i.Policy, Delivery: c.Effect, PR: 7}
	object := func(kind string, raw []byte) delivery.GitObject {
		h := sha1.New()
		fmt.Fprintf(h, "%s %d\x00", kind, len(raw))
		h.Write(raw)
		return delivery.GitObject{Kind: kind, Raw: raw, OID: hex.EncodeToString(h.Sum(nil))}
	}
	var tree []byte
	for n := 0; n < 390; n++ {
		name := fmt.Sprintf("value%04d.go", n)
		raw := []byte(fmt.Sprintf("public value %04d", n))
		o := object("blob", raw)
		p.Objects = append(p.Objects, o)
		tree = append(tree, []byte("100644 "+name+"\x00")...)
		oid, _ := hex.DecodeString(o.OID)
		tree = append(tree, oid...)
		p.Commit.Files = append(p.Commit.Files, delivery.File{Path: name, Mode: "100644", Bytes: raw})
	}
	tr := object("tree", tree)
	p.Commit.Parent = c.Base
	p.Commit.Branch = p.Branch
	p.Commit.Tree = tr.OID
	p.Commit.Author = delivery.Identity{Name: "Fixture", Email: "fixture@example.invalid"}
	p.Commit.Committer = p.Commit.Author
	p.Commit.Timestamp = "2026-01-01T00:00:00Z"
	p.Commit.Message = "scoped\n"
	co := object("commit", []byte(fmt.Sprintf("tree %s\nparent %s\nauthor Fixture <fixture@example.invalid> 1767225600 +0000\ncommitter Fixture <fixture@example.invalid> 1767225600 +0000\n\nscoped\n", tr.OID, c.Base)))
	p.Commit.Commit = co.OID
	p.Head = co.OID
	p.Objects = append(p.Objects, tr, co)
	a := capacityReadbackAdapter{&v7HistoryAdapter{observation: delivery.Observation{Outcome: delivery.Confirmed}}}
	engine := delivery.NewEngine(deliveryRepo(t, s, b, auth), v6Source{p}, auth, a)
	// Every operation passes through real Engine admission, reservation, fencing
	// and Finish. Synthetic adapter readback is not live GitHub qualification.
	for _, op := range []delivery.Operation{delivery.Publish, delivery.Commit, delivery.PullRequest, delivery.UpdatePullRequest, delivery.CheckDelivery, delivery.Merge, delivery.ReconcileIssue} {
		var got delivery.Record
		var err error
		for tries := 0; tries < 400; tries++ {
			got, err = engine.Execute(ctx, p.Reference, op)
			if err != nil {
				t.Fatalf("%s: %v", op, err)
			}
			if got.Outcome == delivery.Confirmed {
				break
			}
		}
		if got.Outcome != delivery.Confirmed {
			t.Fatalf("%s not confirmed: %+v", op, got)
		}
	}
	before, err := s.GetContributionDeliveryHistory(ctx, c.OwnerID, c.Key)
	if err != nil {
		t.Fatal(err)
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
	after, err := s.GetContributionDeliveryHistory(ctx, c.OwnerID, c.Key)
	if err != nil || delivery.ValidateHistorySnapshot(after) != nil || len(after.Histories) != 400 {
		t.Fatalf("reopened chains=%d err=%v", len(after.Histories), err)
	}
	for n, chain := range after.Histories {
		if len(chain) != len(before.Histories[n]) || chain[len(chain)-1] != before.Histories[n][len(chain)-1] || chain[len(chain)-1].Record.Outcome != delivery.Confirmed {
			t.Fatalf("history %d changed", n)
		}
	}
}
