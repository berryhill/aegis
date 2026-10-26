package badger

import (
	"context"
	"github.com/berryhill/aegis/internal/contribution"
	badgerdb "github.com/dgraph-io/badger/v4"
	"testing"
)

// Actual Badger persistence with explicitly synthetic seeded intake/authority.
func TestPublicationAuthoritativeAbsenceNotMissingIndex(t *testing.T) {
	ctx := context.Background()
	s := newDeliveryStore(t)
	c, b, i := seedDelivery(t, s, "")
	if absent, e := s.ContributionDeliveryAuthoritativeAbsence(ctx, c.OwnerID, c.Key); e == nil || absent {
		t.Fatal("absence without sticky stop", absent, e)
	}
	repo := deliveryRepo(t, s, b, &deliveryTestAuthority{})
	if _, _, e := repo.Reserve(ctx, i); e != nil {
		t.Fatal(e)
	}
	if e := s.AppendContributionControl(ctx, contribution.Control{QueueItemID: c.QueueItemID, ContractDigest: c.Digest, OwnerID: c.OwnerID, SessionID: "synthetic", Sequence: 1, Stop: true, Delivery: c.Effect, At: c.AdmittedAt}); e != nil {
		t.Fatal(e)
	}
	if absent, e := s.ContributionDeliveryAuthoritativeAbsence(ctx, c.OwnerID, c.Key); e != nil || absent {
		t.Fatal(absent, e)
	}
	// A lost root index cannot erase the retained original UNKNOWN slot/history.
	if e := s.update(ctx, func(txn *badgerdb.Txn) error {
		return txn.Delete(key(familyContributionPublication, "root", c.OwnerID, c.Key))
	}); e != nil {
		t.Fatal(e)
	}
	if absent, e := s.ContributionDeliveryAuthoritativeAbsence(ctx, c.OwnerID, c.Key); e != nil || absent {
		t.Fatal(absent, e)
	}
	if _, e := s.GetContributionPublicationContract(ctx, "foreign", c.QueueItemID); e == nil {
		t.Fatal("foreign root")
	}
}

func TestPublicationAuthoritativeNoRootAbsence(t *testing.T) {
	ctx := context.Background()
	s := newDeliveryStore(t)
	c, _, _ := seedDelivery(t, s, "")
	if e := s.AppendContributionControl(ctx, contribution.Control{QueueItemID: c.QueueItemID, ContractDigest: c.Digest, OwnerID: c.OwnerID, SessionID: "synthetic", Sequence: 1, Stop: true, Delivery: c.Effect, At: c.AdmittedAt}); e != nil {
		t.Fatal(e)
	}
	if absent, e := s.ContributionDeliveryAuthoritativeAbsence(ctx, c.OwnerID, c.Key); e != nil || !absent {
		t.Fatal(absent, e)
	}
}
