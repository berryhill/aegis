package badger

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/berryhill/aegis/internal/core"
	badgerdb "github.com/dgraph-io/badger/v4"
)

var _ core.AuthorityCommandRepository = (*Store)(nil)

var ErrAuthorityCommandConflict = errors.New("authority command ID conflicts with a different command digest")

func authorityReceiptFromTxn(txn *badgerdb.Txn, commandID string) (core.AuthorityReceipt, error) {
	key, err := encodeKey(KeyAuthorityReceipt, []string{commandID}, 0)
	if err != nil {
		return core.AuthorityReceipt{}, err
	}
	encoded, err := getValue(txn, key)
	if err != nil {
		return core.AuthorityReceipt{}, err
	}
	receipt, err := core.DecodeAuthorityReceiptCanonical(encoded)
	if err != nil || receipt.CommandID != commandID || core.ValidateAuthorityReceipt(receipt) != nil {
		return core.AuthorityReceipt{}, fmt.Errorf("%w: invalid authority receipt", ErrCorruptRecord)
	}
	return receipt, nil
}

// verifiedAuthorityReceiptFromTxn proves that a receipt is backed by the exact
// canonical command/fact pair, its prefix projection, and the atomically
// emitted outbox entry. A syntactically valid receipt is never enough to claim
// that command processing completed.
func verifiedAuthorityReceiptFromTxn(txn *badgerdb.Txn, commandID string) (core.AuthorityReceipt, error) {
	receipt, err := authorityReceiptFromTxn(txn, commandID)
	if err != nil {
		return core.AuthorityReceipt{}, err
	}
	commandKey, _ := encodeKey(KeyAuthorityCommand, []string{commandID}, 0)
	encoded, err := getValue(txn, commandKey)
	if err != nil {
		return core.AuthorityReceipt{}, fmt.Errorf("%w: receipt lacks its canonical command", ErrCorruptRecord)
	}
	command, err := core.DecodeAuthorityCommandCanonical(encoded)
	if err != nil || command.ID != commandID || core.ValidateAuthorityCommand(command) != nil || receipt.CommandDigest != command.Digest {
		return core.AuthorityReceipt{}, fmt.Errorf("%w: receipt does not bind its canonical command", ErrCorruptRecord)
	}
	commands, facts, err := canonicalAuthorityFromTxn(txn, command.AuthorityContextID)
	if err != nil {
		return core.AuthorityReceipt{}, err
	}
	index := int(command.ExpectedSequence - 1)
	if index < 0 || index >= len(commands) || index >= len(facts) || commands[index].ID != command.ID {
		return core.AuthorityReceipt{}, fmt.Errorf("%w: receipt command is absent from canonical sequence", ErrCorruptRecord)
	}
	fact := facts[index]
	projection, err := core.ReplayCanonicalAuthority(commands[:index+1], facts[:index+1])
	if err != nil {
		return core.AuthorityReceipt{}, fmt.Errorf("%w: receipt prefix replay failed: %v", ErrCorruptRecord, err)
	}
	if !receipt.Accepted || receipt.FactID != fact.ID || receipt.FactDigest != fact.Digest || receipt.ProjectionDigest != projection.Digest || receipt.RecordedAt != fact.OccurredAt || receipt.RecordedBy != fact.RecordedBy {
		return core.AuthorityReceipt{}, fmt.Errorf("%w: receipt does not bind its canonical result", ErrCorruptRecord)
	}
	outboxKey, _ := encodeKey(KeyAuthorityOutbox, []string{command.AuthorityContextID}, command.ExpectedSequence)
	encoded, err = getValue(txn, outboxKey)
	if err != nil {
		return core.AuthorityReceipt{}, fmt.Errorf("%w: receipt lacks its atomic outbox entry", ErrCorruptRecord)
	}
	outbox, err := core.DecodeAuthorityOutboxEntryCanonical(encoded)
	if err != nil || core.ValidateAuthorityOutboxEntry(outbox) != nil || outbox.AuthorityContextID != command.AuthorityContextID || outbox.Sequence != command.ExpectedSequence ||
		outbox.CommandID != command.ID || outbox.CommandDigest != command.Digest || outbox.FactID != fact.ID || outbox.FactDigest != fact.Digest ||
		outbox.ReceiptID != receipt.ID || outbox.ProjectionDigest != projection.Digest || outbox.RecordedAt != receipt.RecordedAt {
		return core.AuthorityReceipt{}, fmt.Errorf("%w: receipt outbox entry does not bind its canonical result", ErrCorruptRecord)
	}
	return receipt, nil
}

