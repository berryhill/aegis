package app

import (
	"context"
	"encoding/json"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/delivery"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"strings"
	"testing"
	"time"
)

type qualificationHistoryAuthority struct{}

func (qualificationHistoryAuthority) Admit(context.Context, delivery.Intent, bool) error { return nil }
func TestReadContributionDeliveryHistoryFreshStaleForeign(t *testing.T) {
	ctx := context.Background()
	s, store, subject, loop, reader := contributionFixture(t)
	s.Now = time.Now
	subject = localImportPrincipal(s)
	reader.source.ObservedAt = s.Now()
	reader.source.UpdatedAt = s.Now()
	reader.source.CreatedAt = s.Now()
	configureLocalImportFleet(t, s, store)
	c, e := s.admitContributionAs(ctx, subject, "session", loop, reader.source.URL.URL, "qualification-history", "local-only", reader)
	if e != nil {
		t.Fatal(e)
	}
	d := func(v string) string { return contribution.Digest([]byte(v)) }
	b := fleetbadger.ContributionDeliveryBinding{QueueItemID: c.QueueItemID, OwnerID: c.OwnerID, Key: c.Key, Reference: delivery.Reference{Run: c.RunID, Contract: c.Digest, Candidate: d("synthetic-candidate")}, FenceEpoch: 1, RoleBinding: d("synthetic-role"), Base: "main"}
	repo, e := store.ContributionDelivery(ctx, b, qualificationHistoryAuthority{})
	if e != nil {
		t.Fatal(e)
	}
	i := delivery.Intent{Reference: b.Reference, FenceEpoch: 1, RoleBinding: b.RoleBinding, Operation: delivery.Commit, Actor: "actor", Repository: c.Repository, RepositoryID: 7, Base: "main", Branch: "contribution/run", Head: strings.Repeat("a", 40), BaseHead: c.Base, Policy: "synthetic", Payload: d("synthetic-payload")}
	raw, _ := json.Marshal(struct {
		Run, Contract, Repository, Base, Branch string
		RepositoryID                            uint64
		Operation                               delivery.Operation
	}{i.Run, i.Contract, i.Repository, i.Base, i.Branch, i.RepositoryID, i.Operation})
	i.ID = contribution.Digest(raw)
	if _, _, e = repo.Reserve(ctx, i); e != nil {
		t.Fatal(e)
	}
	h, e := s.ReadContributionDeliveryHistoryAs(ctx, subject, c.Key)
	if e != nil || h.Contract.Digest != c.Digest || h.RootRun != c.RunID || len(h.Histories) != 1 {
		t.Fatal("fresh same-owner canonical read", e)
	}
	stale := subject
	stale.ExpiresAt = s.Now().Add(-time.Second)
	if _, e = s.ReadContributionDeliveryHistoryAs(ctx, stale, c.Key); e == nil {
		t.Fatal("stale accepted")
	}
	foreign := subject
	foreign.PrincipalID = "foreign"
	if _, e = s.ReadContributionDeliveryHistoryAs(ctx, foreign, c.Key); e == nil {
		t.Fatal("foreign accepted")
	}
	t.Log("fresh owner real app/canonical root read PASS; stale and foreign denied; synthetic delivery reservation authority only, no runtime effect")
}
