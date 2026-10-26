package badger

import (
	"context"
	"fmt"
	badgerdb "github.com/dgraph-io/badger/v4"
	"testing"
)

func TestContributionDeliveryV15RootMissingAndCorrupt(t *testing.T) {
	for _, kind := range []string{"root", "slot", "queue", "run"} {
		for _, corrupt := range []bool{false, true} {
			t.Run(fmt.Sprint(kind, corrupt), func(t *testing.T) {
				ctx := context.Background()
				s := newDeliveryStore(t)
				c, b, i := seedDelivery(t, s, "")
				r := deliveryRepo(t, s, b, &deliveryTestAuthority{})
				if _, _, e := r.Reserve(ctx, i); e != nil {
					t.Fatal(e)
				}
				var k []byte
				switch kind {
				case "root":
					k = key(familyContributionPublication, "root", c.OwnerID, c.Key)
				case "slot":
					k = key(familyContributionPublication, fmt.Sprint(i.RepositoryID), i.Base, "slot", i.ID)
				case "queue":
					k = key(familyQueueItem, c.QueueItemID)
				case "run":
					k = key(familyGraphRun, c.RunID)
				}
				if e := s.update(ctx, func(txn *badgerdb.Txn) error {
					if corrupt {
						return txn.Set(k, []byte(`{"bad":true}`))
					}
					return txn.Delete(k)
				}); e != nil {
					t.Fatal(e)
				}
				if _, e := s.GetContributionDeliveryHistory(ctx, c.OwnerID, c.Key); e == nil {
					t.Fatal("corruption accepted")
				}
			})
		}
	}
}
