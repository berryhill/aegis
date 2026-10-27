package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/delivery"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"github.com/berryhill/aegis/internal/reference"
)

// Actual service/Badger history; reservation authority is synthetic. This is
// explicitly not #377, live GitHub or native accepted-candidate qualification.
func TestRetainedDeliveryCanonicalHistory(t *testing.T) {
	ctx := context.Background()
	s, store, subject, loop, reader := contributionFixture(t)
	s.Now = time.Now
	subject = localImportPrincipal(s)
	reader.source.ObservedAt = s.Now()
	reader.source.UpdatedAt = s.Now()
	reader.source.CreatedAt = s.Now()
	configureLocalImportFleet(t, s, store)
	c, err := s.admitContributionAs(ctx, subject, "session", loop, reader.source.URL.URL, "assessment", "local-only", reader)
	if err != nil {
		t.Fatal(err)
	}
	d := func(s string) string { return contribution.Digest([]byte(s)) }
	p := ContributionDeliveryPolicy{Loop: c.Loop, Workspace: c.Worktree, BaseHead: c.Base, Actor: "actor", Branch: "contribution/assessment", Tree: strings.Repeat("b", 40)}
	p.Binding = fleetbadger.ContributionDeliveryBinding{QueueItemID: c.QueueItemID, OwnerID: c.OwnerID, Key: c.Key, Reference: delivery.Reference{Run: c.RunID, Contract: c.Digest, Candidate: d("candidate")}, FenceEpoch: 1, RoleBinding: d("role"), Base: "main"}
	owner, err := store.ContributionDelivery(ctx, p.Binding, qualificationHistoryAuthority{})
	if err != nil {
		t.Fatal(err)
	}
	i := delivery.Intent{Reference: p.Binding.Reference, FenceEpoch: 1, RoleBinding: p.Binding.RoleBinding, Operation: delivery.Commit, Actor: p.Actor, Repository: c.Repository, RepositoryID: c.Source.RepositoryID, Base: "main", Branch: p.Branch, Head: strings.Repeat("a", 40), BaseHead: c.Base, Policy: p.Digest(), Payload: d("payload")}
	raw, _ := json.Marshal(struct {
		Run, Contract, Repository, Base, Branch string
		RepositoryID                            uint64
		Operation                               delivery.Operation
	}{i.Run, i.Contract, i.Repository, i.Base, i.Branch, i.RepositoryID, i.Operation})
	i.ID = contribution.Digest(raw)
	reserved, _, err := owner.Reserve(ctx, i)
	if err != nil {
		t.Fatal(err)
	}
	h, err := s.ReadContributionDeliveryHistoryAs(ctx, subject, c.Key)
	if err != nil {
		t.Fatal(err)
	}
	if code := assessRetainedDeliveryHistory(h, p, "local-only"); code == "retained_delivery_verified" {
		t.Fatal("unknown effect accepted")
	}
	_, err = owner.Finish(ctx, reserved, delivery.Observation{Outcome: delivery.Confirmed, Head: i.Head, Tree: p.Tree})
	if err != nil {
		t.Fatal(err)
	}
	h, err = s.ReadContributionDeliveryHistoryAs(ctx, subject, c.Key)
	if err != nil {
		t.Fatal(err)
	}
	if code := assessRetainedDeliveryHistory(h, p, "local-only"); code != "retained_delivery_verified" {
		t.Fatal(code)
	}
	for _, kind := range []string{"policy", "candidate", "role", "tree", "epoch", "head", "chain", "scope"} {
		copy := p
		history := h
		target := "local-only"
		switch kind {
		case "policy":
			copy.Actor = "other"
		case "candidate":
			copy.Binding.Candidate = d("foreign")
		case "role":
			copy.Binding.RoleBinding = d("foreign")
		case "tree":
			copy.Tree = strings.Repeat("c", 40)
		case "epoch":
			copy.Binding.FenceEpoch++
		case "head":
			history.Histories = append([][]delivery.HistoryEnvelope(nil), h.Histories...)
			history.Histories[0] = append([]delivery.HistoryEnvelope(nil), h.Histories[0]...)
			history.Histories[0][1].Record.Head = strings.Repeat("d", 40)
		case "chain":
			history.ContractBytes = []byte("corrupt")
		case "scope":
			target = "verified-protected-base-merge"
		}
		if assessRetainedDeliveryHistory(history, copy, target) == "retained_delivery_verified" {
			t.Fatal("substitution accepted", kind)
		}
	}
	ref := reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: "policy", Digest: p.Digest()}
	resolver := &reviewedDeliveryFixture{policy: p}
	result, err := s.AssessContributionRetainedDeliveryAs(ctx, subject, c.Key, ref, resolver)
	if err == nil || result.Status == "retained-delivery-verified" {
		t.Fatal("intake without actual native evidence accepted")
	}
	stale := subject
	stale.ExpiresAt = s.Now().Add(-time.Second)
	if _, err := s.AssessContributionRetainedDeliveryAs(ctx, stale, c.Key, ref, resolver); err == nil {
		t.Fatal("stale accepted")
	}
	foreign := subject
	foreign.PrincipalID = "foreign"
	if _, err := s.AssessContributionRetainedDeliveryAs(ctx, foreign, c.Key, ref, resolver); err == nil {
		t.Fatal("foreign accepted")
	}
	resolver.policy.Actor = "drift"
	if _, err := s.AssessContributionRetainedDeliveryAs(ctx, subject, c.Key, ref, resolver); err == nil {
		t.Fatal("policy drift accepted")
	}
}
