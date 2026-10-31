package badger

import (
	"context"
	"github.com/berryhill/aegis/internal/delivery"
	badgerdb "github.com/dgraph-io/badger/v4"
	"testing"
)

func TestPublicationModernIndexMigrationRunsOnce(t *testing.T) {
	ctx := context.Background()
	s := newDeliveryStore(t)
	_, binding, intent := seedDelivery(t, s, "once")
	r := deliveryRepo(t, s, binding, &deliveryTestAuthority{})
	if _, fresh, err := r.Reserve(ctx, intent); err != nil || !fresh {
		t.Fatal(err)
	}
	version := func(k []byte) uint64 {
		t.Helper()
		var v uint64
		if err := s.view(ctx, func(txn *badgerdb.Txn) error {
			item, e := txn.Get(k)
			if e != nil {
				return e
			}
			v = item.Version()
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return v
	}
	slot := key(familyContributionPublication, "slots", binding.OwnerID, binding.Key, intent.ID)
	marker := key(familyContributionPublication, "indexed", binding.OwnerID, binding.Key)
	slotBefore, markerBefore := version(slot), version(marker)
	next := intent
	next.Operation = delivery.Merge
	next.ID = deliveryTestSlot(next)
	if _, fresh, err := r.Reserve(ctx, next); err != nil || !fresh {
		t.Fatal(err)
	}
	if version(slot) != slotBefore || version(marker) != markerBefore {
		t.Fatal("modern root re-ran legacy migration and rewrote immutable catalogue prefix")
	}
}
