package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/berryhill/aegis/internal/core"
)

const (
	authorityMandatesKind    = "authority-mandates"
	authorityContextsKind    = "authority-contexts-v1"
	authorityTransitionsKind = "authority-transition-facts"
)

var _ core.AuthorityRepository = (*Store)(nil)

func validateMandateRecord(mandate core.Mandate) error {
	if mandate.ID == "" || mandate.Subject.ID == "" || mandate.AgentID == "" || mandate.StanzaID == "" ||
		mandate.CharterRevision == 0 || mandate.CharterDigest == "" || mandate.Runtime.Runtime == "" ||
		mandate.IssuedAt.IsZero() || !mandate.ExpiresAt.After(mandate.IssuedAt) {
		return errors.New("mandate is incomplete or has an invalid lifetime")
	}
	if mandate.Subject.ExpiresAt.Before(mandate.IssuedAt) || mandate.ExpiresAt.After(mandate.Subject.ExpiresAt) {
		return errors.New("mandate lifetime exceeds authenticated subject lifetime")
	}
	return nil
}

func (s *Store) createCanonical(ctx context.Context, kind, id string, encoded []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.objectPath(kind, id)
	if err != nil {
		return err
	}
	return s.withLock(func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return s.createCanonicalUnlocked(path, encoded)
	})
}

func (s *Store) createCanonicalUnlocked(path string, encoded []byte) error {
	if err := s.secureStoreDirectory(filepath.Dir(path), true); err != nil {
		return err
	}
	if info, statErr := os.Lstat(path); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("authority record target is a symlink")
		}
		return ErrAlreadyExists
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	return writeBytesCreateOnly(path, encoded)
}

func readCanonicalPath(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("authority record is not a regular file")
	}
	return os.ReadFile(path)
}

func (s *Store) authorityRecordPaths(kind string) ([]string, error) {
	directory := filepath.Join(s.root, kind)
	if err := s.secureStoreDirectory(directory, false); errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	} else if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return nil, errors.New("authority repository contains an unexpected entry")
		}
		paths = append(paths, filepath.Join(directory, entry.Name()))
	}
	sort.Strings(paths)
	return paths, nil
}

func (s *Store) CreateMandate(ctx context.Context, mandate core.Mandate) error {
	if err := validateMandateRecord(mandate); err != nil {
		return err
	}
	encoded, err := core.EncodeMandateCanonical(mandate)
	if err != nil {
		return err
	}
	return s.createCanonical(ctx, authorityMandatesKind, mandate.ID, encoded)
}

func (s *Store) GetMandate(ctx context.Context, id string) (core.Mandate, error) {
	if err := ctx.Err(); err != nil {
		return core.Mandate{}, err
	}
	path, err := s.objectPath(authorityMandatesKind, id)
	if err != nil {
		return core.Mandate{}, err
	}
	encoded, err := readCanonicalPath(path)
	if err != nil {
		return core.Mandate{}, err
	}
	mandate, err := core.DecodeMandateCanonical(encoded)
	if err != nil {
		return core.Mandate{}, err
	}
	if mandate.ID != id {
		return core.Mandate{}, errors.New("stored mandate ID does not match its repository key")
	}
	if err = validateMandateRecord(mandate); err != nil {
		return core.Mandate{}, fmt.Errorf("stored mandate is invalid: %w", err)
	}
	return mandate, nil
}

func (s *Store) ListMandates(ctx context.Context) ([]core.Mandate, error) {
	paths, err := s.authorityRecordPaths(authorityMandatesKind)
	if err != nil {
		return nil, err
	}
	values := make([]core.Mandate, 0, len(paths))
	for _, path := range paths {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		encoded, readErr := readCanonicalPath(path)
		if readErr != nil {
			return nil, readErr
		}
		value, decodeErr := core.DecodeMandateCanonical(encoded)
		if decodeErr != nil {
			return nil, decodeErr
		}
		if value.ID != strings.TrimSuffix(filepath.Base(path), ".json") {
			return nil, errors.New("stored mandate ID does not match its repository key")
		}
		if err = validateMandateRecord(value); err != nil {
			return nil, fmt.Errorf("stored mandate is invalid: %w", err)
		}
		values = append(values, value)
	}
	return values, nil
}

func (s *Store) CreateAuthorityContext(ctx context.Context, authority core.AuthorityContext) error {
	mandate, err := s.GetMandate(ctx, authority.MandateID)
	if err != nil {
		return fmt.Errorf("load authority context mandate: %w", err)
	}
	if err = core.ValidateAuthorityContext(authority, mandate); err != nil {
		return err
	}
	encoded, err := core.EncodeAuthorityContextCanonical(authority)
	if err != nil {
		return err
	}
	path, err := s.objectPath(authorityContextsKind, authority.ID)
	if err != nil {
		return err
	}
	return s.withLock(func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		contexts, listErr := s.ListAuthorityContexts(ctx)
		if listErr != nil {
			return listErr
		}
		for _, existing := range contexts {
			if existing.ID == authority.ID {
				return ErrAlreadyExists
			}
			if existing.SessionID == authority.SessionID {
				return errors.New("runtime session already has an immutable authority context")
			}
		}
		return s.createCanonicalUnlocked(path, encoded)
	})
}