func canonicalAuthorityFromTxn(txn *badgerdb.Txn, contextID string) ([]core.AuthorityCommand, []core.AuthorityFact, error) {
	commands := make([]core.AuthorityCommand, 0)
	if err := scanFamily(txn, KeyAuthorityCommand, func(key BinaryKey, encoded []byte) error {
		command, err := core.DecodeAuthorityCommandCanonical(encoded)
		if err != nil || command.ID != key.Identifiers[0] || core.ValidateAuthorityCommand(command) != nil {
			return fmt.Errorf("%w: invalid authority command", ErrCorruptRecord)
		}
		if command.AuthorityContextID == contextID {
			commands = append(commands, command)
		}
		return nil
	}); err != nil {
		return nil, nil, err
	}
	facts := make([]core.AuthorityFact, 0)
	prefix, err := identifierPrefix(KeyAuthorityFact, contextID)
	if err != nil {
		return nil, nil, err
	}
	iterator := txn.NewIterator(badgerdb.DefaultIteratorOptions)
	defer iterator.Close()
	for iterator.Seek(prefix); iterator.ValidForPrefix(prefix); iterator.Next() {
		item := iterator.Item()
		key, keyErr := DecodeKey(item.Key())
		encoded, valueErr := item.ValueCopy(nil)
		fact, factErr := core.DecodeAuthorityFactCanonical(encoded)
		if keyErr != nil || valueErr != nil || factErr != nil || key.Family != KeyAuthorityFact || key.Identifiers[0] != contextID || fact.AuthorityContextID != contextID || fact.Sequence != key.Sequence {
			return nil, nil, fmt.Errorf("%w: invalid authority fact", ErrCorruptRecord)
		}
		facts = append(facts, fact)
	}
	sort.Slice(commands, func(i, j int) bool { return commands[i].ExpectedSequence < commands[j].ExpectedSequence })
	return commands, facts, nil
}

func verifiedProjectionFromTxn(txn *badgerdb.Txn, contextID string) (core.AuthorityProjection, error) {
	commands, facts, err := canonicalAuthorityFromTxn(txn, contextID)
	if err != nil {
		return core.AuthorityProjection{}, err
	}
	if len(commands) == 0 || len(facts) == 0 {
		return core.AuthorityProjection{}, os.ErrNotExist
	}
	replayed, err := core.ReplayCanonicalAuthority(commands, facts)
	if err != nil {
		return core.AuthorityProjection{}, fmt.Errorf("%w: canonical authority replay failed: %v", ErrCorruptRecord, err)
	}
	key, _ := encodeKey(KeyAuthorityProjection, []string{contextID}, 0)
	encoded, err := getValue(txn, key)
	if err != nil {
		return core.AuthorityProjection{}, fmt.Errorf("%w: authority projection missing", ErrCorruptRecord)
	}
	stored, err := core.DecodeAuthorityProjectionCanonical(encoded)
	if err != nil || stored.AuthorityContextID != contextID || stored.Digest != replayed.Digest {
		return core.AuthorityProjection{}, fmt.Errorf("%w: authority projection diverges from canonical replay", ErrCorruptRecord)
	}
	return replayed, nil
}

