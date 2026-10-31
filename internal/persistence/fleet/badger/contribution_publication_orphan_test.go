package badger

import (
	"context"
	"fmt"
	"github.com/berryhill/aegis/internal/delivery"
	badgerdb "github.com/dgraph-io/badger/v4"
	"testing"
)

func TestPublicationCustodyLostActiveAndLegacyMirrorIsNotFree(t *testing.T) {
	ctx := context.Background()
	s := newDeliveryStore(t)
	_, binding, intent := seedDelivery(t, s, "retained")
	r := deliveryRepo(t, s, binding, &deliveryTestAuthority{})
	if _, fresh, err := r.Reserve(ctx, intent); err != nil || !fresh {
		t.Fatal(err)
	}
	// The immutable root-local UNKNOWN/history survives these damaged indexes.
	if err := s.update(ctx, func(txn *badgerdb.Txn) error {
		if e := txn.Delete(key(familyContributionPublication, fmt.Sprint(intent.RepositoryID), intent.Base)); e != nil {
			return e
		}
		return txn.Delete(key(familyContributionPublication, fmt.Sprint(intent.RepositoryID), intent.Base, "slot", intent.ID))
	}); err != nil {
		t.Fatal(err)
	}
	_, next, nextIntent := seedDelivery(t, s, "successor")
	if _, fresh, err := deliveryRepo(t, s, next, &deliveryTestAuthority{}).Reserve(ctx, nextIntent); err == nil || fresh {
		t.Fatal("lost exclusion and legacy mirror hid an existing UNKNOWN root")
	}
}

func TestPublicationCustodyHistoryOnlyOrphanIsNotFree(t *testing.T) {
	ctx := context.Background()
	s := newDeliveryStore(t)
	_, binding, intent := seedDelivery(t, s, "retained")
	r := deliveryRepo(t, s, binding, &deliveryTestAuthority{})
	if _, fresh, err := r.Reserve(ctx, intent); err != nil || !fresh {
		t.Fatal(err)
	}
	if err := s.update(ctx, func(txn *badgerdb.Txn) error {
		for _, k := range [][]byte{
			key(familyContributionPublication, fmt.Sprint(intent.RepositoryID), intent.Base),
			key(familyContributionPublication, fmt.Sprint(intent.RepositoryID), intent.Base, "slot", intent.ID),
			key(familyContributionPublication, "root", binding.OwnerID, binding.Key),
			key(familyContributionPublication, "indexed", binding.OwnerID, binding.Key),
			key(familyContributionPublication, "slots", binding.OwnerID, binding.Key, intent.ID),
		} {
			if err := txn.Delete(k); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, next, nextIntent := seedDelivery(t, s, "successor")
	if _, fresh, err := deliveryRepo(t, s, next, &deliveryTestAuthority{}).Reserve(ctx, nextIntent); err == nil || fresh {
		t.Fatal("lost root indexes hid a surviving UNKNOWN effect history")
	}
}

func TestPublicationCustodyBothSlotIndexesCannotHideUnknown(t *testing.T) {
	ctx := context.Background()
	s := newDeliveryStore(t)
	c, b, i := seedDelivery(t, s, "partial")
	r := deliveryRepo(t, s, b, &deliveryTestAuthority{})
	if _, _, e := r.Reserve(ctx, i); e != nil {
		t.Fatal(e)
	}
	next := i
	next.Operation = delivery.Merge
	next.ID = deliveryTestSlot(next)
	old, _, e := r.Reserve(ctx, next)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = r.Finish(ctx, old, delivery.Observation{Outcome: delivery.Confirmed}); e != nil {
		t.Fatal(e)
	}
	if e = s.update(ctx, func(txn *badgerdb.Txn) error {
		if e := txn.Delete(key(familyContributionPublication, "slots", b.OwnerID, b.Key, i.ID)); e != nil {
			return e
		}
		return txn.Delete(key(familyContributionPublication, fmt.Sprint(i.RepositoryID), i.Base, "slot", i.ID))
	}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.GetContributionDeliveryHistory(ctx, c.OwnerID, c.Key); e == nil {
		t.Fatal("two deleted slot indexes hid UNKNOWN history behind a resolved slot")
	}
}
