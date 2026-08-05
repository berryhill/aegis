package badger

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/berryhill/aegis/internal/core"
	badgerdb "github.com/dgraph-io/badger/v4"
)

var (
	ErrAlreadyExists = errors.New("authority record already exists")
	ErrCorruptRecord = errors.New("authority persistence record is corrupt")
)

var _ core.AuthorityRepository = (*Store)(nil)

func validateMandateRecord(mandate core.Mandate) error {
	if err := validateKeyIdentifier(mandate.ID); err != nil {
		return fmt.Errorf("invalid mandate ID: %w", err)
	}
	if mandate.Subject.ID == "" || mandate.AgentID == "" || mandate.StanzaID == "" ||
		mandate.CharterRevision == 0 || mandate.CharterDigest == "" || mandate.Runtime.Runtime == "" ||
		mandate.IssuedAt.IsZero() || !mandate.ExpiresAt.After(mandate.IssuedAt) {
		return errors.New("mandate is incomplete or has an invalid lifetime")
	}
	if mandate.Subject.ExpiresAt.Before(mandate.IssuedAt) || mandate.ExpiresAt.After(mandate.Subject.ExpiresAt) {
		return errors.New("mandate lifetime exceeds authenticated subject lifetime")
	}
	return nil
}

func (s *Store) withView(ctx context.Context, operation func(*badgerdb.Txn) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.db.View(operation)
}

func (s *Store) withUpdate(ctx context.Context, operation func(*badgerdb.Txn) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.db.Update(operation)
}

func getValue(txn *badgerdb.Txn, key []byte) ([]byte, error) {
	item, err := txn.Get(key)
	if errors.Is(err, badgerdb.ErrKeyNotFound) {
		return nil, os.ErrNotExist
	}
	if err != nil {
		return nil, err
	}
	return item.ValueCopy(nil)
}

func createValue(txn *badgerdb.Txn, key, value []byte) error {
	if _, err := txn.Get(key); err == nil {
		return ErrAlreadyExists
	} else if !errors.Is(err, badgerdb.ErrKeyNotFound) {
		return err
	}
	return txn.Set(key, value)
}

func scanFamily(txn *badgerdb.Txn, family KeyFamily, visit func(BinaryKey, []byte) error) error {
	prefix, err := familyPrefix(family)
	if err != nil {
		return err
	}
	iterator := txn.NewIterator(badgerdb.DefaultIteratorOptions)
	defer iterator.Close()
	for iterator.Seek(prefix); iterator.ValidForPrefix(prefix); iterator.Next() {
		item := iterator.Item()
		decoded, decodeErr := DecodeKey(item.Key())
		if decodeErr != nil || decoded.Family != family {
			return fmt.Errorf("%w: invalid key in family %x", ErrCorruptRecord, byte(family))
		}
		value, valueErr := item.ValueCopy(nil)
		if valueErr != nil {
			return valueErr
		}
		if visitErr := visit(decoded, value); visitErr != nil {
			return visitErr
		}
	}
	return nil
}

func mandateFromTxn(txn *badgerdb.Txn, id string) (core.Mandate, error) {
	key, err := encodeKey(KeyMandate, []string{id}, 0)
	if err != nil {
		return core.Mandate{}, err
	}
	encoded, err := getValue(txn, key)
	if err != nil {
		return core.Mandate{}, err
	}
	mandate, err := core.DecodeMandateCanonical(encoded)
	if err != nil || mandate.ID != id {
		return core.Mandate{}, fmt.Errorf("%w: mandate key/value identity mismatch", ErrCorruptRecord)
	}
	if err = validateMandateRecord(mandate); err != nil {
		return core.Mandate{}, fmt.Errorf("%w: invalid mandate: %v", ErrCorruptRecord, err)
	}
	return mandate, nil
}