func (s *Store) ProcessAuthorityCommand(ctx context.Context, command core.AuthorityCommand, recordedAt time.Time, recordedBy string) (core.AuthorityReceipt, error) {
	// Validate the immutable command shape before lookup. Its acceptance window is
	// checked only for a first attempt so an exact retry can recover its durable
	// receipt after the command window has elapsed.
	if err := core.ValidateAuthorityCommand(command); err != nil {
		return core.AuthorityReceipt{}, err
	}
	for name, value := range map[string]string{"command ID": command.ID, "authority context ID": command.AuthorityContextID, "mandate ID": command.MandateID, "recorder": recordedBy} {
		if err := validateKeyIdentifier(value); err != nil {
			return core.AuthorityReceipt{}, fmt.Errorf("invalid %s: %w", name, err)
		}
	}
	var receipt core.AuthorityReceipt
	err := s.withUpdate(ctx, func(txn *badgerdb.Txn) error {
		existing, getErr := verifiedAuthorityReceiptFromTxn(txn, command.ID)
		if getErr == nil {
			if existing.CommandDigest != command.Digest {
				return ErrAuthorityCommandConflict
			}
			receipt = existing
			return nil
		}
		if !errors.Is(getErr, os.ErrNotExist) {
			return getErr
		}
		if validateErr := core.ValidateAuthorityCommandAt(command, recordedAt); validateErr != nil {
			return validateErr
		}
		commandKey, _ := encodeKey(KeyAuthorityCommand, []string{command.ID}, 0)
		if _, loadErr := txn.Get(commandKey); loadErr == nil {
			return fmt.Errorf("%w: command exists without receipt", ErrCorruptRecord)
		} else if !errors.Is(loadErr, badgerdb.ErrKeyNotFound) {
			return loadErr
		}

		authority, loadErr := authorityContextFromTxn(txn, command.AuthorityContextID)
		if loadErr != nil {
			return loadErr
		}
		if command.MandateID != authority.MandateID || command.ActorSubjectID != authority.SubjectID || recordedAt.Before(authority.IssuedAt) || !recordedAt.Before(authority.ExpiresAt) {
			return errors.New("authority command does not bind the current authenticated authority context")
		}
		commands, facts, replayErr := canonicalAuthorityFromTxn(txn, authority.ID)
		if replayErr != nil {
			return replayErr
		}
		var current core.AuthorityProjection
		if len(commands) != 0 || len(facts) != 0 {
			current, replayErr = verifiedProjectionFromTxn(txn, authority.ID)
			if replayErr != nil {
				return replayErr
			}
		} else {
			projectionKey, _ := encodeKey(KeyAuthorityProjection, []string{authority.ID}, 0)
			if _, projectionErr := txn.Get(projectionKey); projectionErr == nil {
				return fmt.Errorf("%w: projection exists without canonical authority", ErrCorruptRecord)
			} else if !errors.Is(projectionErr, badgerdb.ErrKeyNotFound) {
				return projectionErr
			}
		}
		if command.ExpectedSequence != uint64(len(facts)+1) || command.ExpectedPreviousDigest != current.SourceFactDigest || (len(facts) == 0) != (command.Kind == core.AuthorityCommandActivate) || (len(facts) > 0 && current.State != core.AuthorityStateActive) {
			return errors.New("authority command precondition does not match verified canonical state")
		}

		to := core.AuthorityStateActive
		switch command.Kind {
		case core.AuthorityCommandRevoke:
			to = core.AuthorityStateRevoked
		case core.AuthorityCommandExpire:
			to = core.AuthorityStateExpired
		}
		fact := core.AuthorityFact{ID: "fact-" + command.ID, Sequence: command.ExpectedSequence, CommandID: command.ID, CommandDigest: command.Digest, MandateID: command.MandateID, AuthorityContextID: command.AuthorityContextID, From: current.State, To: to, OccurredAt: recordedAt, RecordedBy: recordedBy, PreviousDigest: current.SourceFactDigest}
		fact.Digest = core.AuthorityFactDigest(fact)
		projection, replayErr := core.ReplayCanonicalAuthority(append(commands, command), append(facts, fact))
		if replayErr != nil {
			return replayErr
		}
		receipt = core.AuthorityReceipt{ID: "receipt-" + command.ID, CommandID: command.ID, CommandDigest: command.Digest, Accepted: true, FactID: fact.ID, FactDigest: fact.Digest, ProjectionDigest: projection.Digest, ReasonCode: "accepted", RecordedAt: recordedAt, RecordedBy: recordedBy}
		receipt.Digest = core.AuthorityReceiptDigest(receipt)
		outbox := core.AuthorityOutboxEntry{ID: "outbox-" + command.ID, AuthorityContextID: authority.ID, Sequence: fact.Sequence, CommandID: command.ID, CommandDigest: command.Digest, FactID: fact.ID, FactDigest: fact.Digest, ReceiptID: receipt.ID, ProjectionDigest: projection.Digest, RecordedAt: recordedAt}
		outbox.Digest = core.AuthorityOutboxEntryDigest(outbox)

		commandEncoded, err := core.EncodeAuthorityCommandCanonical(command)
		if err != nil {
			return err
		}
		factEncoded, err := core.EncodeAuthorityFactCanonical(fact)
		if err != nil {
			return err
		}
		receiptEncoded, err := core.EncodeAuthorityReceiptCanonical(receipt)
		if err != nil {
			return err
		}
		projectionEncoded, err := core.EncodeAuthorityProjectionCanonical(projection)
		if err != nil {
			return err
		}
		outboxEncoded, err := core.EncodeAuthorityOutboxEntryCanonical(outbox)
		if err != nil {
			return err
		}
		factKey, _ := encodeKey(KeyAuthorityFact, []string{authority.ID}, fact.Sequence)
		receiptKey, _ := encodeKey(KeyAuthorityReceipt, []string{command.ID}, 0)
		projectionKey, _ := encodeKey(KeyAuthorityProjection, []string{authority.ID}, 0)
		outboxKey, _ := encodeKey(KeyAuthorityOutbox, []string{authority.ID}, fact.Sequence)
		for _, value := range []struct{ key, encoded []byte }{{commandKey, commandEncoded}, {factKey, factEncoded}, {receiptKey, receiptEncoded}, {outboxKey, outboxEncoded}} {
			if setErr := createValue(txn, value.key, value.encoded); setErr != nil {
				return setErr
			}
		}
		return txn.Set(projectionKey, projectionEncoded)
	})
	return receipt, err
}

