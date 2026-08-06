package badger

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	badgerdb "github.com/dgraph-io/badger/v4"
)

func TestProcessAuthorityCommandCommitsReceiptProjectionOutboxAndAdmission(t *testing.T) {
	store, authority := openCommandStore(t)
	ctx := context.Background()
	recordedAt := authority.IssuedAt.Add(10 * time.Second)
	activate := badgerAuthorityCommand("command-activate", core.AuthorityCommandActivate, authority, 1, "", recordedAt.Add(-time.Second))

	receipt, err := store.ProcessAuthorityCommand(ctx, activate, recordedAt, "aegis-controller")
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Accepted || receipt.CommandID != activate.ID || receipt.CommandDigest != activate.Digest || receipt.FactID == "" || receipt.ProjectionDigest == "" {
		t.Fatalf("accepted receipt does not bind all authority results: %+v", receipt)
	}
	storedReceipt, err := store.GetAuthorityReceipt(ctx, activate.ID)
	if err != nil || storedReceipt.Digest != receipt.Digest {
		t.Fatalf("receipt lookup changed result: receipt=%+v err=%v", storedReceipt, err)
	}

	projection, err := store.CurrentAuthorityProjection(ctx, authority.ID)
	if err != nil {
		t.Fatal(err)
	}
	if projection.State != core.AuthorityStateActive || projection.SourceSequence != 1 || projection.Digest != receipt.ProjectionDigest {
		t.Fatalf("unexpected current projection: %+v", projection)
	}
	outbox, err := store.AuthorityOutbox(ctx, authority.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(outbox) != 1 || outbox[0].CommandDigest != activate.Digest || outbox[0].FactDigest != receipt.FactDigest || outbox[0].ReceiptID != receipt.ID || outbox[0].ProjectionDigest != projection.Digest {
		t.Fatalf("outbox does not bind atomic command result: %+v", outbox)
	}
	admission, err := store.AuthorityAdmission(ctx, authority.ID, authority.Digest, recordedAt)
	if err != nil || !admission.Admitted || admission.ReasonCode != "admitted" || admission.Projection.Digest != projection.Digest {
		t.Fatalf("active authority was not admitted from a consistent view: view=%+v err=%v", admission, err)
	}
	denied, err := store.AuthorityAdmission(ctx, authority.ID, "sha256:substituted", recordedAt)
	if err != nil || denied.Admitted || denied.ReasonCode != "context_mismatch" {
		t.Fatalf("substituted authority context digest did not fail closed: view=%+v err=%v", denied, err)
	}

	// An exact retry returns the durable receipt even after the command's own
	// acceptance window. It must not append another fact or outbox entry.
	retried, err := store.ProcessAuthorityCommand(ctx, activate, activate.ExpiresAt.Add(time.Second), "different-recorder")
	if err != nil || retried.Digest != receipt.Digest {
		t.Fatalf("exact retry did not return the original receipt: receipt=%+v err=%v", retried, err)
	}
	outbox, err = store.AuthorityOutbox(ctx, authority.ID)
	if err != nil || len(outbox) != 1 {
		t.Fatalf("exact retry created duplicate authority work: outbox=%+v err=%v", outbox, err)
	}

	revokeAt := recordedAt.Add(time.Minute)
	revoke := badgerAuthorityCommand("command-revoke", core.AuthorityCommandRevoke, authority, 2, projection.SourceFactDigest, revokeAt.Add(-time.Second))
	revokedReceipt, err := store.ProcessAuthorityCommand(ctx, revoke, revokeAt, "aegis-controller")
	if err != nil {
		t.Fatal(err)
	}
	revoked, err := store.CurrentAuthorityProjection(ctx, authority.ID)
	if err != nil || revoked.State != core.AuthorityStateRevoked || revoked.SourceSequence != 2 || revoked.Digest != revokedReceipt.ProjectionDigest {
		t.Fatalf("revocation was not current: projection=%+v err=%v", revoked, err)
	}
	denied, err = store.AuthorityAdmission(ctx, authority.ID, authority.Digest, revokeAt)
	if err != nil || denied.Admitted || denied.ReasonCode != "authority_inactive" {
		t.Fatalf("revoked authority did not fail closed: view=%+v err=%v", denied, err)
	}
}

func TestAuthorityAuditDeliveryGatesGrantReadinessAtCommittedPosition(t *testing.T) {
	store, authority := openCommandStore(t)
	ctx := context.Background()
	recordedAt := authority.IssuedAt.Add(10 * time.Second)
	activate := badgerAuthorityCommand("command-audit-ready", core.AuthorityCommandActivate, authority, 1, "", recordedAt.Add(-time.Second))
	receipt, err := store.ProcessAuthorityCommand(ctx, activate, recordedAt, "aegis-controller")
	if err != nil {
		t.Fatal(err)
	}

	committed, err := store.CommittedAuthorityPosition(ctx, authority.ID)
	if err != nil {
		t.Fatal(err)
	}
	if committed.AuthorityContextID != authority.ID || committed.Sequence != 1 || committed.FactDigest != receipt.FactDigest || committed.ProjectionDigest != receipt.ProjectionDigest || core.ValidateCommittedAuthorityPosition(committed) != nil {
		t.Fatalf("committed position does not bind replay-verified authority: %+v", committed)
	}

	view, grantedAt, err := store.AuthorityReadiness(ctx, authority.ID, authority.Digest, recordedAt)
	if err != nil || view.Admitted || view.ReasonCode != "audit_delivery_lagging" || grantedAt != (core.CommittedAuthorityPosition{}) {
		t.Fatalf("authority granted before canonical audit delivery: view=%+v position=%+v err=%v", view, grantedAt, err)
	}

	delivered, err := store.DeliverAuthorityAudit(ctx, authority.ID, 1)
	if err != nil || len(delivered) != 1 {
		t.Fatalf("canonical audit delivery failed: evidence=%+v err=%v", delivered, err)
	}
	evidence := delivered[0]
	if evidence.Position != committed || evidence.AuthorityOutboxDigest == "" || evidence.RecordedAt != recordedAt || core.ValidateAuthorityAuditEvidence(evidence) != nil {
		t.Fatalf("audit evidence does not bind exact committed position: %+v", evidence)
	}

	view, grantedAt, err = store.AuthorityReadiness(ctx, authority.ID, authority.Digest, recordedAt)
	if err != nil || !view.Admitted || view.ReasonCode != "admitted" || grantedAt != committed {
		t.Fatalf("delivered authority did not grant at the committed position: view=%+v position=%+v err=%v", view, grantedAt, err)
	}

	retry, err := store.DeliverAuthorityAudit(ctx, authority.ID, 1)
	if err != nil || len(retry) != 0 {
		t.Fatalf("exact audit retry was not idempotent: evidence=%+v err=%v", retry, err)
	}
	stored, err := store.AuthorityAuditEvidence(ctx, authority.ID)
	if err != nil || len(stored) != 1 || stored[0] != evidence {
		t.Fatalf("audit retry changed canonical evidence: evidence=%+v err=%v", stored, err)
	}
}

func TestAuthorityAuditDeliveryAndReadinessFailClosedOnSubstitution(t *testing.T) {
	store, authority := openCommandStore(t)
	ctx := context.Background()
	recordedAt := authority.IssuedAt.Add(10 * time.Second)
	activate := badgerAuthorityCommand("command-audit-corrupt", core.AuthorityCommandActivate, authority, 1, "", recordedAt.Add(-time.Second))
	if _, err := store.ProcessAuthorityCommand(ctx, activate, recordedAt, "aegis-controller"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DeliverAuthorityAudit(ctx, authority.ID, 1); err != nil {
		t.Fatal(err)
	}

	key, _ := encodeKey(KeyAuthorityAudit, []string{authority.ID}, 1)
	if err := store.db.Update(func(txn *badgerdb.Txn) error {
		encoded, err := getValue(txn, key)
		if err != nil {
			return err
		}
		evidence, err := core.DecodeAuthorityAuditEvidenceCanonical(encoded)
		if err != nil {
			return err
		}
		evidence.Position.ProjectionDigest = "sha256:substituted"
		evidence.Position.Digest = core.CommittedAuthorityPositionDigest(evidence.Position)
		evidence.Digest = core.AuthorityAuditEvidenceDigest(evidence)
		encoded, err = core.EncodeAuthorityAuditEvidenceCanonical(evidence)
		if err != nil {
			return err
		}
		return txn.Set(key, encoded)
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := store.AuthorityAuditEvidence(ctx, authority.ID); !errors.Is(err, ErrCorruptRecord) {
		t.Fatalf("substituted audit evidence error=%v, want ErrCorruptRecord", err)
	}
	view, position, err := store.AuthorityReadiness(ctx, authority.ID, authority.Digest, recordedAt)
	if !errors.Is(err, ErrCorruptRecord) || view.Admitted || view.ReasonCode != "audit_unverifiable" || position != (core.CommittedAuthorityPosition{}) {
		t.Fatalf("substituted audit evidence did not fail readiness closed: view=%+v position=%+v err=%v", view, position, err)
	}
	if _, err = store.DeliverAuthorityAudit(ctx, authority.ID, 1); !errors.Is(err, ErrCorruptRecord) {
		t.Fatalf("delivery accepted substituted existing evidence: %v", err)
	}
}

func TestAuthorityAuditDeliveryRejectsInvalidBatchLimits(t *testing.T) {
	store, authority := openCommandStore(t)
	for _, limit := range []int{-1, 1001} {
		if _, err := store.DeliverAuthorityAudit(context.Background(), authority.ID, limit); err == nil {
			t.Fatalf("invalid audit delivery limit %d was accepted", limit)
		}
	}
}

func TestProcessAuthorityCommandConcurrentExactRetriesDeduplicate(t *testing.T) {
	store, authority := openCommandStore(t)
	ctx := context.Background()
	recordedAt := authority.IssuedAt.Add(10 * time.Second)
	command := badgerAuthorityCommand("command-concurrent", core.AuthorityCommandActivate, authority, 1, "", recordedAt.Add(-time.Second))

	const workers = 16
	start := make(chan struct{})
	results := make(chan core.AuthorityReceipt, workers)
	errorsSeen := make(chan error, workers)
	var wait sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			<-start
			receipt, err := store.ProcessAuthorityCommand(ctx, command, recordedAt, fmt.Sprintf("controller-%d", worker))
			results <- receipt
			errorsSeen <- err
		}(worker)
	}
	close(start)
	wait.Wait()
	close(results)
	close(errorsSeen)
	for err := range errorsSeen {
		if err != nil {
			t.Fatalf("concurrent exact retry failed: %v", err)
		}
	}
	var digest string
	for receipt := range results {
		if digest == "" {
			digest = receipt.Digest
		}
		if receipt.Digest != digest {
			t.Fatalf("concurrent retry returned multiple receipts: %q and %q", digest, receipt.Digest)
		}
	}
	outbox, err := store.AuthorityOutbox(ctx, authority.ID)
	if err != nil || len(outbox) != 1 {
		t.Fatalf("concurrent retries committed more than one result: outbox=%+v err=%v", outbox, err)
	}
}

func TestProcessAuthorityCommandDenialsAndTransactionFailureLeaveNoPartialState(t *testing.T) {
	t.Run("conflicting digest for command ID", func(t *testing.T) {
		store, authority := openCommandStore(t)
		recordedAt := authority.IssuedAt.Add(10 * time.Second)
		command := badgerAuthorityCommand("command-conflict", core.AuthorityCommandActivate, authority, 1, "", recordedAt.Add(-time.Second))
		original, err := store.ProcessAuthorityCommand(context.Background(), command, recordedAt, "controller")
		if err != nil {
			t.Fatal(err)
		}
		conflict := command
		conflict.Reason = "different authenticated intent"
		conflict.Digest = core.AuthorityCommandDigest(conflict)
		if _, err = store.ProcessAuthorityCommand(context.Background(), conflict, recordedAt, "controller"); !errors.Is(err, ErrAuthorityCommandConflict) {
			t.Fatalf("command ID digest conflict error=%v, want ErrAuthorityCommandConflict", err)
		}
		stored, err := store.GetAuthorityReceipt(context.Background(), command.ID)
		if err != nil || stored.Digest != original.Digest {
			t.Fatalf("conflict changed durable receipt: receipt=%+v err=%v", stored, err)
		}
	})

	t.Run("acceptance denial", func(t *testing.T) {
		store, authority := openCommandStore(t)
		recordedAt := authority.IssuedAt.Add(10 * time.Second)
		command := badgerAuthorityCommand("command-denied", core.AuthorityCommandActivate, authority, 1, "", recordedAt.Add(-time.Second))
		command.ActorSubjectID = "subject-substituted"
		command.Digest = core.AuthorityCommandDigest(command)
		if _, err := store.ProcessAuthorityCommand(context.Background(), command, recordedAt, "controller"); err == nil {
			t.Fatal("command from a substituted actor was accepted")
		}
		assertNoCommandResult(t, store, authority.ID, command.ID)
	})

	t.Run("authority expiry boundary", func(t *testing.T) {
		store, authority := openCommandStore(t)
		command := badgerAuthorityCommand("command-expired-authority", core.AuthorityCommandActivate, authority, 1, "", authority.ExpiresAt.Add(-time.Second))
		command.ExpiresAt = authority.ExpiresAt.Add(time.Minute)
		command.Digest = core.AuthorityCommandDigest(command)
		if _, err := store.ProcessAuthorityCommand(context.Background(), command, authority.ExpiresAt, "controller"); err == nil {
			t.Fatal("command was accepted at the authority context expiry boundary")
		}
		assertNoCommandResult(t, store, authority.ID, command.ID)
	})

	t.Run("late transaction collision rolls back every new record", func(t *testing.T) {
		store, authority := openCommandStore(t)
		recordedAt := authority.IssuedAt.Add(10 * time.Second)
		command := badgerAuthorityCommand("command-rollback", core.AuthorityCommandActivate, authority, 1, "", recordedAt.Add(-time.Second))
		outboxKey, err := encodeKey(KeyAuthorityOutbox, []string{authority.ID}, 1)
		if err != nil {
			t.Fatal(err)
		}
		if err = store.db.Update(func(txn *badgerdb.Txn) error { return txn.Set(outboxKey, []byte("collision")) }); err != nil {
			t.Fatal(err)
		}
		if _, err = store.ProcessAuthorityCommand(context.Background(), command, recordedAt, "controller"); !errors.Is(err, ErrAlreadyExists) {
			t.Fatalf("late transaction collision error=%v, want ErrAlreadyExists", err)
		}
		assertNoCommandResult(t, store, authority.ID, command.ID)
		if err = store.db.View(func(txn *badgerdb.Txn) error {
			value, loadErr := getValue(txn, outboxKey)
			if loadErr != nil || string(value) != "collision" {
				return fmt.Errorf("preexisting collision record changed: value=%q err=%v", value, loadErr)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestAuthorityReadsFailClosedOnDerivedRecordSubstitution(t *testing.T) {
	t.Run("projection", func(t *testing.T) {
		store, authority := openCommandStore(t)
		recordedAt := authority.IssuedAt.Add(10 * time.Second)
		command := badgerAuthorityCommand("command-projection-corrupt", core.AuthorityCommandActivate, authority, 1, "", recordedAt.Add(-time.Second))
		if _, err := store.ProcessAuthorityCommand(context.Background(), command, recordedAt, "controller"); err != nil {
			t.Fatal(err)
		}
		key, _ := encodeKey(KeyAuthorityProjection, []string{authority.ID}, 0)
		if err := store.db.Update(func(txn *badgerdb.Txn) error { return txn.Set(key, []byte("{}")) }); err != nil {
			t.Fatal(err)
		}
		if _, err := store.CurrentAuthorityProjection(context.Background(), authority.ID); !errors.Is(err, ErrCorruptRecord) {
			t.Fatalf("substituted projection error=%v, want ErrCorruptRecord", err)
		}
		if view, err := store.AuthorityAdmission(context.Background(), authority.ID, authority.Digest, recordedAt); !errors.Is(err, ErrCorruptRecord) || view.Admitted {
			t.Fatalf("substituted projection admission=%+v err=%v, want fail closed", view, err)
		}
	})

	t.Run("outbox canonical links", func(t *testing.T) {
		store, authority := openCommandStore(t)
		recordedAt := authority.IssuedAt.Add(10 * time.Second)
		command := badgerAuthorityCommand("command-outbox-corrupt", core.AuthorityCommandActivate, authority, 1, "", recordedAt.Add(-time.Second))
		if _, err := store.ProcessAuthorityCommand(context.Background(), command, recordedAt, "controller"); err != nil {
			t.Fatal(err)
		}
		key, _ := encodeKey(KeyAuthorityOutbox, []string{authority.ID}, 1)
		var entry core.AuthorityOutboxEntry
		if err := store.db.View(func(txn *badgerdb.Txn) error {
			encoded, err := getValue(txn, key)
			if err != nil {
				return err
			}
			entry, err = core.DecodeAuthorityOutboxEntryCanonical(encoded)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		entry.ReceiptID = "receipt-substituted"
		entry.Digest = core.AuthorityOutboxEntryDigest(entry)
		encoded, err := core.EncodeAuthorityOutboxEntryCanonical(entry)
		if err != nil {
			t.Fatal(err)
		}
		if err = store.db.Update(func(txn *badgerdb.Txn) error { return txn.Set(key, encoded) }); err != nil {
			t.Fatal(err)
		}
		if _, err = store.AuthorityOutbox(context.Background(), authority.ID); !errors.Is(err, ErrCorruptRecord) {
			t.Fatalf("outbox with substituted canonical links error=%v, want ErrCorruptRecord", err)
		}
	})
}

func openCommandStore(t *testing.T) (*Store, core.AuthorityContext) {
	t.Helper()
	store := openAuthorityStore(t)
	mandate, authority := badgerAuthorityBinding("mandate-command", "authority-command", "session-command")
	if err := store.CreateMandate(context.Background(), mandate); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateAuthorityContext(context.Background(), authority); err != nil {
		t.Fatal(err)
	}
	return store, authority
}

func badgerAuthorityCommand(id string, kind core.AuthorityCommandKind, authority core.AuthorityContext, sequence uint64, previous string, issuedAt time.Time) core.AuthorityCommand {
	command := core.AuthorityCommand{
		ID: id, Kind: kind, MandateID: authority.MandateID, AuthorityContextID: authority.ID,
		ExpectedSequence: sequence, ExpectedPreviousDigest: previous,
		ActorSubjectID: authority.SubjectID, ActorAuthentication: "authenticated-principal-session",
		IssuedAt: issuedAt, ExpiresAt: issuedAt.Add(30 * time.Second), Reason: "operator decision",
	}
	command.Digest = core.AuthorityCommandDigest(command)
	return command
}

func assertNoCommandResult(t *testing.T, store *Store, contextID, commandID string) {
	t.Helper()
	if _, err := store.GetAuthorityReceipt(context.Background(), commandID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("denied command left receipt: %v", err)
	}
	if _, err := store.CurrentAuthorityProjection(context.Background(), contextID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("denied command left projection or canonical state: %v", err)
	}
	for _, family := range []KeyFamily{KeyAuthorityCommand, KeyAuthorityReceipt} {
		key, _ := encodeKey(family, []string{commandID}, 0)
		if err := store.db.View(func(txn *badgerdb.Txn) error {
			_, err := getValue(txn, key)
			return err
		}); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("denied command left family %x record: %v", family, err)
		}
	}
	factKey, _ := encodeKey(KeyAuthorityFact, []string{contextID}, 1)
	if err := store.db.View(func(txn *badgerdb.Txn) error {
		_, err := getValue(txn, factKey)
		return err
	}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("denied command left fact: %v", err)
	}
}

func TestRebuildAuthorityProjectionsReplacesDerivedStateFromCanonicalRecords(t *testing.T) {
	store, authority := openCommandStore(t)
	ctx := context.Background()
	recordedAt := authority.IssuedAt.Add(10 * time.Second)
	command := badgerAuthorityCommand("command-rebuild", core.AuthorityCommandActivate, authority, 1, "", recordedAt.Add(-time.Second))
	if _, err := store.ProcessAuthorityCommand(ctx, command, recordedAt, "controller"); err != nil {
		t.Fatal(err)
	}
	key, _ := encodeKey(KeyAuthorityProjection, []string{authority.ID}, 0)
	if err := store.db.Update(func(txn *badgerdb.Txn) error { return txn.Set(key, []byte("{}")) }); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CurrentAuthorityProjection(ctx, authority.ID); !errors.Is(err, ErrCorruptRecord) {
		t.Fatalf("corrupt projection remained readable before rebuild: %v", err)
	}

	rebuilt, err := store.RebuildAuthorityProjections(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rebuilt) != 1 || rebuilt[0].AuthorityContextID != authority.ID || rebuilt[0].State != core.AuthorityStateActive {
		t.Fatalf("unexpected rebuilt projections: %+v", rebuilt)
	}
	projection, err := store.CurrentAuthorityProjection(ctx, authority.ID)
	if err != nil || projection != rebuilt[0] {
		t.Fatalf("rebuilt projection was not durable: projection=%+v err=%v", projection, err)
	}
}

func TestRebuildAuthorityProjectionsIsAtomicOnCorruptCanonicalState(t *testing.T) {
	store, authority := openCommandStore(t)
	ctx := context.Background()
	recordedAt := authority.IssuedAt.Add(10 * time.Second)
	command := badgerAuthorityCommand("command-rebuild-denied", core.AuthorityCommandActivate, authority, 1, "", recordedAt.Add(-time.Second))
	if _, err := store.ProcessAuthorityCommand(ctx, command, recordedAt, "controller"); err != nil {
		t.Fatal(err)
	}
	projectionKey, _ := encodeKey(KeyAuthorityProjection, []string{authority.ID}, 0)
	factKey, _ := encodeKey(KeyAuthorityFact, []string{authority.ID}, 1)
	var before []byte
	if err := store.db.Update(func(txn *badgerdb.Txn) error {
		var err error
		before, err = getValue(txn, projectionKey)
		if err != nil {
			return err
		}
		return txn.Set(factKey, []byte("{}"))
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RebuildAuthorityProjections(ctx); !errors.Is(err, ErrCorruptRecord) {
		t.Fatalf("canonical corruption error=%v, want ErrCorruptRecord", err)
	}
	if err := store.db.View(func(txn *badgerdb.Txn) error {
		after, err := getValue(txn, projectionKey)
		if err != nil {
			return err
		}
		if string(after) != string(before) {
			return errors.New("failed rebuild changed derived projection")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
