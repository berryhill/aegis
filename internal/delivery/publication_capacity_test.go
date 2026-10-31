package delivery

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// Build a real reachable canonical graph, not duplicate padding that would fail
// graph admission for an unrelated reason. 512 is the existing history bound.
func capacityPlan(t *testing.T, count int) Plan {
	t.Helper()
	p := objectPlan(t)
	template := p.Objects[len(p.Objects)-1]
	var tree []byte
	p.Objects = nil
	for n := 0; n < count-2; n++ {
		o := GitObject{Kind: "blob", Raw: []byte(fmt.Sprintf("public value %04d", n))}
		o.OID = objectOID(o.Kind, o.Raw)
		p.Objects = append(p.Objects, o)
		tree = append(tree, []byte(fmt.Sprintf("100644 value%04d.go\x00", n))...)
		oid, _ := hex.DecodeString(o.OID)
		tree = append(tree, oid...)
	}
	tr := GitObject{Kind: "tree", Raw: tree}
	tr.OID = objectOID(tr.Kind, tr.Raw)
	oldTree := p.Commit.Tree
	p.Commit.Tree = tr.OID
	template.Raw = []byte(strings.Replace(string(template.Raw), oldTree, tr.OID, 1))
	template.OID = objectOID(template.Kind, template.Raw)
	p.Head = template.OID
	p.Commit.Commit = p.Head
	p.Objects = append(p.Objects, tr, template)
	return p
}

func TestPublicationCapacityAdmissionBeforeAnyEffects(t *testing.T) {
	// Eight distinct non-object intent slots must still fit after object uploads.
	for _, op := range []Operation{Commit, TransferGraph, UploadBlob, UploadTree, UploadCommit, Publish, PullRequest, UpdatePullRequest, CheckDelivery, Merge, ReconcileIssue} {
		t.Run(string(op), func(t *testing.T) {
			p := capacityPlan(t, 393)
			for _, o := range p.Objects {
				if objectOp(o) == op {
					p.Object = o
					break
				}
			}
			owner := &memoryOwner{}
			adapter := &fixtureAdapter{}
			_, err := NewEngine(owner, fixtureSource{p}, &fixtureAuthority{}, adapter).Execute(context.Background(), p.Reference, op)
			if err == nil || len(owner.records) != 0 || adapter.writes != 0 {
				t.Fatalf("oversized graph reached reservation/effect: err=%v slots=%d writes=%d", err, len(owner.records), adapter.writes)
			}
		})
	}
}
func TestPublicationCapacityBoundary(t *testing.T) {
	p := capacityPlan(t, 392)
	if err := validateGraph(p); err != nil {
		t.Fatal(err)
	}
	slots := map[string]bool{}
	for _, op := range []Operation{Commit, TransferGraph, Publish, PullRequest, UpdatePullRequest, CheckDelivery, Merge, ReconcileIssue} {
		i, err := intentFor(p, op)
		if err != nil {
			t.Fatal(op, err)
		}
		slots[i.ID] = true
	}
	for _, o := range p.Objects {
		p.Object = o
		i, err := intentFor(p, objectOp(o))
		if err != nil {
			t.Fatal(err)
		}
		slots[i.ID] = true
	}
	if len(slots) != 400 {
		t.Fatalf("envelope slots=%d", len(slots))
	}
}