func (s *Store) GetAuthorityReceipt(ctx context.Context, commandID string) (core.AuthorityReceipt, error) {
	var receipt core.AuthorityReceipt
	err := s.withView(ctx, func(txn *badgerdb.Txn) error {
		var err error
		receipt, err = verifiedAuthorityReceiptFromTxn(txn, commandID)
		return err
	})
	return receipt, err
}

func (s *Store) CurrentAuthorityProjection(ctx context.Context, contextID string) (core.AuthorityProjection, error) {
	var projection core.AuthorityProjection
	err := s.withView(ctx, func(txn *badgerdb.Txn) error {
		var err error
		projection, err = verifiedProjectionFromTxn(txn, contextID)
		return err
	})
	return projection, err
}

// RebuildAuthorityProjections replaces only derived projections after replaying
// canonical commands and facts. Invalid canonical state aborts atomically.
func (s *Store) RebuildAuthorityProjections(ctx context.Context) ([]core.AuthorityProjection, error) {
	var projections []core.AuthorityProjection
	err := s.withUpdate(ctx, func(txn *badgerdb.Txn) error {
		var err error
		projections, err = rebuildAuthorityProjectionsTxn(txn)
		return err
	})
	return projections, err
}

func rebuildAuthorityProjectionsTxn(txn *badgerdb.Txn) ([]core.AuthorityProjection, error) {
	contextIDs := make([]string, 0)
	if err := scanFamily(txn, KeyAuthorityContext, func(key BinaryKey, _ []byte) error {
		contextIDs = append(contextIDs, key.Identifiers[0])
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Strings(contextIDs)
	projections := make([]core.AuthorityProjection, 0, len(contextIDs))
	for _, contextID := range contextIDs {
		commands, facts, err := canonicalAuthorityFromTxn(txn, contextID)
		if err != nil {
			return nil, err
		}
		key, _ := encodeKey(KeyAuthorityProjection, []string{contextID}, 0)
		if len(commands) == 0 && len(facts) == 0 {
			if err = txn.Delete(key); err != nil {
				return nil, err
			}
			continue
		}
		projection, err := core.ReplayCanonicalAuthority(commands, facts)
		if err != nil {
			return nil, fmt.Errorf("%w: canonical authority replay failed: %v", ErrCorruptRecord, err)
		}
		encoded, err := core.EncodeAuthorityProjectionCanonical(projection)
		if err != nil {
			return nil, err
		}
		if err = txn.Set(key, encoded); err != nil {
			return nil, err
		}
		projections = append(projections, projection)
	}
	return projections, nil
}

func verifiedAuthorityOutboxFromTxn(txn *badgerdb.Txn, contextID string) ([]core.AuthorityOutboxEntry, core.AuthorityProjection, error) {
	entries := make([]core.AuthorityOutboxEntry, 0)
	projection, err := verifiedProjectionFromTxn(txn, contextID)
	if err != nil {
		return nil, core.AuthorityProjection{}, err
	}
	commands, facts, err := canonicalAuthorityFromTxn(txn, contextID)
	if err != nil {
		return nil, core.AuthorityProjection{}, err
	}
	prefix, err := identifierPrefix(KeyAuthorityOutbox, contextID)
	if err != nil {
		return nil, core.AuthorityProjection{}, err
	}
	iterator := txn.NewIterator(badgerdb.DefaultIteratorOptions)
	defer iterator.Close()
	for iterator.Seek(prefix); iterator.ValidForPrefix(prefix); iterator.Next() {
		key, keyErr := DecodeKey(iterator.Item().Key())
		encoded, valueErr := iterator.Item().ValueCopy(nil)
		entry, decodeErr := core.DecodeAuthorityOutboxEntryCanonical(encoded)
		index := len(entries)
		if keyErr != nil || valueErr != nil || decodeErr != nil || key.Sequence != uint64(index+1) || entry.AuthorityContextID != contextID || entry.Sequence != key.Sequence || core.ValidateAuthorityOutboxEntry(entry) != nil || index >= len(commands) || index >= len(facts) {
			return nil, core.AuthorityProjection{}, fmt.Errorf("%w: invalid authority outbox", ErrCorruptRecord)
		}
		command, fact := commands[index], facts[index]
		receipt, receiptErr := authorityReceiptFromTxn(txn, command.ID)
		prefixProjection, replayErr := core.ReplayCanonicalAuthority(commands[:index+1], facts[:index+1])
		if receiptErr != nil || replayErr != nil || !receipt.Accepted || entry.CommandID != command.ID || entry.CommandDigest != command.Digest ||
			entry.FactID != fact.ID || entry.FactDigest != fact.Digest || entry.ReceiptID != receipt.ID || entry.ProjectionDigest != prefixProjection.Digest ||
			receipt.FactID != fact.ID || receipt.FactDigest != fact.Digest || receipt.ProjectionDigest != prefixProjection.Digest || entry.RecordedAt != receipt.RecordedAt {
			return nil, core.AuthorityProjection{}, fmt.Errorf("%w: authority outbox does not bind canonical command result", ErrCorruptRecord)
		}
		entries = append(entries, entry)
	}
	if len(entries) != int(projection.SourceSequence) || len(entries) == 0 || entries[len(entries)-1].ProjectionDigest != projection.Digest {
		return nil, core.AuthorityProjection{}, fmt.Errorf("%w: authority outbox is not current", ErrCorruptRecord)
	}
	return entries, projection, nil
}

func (s *Store) AuthorityOutbox(ctx context.Context, contextID string) ([]core.AuthorityOutboxEntry, error) {
	var entries []core.AuthorityOutboxEntry
	err := s.withView(ctx, func(txn *badgerdb.Txn) error {
		var err error
		entries, _, err = verifiedAuthorityOutboxFromTxn(txn, contextID)
		return err
	})
	return entries, err
}

func positionFromOutbox(entry core.AuthorityOutboxEntry) core.CommittedAuthorityPosition {
	position := core.CommittedAuthorityPosition{AuthorityContextID: entry.AuthorityContextID, Sequence: entry.Sequence, FactDigest: entry.FactDigest, ProjectionDigest: entry.ProjectionDigest}
	position.Digest = core.CommittedAuthorityPositionDigest(position)
	return position
}

func authorityAuditEvidenceFromTxn(txn *badgerdb.Txn, contextID string, outbox []core.AuthorityOutboxEntry) ([]core.AuthorityAuditEvidence, error) {
	evidence := make([]core.AuthorityAuditEvidence, 0)
	prefix, err := identifierPrefix(KeyAuthorityAudit, contextID)
	if err != nil {
		return nil, err
	}
	iterator := txn.NewIterator(badgerdb.DefaultIteratorOptions)
	defer iterator.Close()
	for iterator.Seek(prefix); iterator.ValidForPrefix(prefix); iterator.Next() {
		key, keyErr := DecodeKey(iterator.Item().Key())
		encoded, valueErr := iterator.Item().ValueCopy(nil)
		record, decodeErr := core.DecodeAuthorityAuditEvidenceCanonical(encoded)
		index := len(evidence)
		if keyErr != nil || valueErr != nil || decodeErr != nil || key.Family != KeyAuthorityAudit || key.Sequence != uint64(index+1) || index >= len(outbox) || core.ValidateAuthorityAuditEvidence(record) != nil {
			return nil, fmt.Errorf("%w: invalid authority audit evidence", ErrCorruptRecord)
		}
		entry := outbox[index]
		expectedPosition := positionFromOutbox(entry)
		if record.ID != "audit-"+entry.ID || record.Position != expectedPosition || record.AuthorityOutboxDigest != entry.Digest || record.RecordedAt != entry.RecordedAt {
			return nil, fmt.Errorf("%w: authority audit evidence does not bind committed authority", ErrCorruptRecord)
		}
		evidence = append(evidence, record)
	}
	return evidence, nil
}

func (s *Store) CommittedAuthorityPosition(ctx context.Context, contextID string) (core.CommittedAuthorityPosition, error) {
	var position core.CommittedAuthorityPosition
	err := s.withView(ctx, func(txn *badgerdb.Txn) error {
		outbox, projection, err := verifiedAuthorityOutboxFromTxn(txn, contextID)
		if err != nil {
			return err
		}
		position = positionFromOutbox(outbox[len(outbox)-1])
		if position.Sequence != projection.SourceSequence || position.FactDigest != projection.SourceFactDigest || position.ProjectionDigest != projection.Digest || core.ValidateCommittedAuthorityPosition(position) != nil {
			return fmt.Errorf("%w: committed authority position diverges from replay", ErrCorruptRecord)
		}
		return nil
	})
	return position, err
}

// DeliverAuthorityAudit consumes replay-verified outbox entries in strict
// sequence. Evidence is deterministic and create-only, so an exact retry emits
// no duplicate record and a gap or substituted record fails closed.
func (s *Store) DeliverAuthorityAudit(ctx context.Context, contextID string, limit int) ([]core.AuthorityAuditEvidence, error) {
	if limit == 0 {
		limit = 100
	}
	if limit < 0 || limit > 1000 {
		return nil, errors.New("authority audit delivery batch exceeds limit")
	}
	delivered := make([]core.AuthorityAuditEvidence, 0)
	err := s.withUpdate(ctx, func(txn *badgerdb.Txn) error {
		outbox, _, err := verifiedAuthorityOutboxFromTxn(txn, contextID)
		if err != nil {
			return err
		}
		existing, err := authorityAuditEvidenceFromTxn(txn, contextID, outbox)
		if err != nil {
			return err
		}
		for index := len(existing); index < len(outbox) && len(delivered) < limit; index++ {
			entry := outbox[index]
			record := core.AuthorityAuditEvidence{ID: "audit-" + entry.ID, Position: positionFromOutbox(entry), AuthorityOutboxDigest: entry.Digest, RecordedAt: entry.RecordedAt}
			record.Digest = core.AuthorityAuditEvidenceDigest(record)
			if core.ValidateAuthorityAuditEvidence(record) != nil {
				return fmt.Errorf("%w: generated invalid authority audit evidence", ErrCorruptRecord)
			}
			encoded, encodeErr := core.EncodeAuthorityAuditEvidenceCanonical(record)
			if encodeErr != nil {
				return encodeErr
			}
			key, keyErr := encodeKey(KeyAuthorityAudit, []string{contextID}, entry.Sequence)
			if keyErr != nil {
				return keyErr
			}
			if createErr := createValue(txn, key, encoded); createErr != nil {
				return createErr
			}
			delivered = append(delivered, record)
		}
		return nil
	})
	return delivered, err
}

func (s *Store) AuthorityAuditEvidence(ctx context.Context, contextID string) ([]core.AuthorityAuditEvidence, error) {
	var evidence []core.AuthorityAuditEvidence
	err := s.withView(ctx, func(txn *badgerdb.Txn) error {
		outbox, _, err := verifiedAuthorityOutboxFromTxn(txn, contextID)
		if err != nil {
			return err
		}
		evidence, err = authorityAuditEvidenceFromTxn(txn, contextID, outbox)
		return err
	})
	return evidence, err
}

func authorityAdmissionFromTxn(txn *badgerdb.Txn, contextID, contextDigest string, at time.Time) (core.AuthorityAdmissionView, error) {
	view := core.AuthorityAdmissionView{EvaluatedAt: at, ReasonCode: "denied"}
	authority, err := authorityContextFromTxn(txn, contextID)
	if err != nil {
		return view, err
	}
	projection, err := verifiedProjectionFromTxn(txn, contextID)
	if err != nil {
		return view, err
	}
	view.AuthorityContext, view.Projection = authority, projection
	switch {
	case at.IsZero() || contextDigest == "" || authority.Digest != contextDigest:
		view.ReasonCode = "context_mismatch"
	case at.Before(authority.IssuedAt) || !at.Before(authority.ExpiresAt):
		view.ReasonCode = "outside_lifetime"
	case projection.State != core.AuthorityStateActive:
		view.ReasonCode = "authority_inactive"
	default:
		view.Admitted, view.ReasonCode = true, "admitted"
	}
	return view, nil
}

// AuthorityReadiness is the grant-producing read boundary. It admits only when
// canonical audit evidence has reached the exact replay-verified committed
// position observed in the same Badger snapshot.
func (s *Store) AuthorityReadiness(ctx context.Context, contextID, contextDigest string, at time.Time) (core.AuthorityAdmissionView, core.CommittedAuthorityPosition, error) {
	view := core.AuthorityAdmissionView{EvaluatedAt: at, ReasonCode: "denied"}
	var position core.CommittedAuthorityPosition
	err := s.withView(ctx, func(txn *badgerdb.Txn) error {
		var err error
		view, err = authorityAdmissionFromTxn(txn, contextID, contextDigest, at)
		if err != nil || !view.Admitted {
			return err
		}
		outbox, projection, err := verifiedAuthorityOutboxFromTxn(txn, contextID)
		if err != nil {
			view.Admitted, view.ReasonCode = false, "audit_unverifiable"
			return err
		}
		evidence, err := authorityAuditEvidenceFromTxn(txn, contextID, outbox)
		if err != nil {
			view.Admitted, view.ReasonCode = false, "audit_unverifiable"
			return err
		}
		position = positionFromOutbox(outbox[len(outbox)-1])
		if len(evidence) != len(outbox) || evidence[len(evidence)-1].Position != position || position.Sequence != projection.SourceSequence || position.FactDigest != projection.SourceFactDigest || position.ProjectionDigest != projection.Digest {
			view.Admitted, view.ReasonCode = false, "audit_delivery_lagging"
			position = core.CommittedAuthorityPosition{}
		}
		return nil
	})
	return view, position, err
}

func (s *Store) AuthorityAdmission(ctx context.Context, contextID, contextDigest string, at time.Time) (core.AuthorityAdmissionView, error) {
	view := core.AuthorityAdmissionView{EvaluatedAt: at, ReasonCode: "denied"}
	err := s.withView(ctx, func(txn *badgerdb.Txn) error {
		var err error
		view, err = authorityAdmissionFromTxn(txn, contextID, contextDigest, at)
		return err
	})
	return view, err
}