func (s *Store) CreateMandate(ctx context.Context, mandate core.Mandate) error {
	if err := validateMandateRecord(mandate); err != nil {
		return err
	}
	key, err := encodeKey(KeyMandate, []string{mandate.ID}, 0)
	if err != nil {
		return err
	}
	encoded, err := core.EncodeMandateCanonical(mandate)
	if err != nil {
		return err
	}
	return s.withUpdate(ctx, func(txn *badgerdb.Txn) error { return createValue(txn, key, encoded) })
}

func (s *Store) GetMandate(ctx context.Context, id string) (core.Mandate, error) {
	var mandate core.Mandate
	err := s.withView(ctx, func(txn *badgerdb.Txn) error {
		var err error
		mandate, err = mandateFromTxn(txn, id)
		return err
	})
	return mandate, err
}

func (s *Store) ListMandates(ctx context.Context) ([]core.Mandate, error) {
	values := make([]core.Mandate, 0)
	err := s.withView(ctx, func(txn *badgerdb.Txn) error {
		return scanFamily(txn, KeyMandate, func(key BinaryKey, encoded []byte) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			value, err := core.DecodeMandateCanonical(encoded)
			if err != nil || value.ID != key.Identifiers[0] {
				return fmt.Errorf("%w: mandate key/value identity mismatch", ErrCorruptRecord)
			}
			if err = validateMandateRecord(value); err != nil {
				return fmt.Errorf("%w: invalid mandate: %v", ErrCorruptRecord, err)
			}
			values = append(values, value)
			return nil
		})
	})
	sort.Slice(values, func(i, j int) bool { return values[i].ID < values[j].ID })
	return values, err
}

func authorityContextFromTxn(txn *badgerdb.Txn, id string) (core.AuthorityContext, error) {
	key, err := encodeKey(KeyAuthorityContext, []string{id}, 0)
	if err != nil {
		return core.AuthorityContext{}, err
	}
	encoded, err := getValue(txn, key)
	if err != nil {
		return core.AuthorityContext{}, err
	}
	authority, err := core.DecodeAuthorityContextCanonical(encoded)
	if err != nil || authority.ID != id {
		return core.AuthorityContext{}, fmt.Errorf("%w: authority context key/value identity mismatch", ErrCorruptRecord)
	}
	mandate, err := mandateFromTxn(txn, authority.MandateID)
	if err != nil {
		return core.AuthorityContext{}, err
	}
	if err = core.ValidateAuthorityContext(authority, mandate); err != nil {
		return core.AuthorityContext{}, fmt.Errorf("%w: invalid authority context: %v", ErrCorruptRecord, err)
	}
	sessionKey, err := encodeKey(KeyContextBySession, []string{authority.SessionID}, 0)
	if err != nil {
		return core.AuthorityContext{}, fmt.Errorf("%w: invalid stored session ID", ErrCorruptRecord)
	}
	indexedID, err := getValue(txn, sessionKey)
	if err != nil || !bytes.Equal(indexedID, []byte(authority.ID)) {
		return core.AuthorityContext{}, fmt.Errorf("%w: session index does not bind authority context", ErrCorruptRecord)
	}
	return authority, nil
}

func (s *Store) CreateAuthorityContext(ctx context.Context, authority core.AuthorityContext) error {
	if err := validateKeyIdentifier(authority.ID); err != nil {
		return fmt.Errorf("invalid authority context ID: %w", err)
	}
	if err := validateKeyIdentifier(authority.SessionID); err != nil {
		return fmt.Errorf("invalid runtime session ID: %w", err)
	}
	contextKey, _ := encodeKey(KeyAuthorityContext, []string{authority.ID}, 0)
	sessionKey, _ := encodeKey(KeyContextBySession, []string{authority.SessionID}, 0)
	encoded, err := core.EncodeAuthorityContextCanonical(authority)
	if err != nil {
		return err
	}
	return s.withUpdate(ctx, func(txn *badgerdb.Txn) error {
		mandate, loadErr := mandateFromTxn(txn, authority.MandateID)
		if loadErr != nil {
			return fmt.Errorf("load authority context mandate: %w", loadErr)
		}
		if validateErr := core.ValidateAuthorityContext(authority, mandate); validateErr != nil {
			return validateErr
		}
		if _, getErr := txn.Get(contextKey); getErr == nil {
			return ErrAlreadyExists
		} else if !errors.Is(getErr, badgerdb.ErrKeyNotFound) {
			return getErr
		}
		if _, getErr := txn.Get(sessionKey); getErr == nil {
			return errors.New("runtime session already has an immutable authority context")
		} else if !errors.Is(getErr, badgerdb.ErrKeyNotFound) {
			return getErr
		}
		if setErr := txn.Set(contextKey, encoded); setErr != nil {
			return setErr
		}
		return txn.Set(sessionKey, []byte(authority.ID))
	})
}

