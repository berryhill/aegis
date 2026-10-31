package badger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	badgerdb "github.com/dgraph-io/badger/v4"
)

func TestPublicationCustodyInitialRace(t *testing.T) {
	ctx := context.Background()
	s := newDeliveryStore(t)
	_, a, ia := seedDelivery(t, s, "a")
	_, b, ib := seedDelivery(t, s, "bb")
	var winners atomic.Int32
	var wg sync.WaitGroup
	for n := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			binding, intent := a, ia
			if n%2 != 0 {
				binding, intent = b, ib
			}
			r := deliveryRepo(t, s, binding, &deliveryTestAuthority{})
			_, fresh, err := r.Reserve(ctx, intent)
			if err != nil && !errors.Is(err, delivery.ErrConflict) {
				t.Error(err)
			}
			if fresh {
				winners.Add(1)
			}
		}()
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatalf("active acquisition winners=%d", winners.Load())
	}
}

func TestPublicationCustodyIndexCorruptionDenies(t *testing.T) {
	for _, fault := range []string{"active", "marker", "local-slot", "legacy-slot", "root"} {
		t.Run(fault, func(t *testing.T) {
			ctx := context.Background()
			s := newDeliveryStore(t)
			c, binding, intent := seedDelivery(t, s, "a")
			r := deliveryRepo(t, s, binding, &deliveryTestAuthority{})
			_, fresh, err := r.Reserve(ctx, intent)
			if err != nil || !fresh {
				t.Fatal(err)
			}
			second := intent
			second.Operation = delivery.Merge
			second.ID = deliveryTestSlot(second)
			if _, _, err = r.Reserve(ctx, second); err != nil {
				t.Fatal(err)
			}
			h, err := s.GetContributionDeliveryHistory(ctx, c.OwnerID, c.Key)
			if err != nil {
				t.Fatal(err)
			}
			k := key(familyContributionPublication, fmt.Sprint(intent.RepositoryID), intent.Base)
			switch fault {
			case "marker":
				k = key(familyContributionPublication, "indexed", c.OwnerID, c.Key)
			case "local-slot":
				k = publicationSlotKey(h.Root, intent.ID)
			case "legacy-slot":
				k = key(familyContributionPublication, fmt.Sprint(intent.RepositoryID), intent.Base, "slot", intent.ID)
			case "root":
				k = key(familyContributionPublication, "root", c.OwnerID, c.Key)
			}
			if err = s.update(ctx, func(txn *badgerdb.Txn) error { return txn.Delete(k) }); err != nil {
				t.Fatal(err)
			}
			if fault != "active" {
				if _, err = s.GetContributionDeliveryHistory(ctx, c.OwnerID, c.Key); !errors.Is(err, fleet.ErrCorrupt) {
					t.Fatalf("lost %s readback=%v", fault, err)
				}
			}
			_, other, next := seedDelivery(t, s, "bbb")
			if _, fresh, err = deliveryRepo(t, s, other, &deliveryTestAuthority{}).Reserve(ctx, next); err == nil || fresh {
				t.Fatalf("lost %s admitted takeover", fault)
			}
		})
	}
}

func TestPublicationCustodyLegacyHistoryIsConservative(t *testing.T) {
	ctx := context.Background()
	s := newDeliveryStore(t)
	c, b, i := seedDelivery(t, s, "legacy")
	r := deliveryRepo(t, s, b, &deliveryTestAuthority{})
	record, _, err := r.Reserve(ctx, i)
	if err != nil {
		t.Fatal(err)
	}
	h, err := s.GetContributionDeliveryHistory(ctx, c.OwnerID, c.Key)
	if err != nil {
		t.Fatal(err)
	}
	// Recreate the old on-disk shape in this disposable fixture only.
	if err = s.update(ctx, func(txn *badgerdb.Txn) error {
		if e := txn.Delete(publicationSlotKey(h.Root, i.ID)); e != nil {
			return e
		}
		return txn.Delete(key(familyContributionPublication, "indexed", c.OwnerID, c.Key))
	}); err != nil {
		t.Fatal(err)
	}
	legacy, err := s.GetContributionDeliveryHistory(ctx, c.OwnerID, c.Key)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(h)
	after, _ := json.Marshal(legacy)
	if string(before) != string(after) {
		t.Fatal("legacy history changed")
	}
	if replay, fresh, e := r.Reserve(ctx, i); e != nil || fresh || replay != record {
		t.Fatal("legacy same-intent replay", e)
	}
	_, other, next := seedDelivery(t, s, "different")
	if _, fresh, e := deliveryRepo(t, s, other, &deliveryTestAuthority{}).Reserve(ctx, next); e == nil || fresh {
		t.Fatal("legacy without canonical terminal/quiescence was reclaimed")
	}
}
