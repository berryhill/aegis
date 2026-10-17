package badger

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	badgerdb "github.com/dgraph-io/badger/v4"
)

// Compare exact current key/value bytes across the engine's working-copy flush.
func recoveryCanonicalDigest(ctx context.Context, db *badgerdb.DB) (string, error) {
	h := sha256.New()
	err := db.View(func(txn *badgerdb.Txn) error {
		it := txn.NewIterator(badgerdb.DefaultIteratorOptions)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			if e := ctx.Err(); e != nil {
				return e
			}
			item := it.Item()
			v, e := item.ValueCopy(nil)
			if e != nil {
				return e
			}
			k := item.Key()
			var n [8]byte
			for _, b := range [][]byte{k, v} {
				binary.BigEndian.PutUint64(n[:], uint64(len(b)))
				h.Write(n[:])
				h.Write(b)
			}
			binary.BigEndian.PutUint64(n[:], item.Version())
			h.Write(n[:])
			binary.BigEndian.PutUint64(n[:], item.ExpiresAt())
			h.Write(n[:])
			h.Write([]byte{item.UserMeta()})
		}
		return nil
	})
	return hex.EncodeToString(h.Sum(nil)), err
}
