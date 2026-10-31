package badger

import (
	"context"
	"fmt"
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