func (s *Store) GetAuthorityContext(ctx context.Context, id string) (core.AuthorityContext, error) {
	if err := ctx.Err(); err != nil {
		return core.AuthorityContext{}, err
	}
	path, err := s.objectPath(authorityContextsKind, id)
	if err != nil {
		return core.AuthorityContext{}, err
	}
	encoded, err := readCanonicalPath(path)
	if err != nil {
		return core.AuthorityContext{}, err
	}
	authority, err := core.DecodeAuthorityContextCanonical(encoded)
	if err != nil {
		return core.AuthorityContext{}, err
	}
	if authority.ID != id {
		return core.AuthorityContext{}, errors.New("stored authority context ID does not match its repository key")
	}
	mandate, err := s.GetMandate(ctx, authority.MandateID)
	if err != nil {
		return core.AuthorityContext{}, err
	}
	if err = core.ValidateAuthorityContext(authority, mandate); err != nil {
		return core.AuthorityContext{}, fmt.Errorf("stored authority context is invalid: %w", err)
	}
	return authority, nil
}

func (s *Store) ListAuthorityContexts(ctx context.Context) ([]core.AuthorityContext, error) {
	paths, err := s.authorityRecordPaths(authorityContextsKind)
	if err != nil {
		return nil, err
	}
	values := make([]core.AuthorityContext, 0, len(paths))
	for _, path := range paths {
		id := strings.TrimSuffix(filepath.Base(path), ".json")
		value, getErr := s.GetAuthorityContext(ctx, id)
		if getErr != nil {
			return nil, getErr
		}
		values = append(values, value)
	}
	return values, nil
}

func transitionStoragePrefix(contextID string) string {
	digest := sha256.Sum256([]byte(contextID))
	return hex.EncodeToString(digest[:]) + "-"
}

func transitionStorageID(contextID string, sequence uint64) string {
	return fmt.Sprintf("%s%020d", transitionStoragePrefix(contextID), sequence)
}

func (s *Store) authorityTransitionFactsUnlocked(contextID string) ([]core.AuthorityTransitionFact, error) {
	paths, err := s.authorityRecordPaths(authorityTransitionsKind)
	if err != nil {
		return nil, err
	}
	prefix := transitionStoragePrefix(contextID)
	facts := make([]core.AuthorityTransitionFact, 0)
	for _, path := range paths {
		if !strings.HasPrefix(strings.TrimSuffix(filepath.Base(path), ".json"), prefix) {
			continue
		}
		encoded, readErr := readCanonicalPath(path)
		if readErr != nil {
			return nil, readErr
		}
		fact, decodeErr := core.DecodeAuthorityTransitionFactCanonical(encoded)
		if decodeErr != nil {
			return nil, decodeErr
		}
		storageID := strings.TrimSuffix(filepath.Base(path), ".json")
		if fact.AuthorityContextID != contextID || storageID != transitionStorageID(contextID, fact.Sequence) {
			return nil, errors.New("stored authority transition does not match its repository key")
		}
		facts = append(facts, fact)
	}
	return facts, nil
}

func (s *Store) AppendAuthorityTransitionFact(ctx context.Context, fact core.AuthorityTransitionFact) (core.AuthorityTransitionRoot, error) {
	if err := ctx.Err(); err != nil {
		return core.AuthorityTransitionRoot{}, err
	}
	authority, err := s.GetAuthorityContext(ctx, fact.AuthorityContextID)
	if err != nil {
		return core.AuthorityTransitionRoot{}, fmt.Errorf("load transition authority context: %w", err)
	}
	if fact.MandateID != authority.MandateID {
		return core.AuthorityTransitionRoot{}, errors.New("authority transition does not bind the stored context mandate")
	}
	fact.Digest = core.AuthorityTransitionFactDigest(fact)
	encoded, err := core.EncodeAuthorityTransitionFactCanonical(fact)
	if err != nil {
		return core.AuthorityTransitionRoot{}, err
	}
	path, err := s.objectPath(authorityTransitionsKind, transitionStorageID(fact.AuthorityContextID, fact.Sequence))
	if err != nil {
		return core.AuthorityTransitionRoot{}, err
	}
	var root core.AuthorityTransitionRoot
	err = s.withLock(func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		facts, replayErr := s.authorityTransitionFactsUnlocked(fact.AuthorityContextID)
		if replayErr != nil {
			return replayErr
		}
		facts = append(facts, fact)
		root, replayErr = core.ReplayAuthorityTransitions(facts)
		if replayErr != nil {
			return replayErr
		}
		if err := s.secureStoreDirectory(filepath.Dir(path), true); err != nil {
			return err
		}
		return writeBytesCreateOnly(path, encoded)
	})
	return root, err
}

func (s *Store) AuthorityTransitionFacts(ctx context.Context, contextID string) ([]core.AuthorityTransitionFact, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := s.GetAuthorityContext(ctx, contextID); err != nil {
		return nil, err
	}
	facts, err := s.authorityTransitionFactsUnlocked(contextID)
	if err != nil {
		return nil, err
	}
	if len(facts) > 0 {
		if _, err = core.ReplayAuthorityTransitions(facts); err != nil {
			return nil, err
		}
	}
	return facts, nil
}

func (s *Store) AuthorityTransitionRoot(ctx context.Context, contextID string) (core.AuthorityTransitionRoot, error) {
	facts, err := s.AuthorityTransitionFacts(ctx, contextID)
	if err != nil {
		return core.AuthorityTransitionRoot{}, err
	}
	return core.ReplayAuthorityTransitions(facts)
}
