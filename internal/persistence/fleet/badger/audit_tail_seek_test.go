package badger

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	badgerdb "github.com/dgraph-io/badger/v4"
)

func auditSeekFact() fleet.AuditFact {
	return fleet.AuditFact{Event: core.AuditEvent{Type: "fixture.audit", PrincipalID: "owner", SubjectID: "owner", Outcome: "succeeded", Reason: "audit_tail_seek_fixture"}}
}

func TestAuditTailSeekPreservesChainAcrossForeignFamiliesAndReopen(t *testing.T) {
	ctx := context.Background()
	s := newDeliveryStore(t)
	// Includes the exact upper boundary and opaque higher-family values. None
	// belongs to the audit chain or may change its sequence/predecessor selection.
	if err := s.update(ctx, func(txn *badgerdb.Txn) error {
		for _, k := range [][]byte{key(familyAudit + 1), key(familyAudit+1, "boundary-child"), key(familyContributionPublication, "opaque-higher-family")} {
			if err := create(txn, k, bytes.Repeat([]byte("opaque"), 1024)); err != nil {
				return err
			}
		}
		return appendAudit(txn, auditSeekFact())
	}); err != nil {
		t.Fatal(err)
	}
	first, err := s.AuditEvents(ctx)
	if err != nil || len(first) != 1 {
		t.Fatal("first audit", err, len(first))
	}
	if first[0].PreviousDigest != "" {
		t.Fatal("foreign family became a predecessor")
	}
	root := s.root
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err = reopened.update(ctx, func(txn *badgerdb.Txn) error { return appendAudit(txn, auditSeekFact()) }); err != nil {
		t.Fatal(err)
	}
	events, err := reopened.AuditEvents(ctx)
	if err != nil || len(events) != 2 {
		t.Fatal("reopened audit", err, len(events))
	}
	if core.Digest(events[0]) != core.Digest(first[0]) || events[1].PreviousDigest != first[0].EventDigest {
		t.Fatal("audit predecessor bytes/digest changed")
	}
	for n, e := range events {
		digest := e.EventDigest
		e.EventDigest = ""
		if digest != core.Digest(e) {
			t.Fatal("audit event seal changed", n)
		}
	}
}

func BenchmarkAuditTailWithHigherFamilyPayloads(b *testing.B) {
	ctx := context.Background()
	s, err := Open(ctx, b.TempDir()+"/fleet-v1")
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	if err = s.update(ctx, func(txn *badgerdb.Txn) error {
		for n := 0; n < 256; n++ {
			if e := create(txn, key(familyContributionPublication, fmt.Sprint(n)), bytes.Repeat([]byte("x"), 16384)); e != nil {
				return e
			}
		}
		return appendAudit(txn, auditSeekFact())
	}); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if err = s.update(ctx, func(txn *badgerdb.Txn) error { return appendAudit(txn, auditSeekFact()) }); err != nil {
			b.Fatal(err)
		}
	}
}
