package badger

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/delivery"
	badgerdb "github.com/dgraph-io/badger/v4"
)

// Every read must independently inspect the current transaction, including
// canonical records outside the slot catalogue and aliases of known records.
func TestPublicationHistoryCensusFreshCorruption(t *testing.T) {
	for _, fault := range []string{"head", "revision", "alias", "extra-revision", "orphan", "malformed-unrelated"} {
		t.Run(fault, func(t *testing.T) {
			ctx := context.Background()
			s := newDeliveryStore(t)
			c, b, i := seedDelivery(t, s, "census")
			r := deliveryRepo(t, s, b, &deliveryTestAuthority{})
			old, _, err := r.Reserve(ctx, i)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = r.Finish(ctx, old, delivery.Observation{Outcome: delivery.Confirmed}); err != nil {
				t.Fatal(err)
			}
			h, err := s.GetContributionDeliveryHistory(ctx, c.OwnerID, c.Key)
			if err != nil {
				t.Fatal(err)
			}
			chain := h.Histories[0]
			err = s.update(ctx, func(txn *badgerdb.Txn) error {
				entry := chain[len(chain)-1]
				k := key(familyContributionDelivery, i.ID, "head")
				switch fault {
				case "head":
					entry.Record.Code = "changed-after-read"
				case "revision":
					k = key(familyContributionDelivery, i.ID, revisionPart(1))
					entry = chain[0]
					entry.Record.Code = "changed-after-read"
				case "alias":
					k = key(familyContributionDelivery, i.ID, "alias")
				case "extra-revision":
					k = key(familyContributionDelivery, i.ID, revisionPart(3))
				case "orphan":
					if err := txn.Delete(publicationSlotKey(h.Root, i.ID)); err != nil {
						return err
					}
					if err := txn.Delete(key(familyContributionPublication, fmt.Sprint(i.RepositoryID), i.Base, "slot", i.ID)); err != nil {
						return err
					}
				case "malformed-unrelated":
					return txn.Set(key(familyContributionDelivery, "unrelated", "head"), []byte("{}"))
				}
				return txn.Set(k, deliveryWire(entry))
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.GetContributionDeliveryHistory(ctx, c.OwnerID, c.Key); err == nil {
				t.Fatal("corruption hidden by prior successful read")
			}
		})
	}
}

// A stable, non-native cost probe: preserve real per-read validation and log
// elapsed time rather than imposing host-dependent performance assertions.
func TestPublicationHistoryReadCost(t *testing.T) {
	ctx := context.Background()
	s := newDeliveryStore(t)
	c, b, i := seedDelivery(t, s, "cost")
	r := deliveryRepo(t, s, b, &deliveryTestAuthority{})
	for _, op := range []delivery.Operation{delivery.Publish, delivery.Commit, delivery.PullRequest, delivery.CheckDelivery} {
		i.Operation = op
		i.ID = deliveryTestSlot(i)
		old, _, err := r.Reserve(ctx, i)
		if err != nil {
			t.Fatal(err)
		}
		for n := 0; n < 16; n++ {
			old, err = r.Finish(ctx, old, delivery.Observation{Outcome: delivery.Unknown, Code: fmt.Sprintf("observation-%d", n)})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	start := time.Now()
	for n := 0; n < 10; n++ {
		h, err := s.GetContributionDeliveryHistory(ctx, c.OwnerID, c.Key)
		if err != nil || len(h.Histories) != 4 {
			t.Fatalf("read: chains=%d error=%v", len(h.Histories), err)
		}
	}
	t.Logf("history-read-cost reads=10 chains=4 revisions-per-chain=17 elapsed=%s", time.Since(start))
}
