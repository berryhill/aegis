package badger

import (
	"errors"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	badgerdb "github.com/dgraph-io/badger/v4"
)

// Native immutable validation reads the SAME fleet completion transaction.
type contributionCompletionSnapshot struct{ txn *badgerdb.Txn }

func (s contributionCompletionSnapshot) Get(k []byte) ([]byte, error) {
	if !implementationKey(k) {
		return nil, fleet.ErrConflict
	}
	item, e := s.txn.Get(k)
	if errors.Is(e, badgerdb.ErrKeyNotFound) {
		return nil, implementation.ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	return item.ValueCopy(nil)
}
func (s contributionCompletionSnapshot) Put([]byte, []byte) error    { return fleet.ErrConflict }
func (s contributionCompletionSnapshot) Create([]byte, []byte) error { return fleet.ErrConflict }