func (s *Store) GetAuthorityContext(ctx context.Context, id string) (core.AuthorityContext, error) {
	var authority core.AuthorityContext
	err := s.withView(ctx, func(txn *badgerdb.Txn) error {
		var err error
		authority, err = authorityContextFromTxn(txn, id)
		return err
	})
	return authority, err
}

func (s *Store) ListAuthorityContexts(ctx context.Context) ([]core.AuthorityContext, error) {
	values := make([]core.AuthorityContext, 0)
	err := s.withView(ctx, func(txn *badgerdb.Txn) error {
		return scanFamily(txn, KeyAuthorityContext, func(key BinaryKey, _ []byte) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			value, err := authorityContextFromTxn(txn, key.Identifiers[0])
			if err != nil {
				return err
			}
			values = append(values, value)
			return nil
		})
	})
	sort.Slice(values, func(i, j int) bool { return values[i].ID < values[j].ID })
	return values, err
}

func transitionFactsFromTxn(txn *badgerdb.Txn, contextID string) ([]core.AuthorityTransitionFact, error) {
	prefix, err := identifierPrefix(KeyTransitionFact, contextID)
	if err != nil {
		return nil, err
	}
	facts := make([]core.AuthorityTransitionFact, 0)
	iterator := txn.NewIterator(badgerdb.DefaultIteratorOptions)
	defer iterator.Close()
	for iterator.Seek(prefix); iterator.ValidForPrefix(prefix); iterator.Next() {
		item := iterator.Item()
		key, decodeErr := DecodeKey(item.Key())
		if decodeErr != nil || key.Family != KeyTransitionFact || key.Identifiers[0] != contextID {
			return nil, fmt.Errorf("%w: invalid transition key", ErrCorruptRecord)
		}
		encoded, valueErr := item.ValueCopy(nil)
		if valueErr != nil {
			return nil, valueErr
		}
		fact, decodeErr := core.DecodeAuthorityTransitionFactCanonical(encoded)
		if decodeErr != nil || fact.AuthorityContextID != contextID || fact.Sequence != key.Sequence {
			return nil, fmt.Errorf("%w: transition key/value identity mismatch", ErrCorruptRecord)
		}
		facts = append(facts, fact)
	}
	return facts, nil
}

func verifyTransitionRootFromTxn(txn *badgerdb.Txn, contextID string, facts []core.AuthorityTransitionFact) (core.AuthorityTransitionRoot, error) {
	rootKey, err := encodeKey(KeyTransitionRoot, []string{contextID}, 0)
	if err != nil {
		return core.AuthorityTransitionRoot{}, err
	}
	if len(facts) == 0 {
		if _, getErr := txn.Get(rootKey); getErr == nil {
			return core.AuthorityTransitionRoot{}, fmt.Errorf("%w: transition root exists without facts", ErrCorruptRecord)
		} else if !errors.Is(getErr, badgerdb.ErrKeyNotFound) {
			return core.AuthorityTransitionRoot{}, getErr
		}
		return core.AuthorityTransitionRoot{}, nil
	}
	replayed, err := core.ReplayAuthorityTransitions(facts)
	if err != nil {
		return core.AuthorityTransitionRoot{}, err
	}
	encoded, err := getValue(txn, rootKey)
	if err != nil {
		return core.AuthorityTransitionRoot{}, fmt.Errorf("%w: transition root missing", ErrCorruptRecord)
	}
	stored, err := core.DecodeAuthorityTransitionRootCanonical(encoded)
	if err != nil || stored.AuthorityContextID != contextID || stored.MandateID != replayed.MandateID || stored.Digest != replayed.Digest {
		return core.AuthorityTransitionRoot{}, fmt.Errorf("%w: transition root key/value or replay mismatch", ErrCorruptRecord)
	}
	return stored, nil
}

