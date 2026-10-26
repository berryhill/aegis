package badger

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	badgerdb "github.com/dgraph-io/badger/v4"
)

func TestHistoricalAuthorityProjectionDoesNotReadmitRevokedScope(t *testing.T) {
	store, authority := openCommandStore(t)
	ctx := context.Background()
	activeAt := authority.IssuedAt.Add(10 * time.Second)
	activate := badgerAuthorityCommand("historical-activate", core.AuthorityCommandActivate, authority, 1, "", activeAt.Add(-time.Second))
	receipt, err := store.ProcessAuthorityCommand(ctx, activate, activeAt, "controller")
	if err != nil || !receipt.Accepted {
		t.Fatal("fixture activation failed", err)
	}
	active, err := store.CurrentAuthorityProjection(ctx, authority.ID)
	if err != nil {
		t.Fatal(err)
	}
	completedAt := activeAt.Add(time.Second)
	revokedAt := activeAt.Add(time.Minute)
	revoke := badgerAuthorityCommand("historical-revoke", core.AuthorityCommandRevoke, authority, 2, active.SourceFactDigest, revokedAt.Add(-time.Second))
	if r, err := store.ProcessAuthorityCommand(ctx, revoke, revokedAt, "controller"); err != nil || !r.Accepted {
		t.Fatal("fixture revocation failed", err)
	}
	before, err := store.CurrentAuthorityProjection(ctx, authority.ID)
	if err != nil {
		t.Fatal(err)
	}
	outboxBefore, err := store.AuthorityOutbox(ctx, authority.ID)
	if err != nil {
		t.Fatal(err)
	}
	past, err := store.HistoricalAuthorityProjection(ctx, authority.ID, authority.Digest, completedAt)
	if err != nil || !reflect.DeepEqual(past, active) {
		t.Fatal("exact historical projection unavailable", err)
	}
	current, err := store.AuthorityAdmission(ctx, authority.ID, authority.Digest, revokedAt)
	if err != nil || current.Admitted || current.Projection.State != core.AuthorityStateRevoked {
		t.Fatal("historical read re-admitted execution", err)
	}
	after, err := store.CurrentAuthorityProjection(ctx, authority.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("historical read changed current projection", err)
	}
	outboxAfter, err := store.AuthorityOutbox(ctx, authority.ID)
	if err != nil || !reflect.DeepEqual(outboxBefore, outboxAfter) {
		t.Fatal("historical read created authority effects", err)
	}
	atRevocation, err := store.HistoricalAuthorityProjection(ctx, authority.ID, authority.Digest, revokedAt)
	if err != nil || atRevocation.State != core.AuthorityStateRevoked {
		t.Fatal("revocation instant misclassified", err)
	}
	for _, at := range []time.Time{{}, authority.IssuedAt.Add(-time.Second), activeAt.Add(-time.Second), authority.ExpiresAt} {
		if _, err := store.HistoricalAuthorityProjection(ctx, authority.ID, authority.Digest, at); err == nil {
			t.Fatal("unadmitted historical interval accepted")
		}
	}
	if _, err := store.HistoricalAuthorityProjection(ctx, authority.ID, "sha256:substituted", completedAt); err == nil {
		t.Fatal("substituted context digest accepted")
	}
	// An intact prefix must not hide corruption in later canonical history.
	key, _ := encodeKey(KeyAuthorityFact, []string{authority.ID}, 2)
	if err := store.db.Update(func(txn *badgerdb.Txn) error { return txn.Set(key, []byte(`{}`)) }); err != nil {
		t.Fatal(err)
	}
	if _, err := store.HistoricalAuthorityProjection(ctx, authority.ID, authority.Digest, completedAt); err == nil {
		t.Fatal("historical prefix ignored corrupt later facts")
	}
}
