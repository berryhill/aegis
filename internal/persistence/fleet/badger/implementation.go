package badger

import (
	"bytes"
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/implementation"
	badgerdb "github.com/dgraph-io/badger/v4"
)

// ImplementationStore keeps native-check custody in the existing fleet writer,
// with the same durability, reserve, and close lifecycle (not a second engine).
func (s *Store) ImplementationStore() implementation.Store { return implementationStore{s} }

type implementationStore struct{ owner *Store }

func implementationKey(k []byte) bool {
	return bytes.HasPrefix(k, []byte("implementation/run/")) || bytes.HasPrefix(k, []byte("implementation/blob/"))
}
func (s implementationStore) Get(k []byte) ([]byte, error) {
	if !implementationKey(k) {
		return nil, errors.New("invalid implementation custody key")
	}
	var b []byte
	// Completion revalidation runs while the owner holds its mutation mutex.
	// A read-only engine snapshot avoids recursively acquiring that mutex; the
	// engine also rejects reads after close. Writes still use the owner lifecycle.
	err := s.owner.db.View(func(tx *badgerdb.Txn) error {
		i, e := tx.Get(k)
		if e != nil {
			return e
		}
		b, e = i.ValueCopy(nil)
		return e
	})
	if errors.Is(err, badgerdb.ErrKeyNotFound) {
		err = implementation.ErrNotFound
	}
	return b, err
}
func (s implementationStore) Put(k, b []byte) error    { return s.write(k, b, false) }
func (s implementationStore) Create(k, b []byte) error { return s.write(k, b, true) }
func (s implementationStore) write(k, b []byte, exclusive bool) error {
	if !implementationKey(k) {
		return errors.New("invalid implementation custody key")
	}
	return s.owner.update(context.Background(), func(tx *badgerdb.Txn) error {
		if exclusive {
			_, err := tx.Get(k)
			if err == nil {
				return errors.New("run already reserved; budget cannot reset")
			}
			if !errors.Is(err, badgerdb.ErrKeyNotFound) {
				return err
			}
		}
		return tx.Set(k, b)
	})
}