func (s *Store) AppendAuthorityTransitionFact(ctx context.Context, fact core.AuthorityTransitionFact) (core.AuthorityTransitionRoot, error) {
	if err := validateKeyIdentifier(fact.AuthorityContextID); err != nil {
		return core.AuthorityTransitionRoot{}, fmt.Errorf("invalid transition authority context ID: %w", err)
	}
	fact.Digest = core.AuthorityTransitionFactDigest(fact)
	factKey, err := encodeKey(KeyTransitionFact, []string{fact.AuthorityContextID}, fact.Sequence)
	if err != nil {
		return core.AuthorityTransitionRoot{}, err
	}
	rootKey, _ := encodeKey(KeyTransitionRoot, []string{fact.AuthorityContextID}, 0)
	factEncoded, err := core.EncodeAuthorityTransitionFactCanonical(fact)
	if err != nil {
		return core.AuthorityTransitionRoot{}, err
	}
	var root core.AuthorityTransitionRoot
	err = s.withUpdate(ctx, func(txn *badgerdb.Txn) error {
		authority, loadErr := authorityContextFromTxn(txn, fact.AuthorityContextID)
		if loadErr != nil {
			return fmt.Errorf("load transition authority context: %w", loadErr)
		}
		if fact.MandateID != authority.MandateID {
			return errors.New("authority transition does not bind the stored context mandate")
		}
		facts, replayErr := transitionFactsFromTxn(txn, fact.AuthorityContextID)
		if replayErr != nil {
			return replayErr
		}
		if _, replayErr = verifyTransitionRootFromTxn(txn, fact.AuthorityContextID, facts); replayErr != nil {
			return replayErr
		}
		facts = append(facts, fact)
		root, replayErr = core.ReplayAuthorityTransitions(facts)
		if replayErr != nil {
			return replayErr
		}
		if _, getErr := txn.Get(factKey); getErr == nil {
			return ErrAlreadyExists
		} else if !errors.Is(getErr, badgerdb.ErrKeyNotFound) {
			return getErr
		}
		rootEncoded, encodeErr := core.EncodeAuthorityTransitionRootCanonical(root)
		if encodeErr != nil {
			return encodeErr
		}
		if setErr := txn.Set(factKey, factEncoded); setErr != nil {
			return setErr
		}
		return txn.Set(rootKey, rootEncoded)
	})
	return root, err
}

func (s *Store) AuthorityTransitionFacts(ctx context.Context, contextID string) ([]core.AuthorityTransitionFact, error) {
	var facts []core.AuthorityTransitionFact
	err := s.withView(ctx, func(txn *badgerdb.Txn) error {
		if _, err := authorityContextFromTxn(txn, contextID); err != nil {
			return err
		}
		var err error
		facts, err = transitionFactsFromTxn(txn, contextID)
		if err != nil {
			return err
		}
		_, err = verifyTransitionRootFromTxn(txn, contextID, facts)
		return err
	})
	return facts, err
}

func (s *Store) AuthorityTransitionRoot(ctx context.Context, contextID string) (core.AuthorityTransitionRoot, error) {
	var root core.AuthorityTransitionRoot
	err := s.withView(ctx, func(txn *badgerdb.Txn) error {
		if _, err := authorityContextFromTxn(txn, contextID); err != nil {
			return err
		}
		facts, err := transitionFactsFromTxn(txn, contextID)
		if err != nil {
			return err
		}
		root, err = verifyTransitionRootFromTxn(txn, contextID, facts)
		if err != nil {
			return err
		}
		return nil
	})
	return root, err
}
