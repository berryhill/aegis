// Proof-only readback of generated-material custody after the server has stopped.
// This opens an existing database read-only and never loads keys or secret values.
package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	bolt "go.etcd.io/bbolt"
)

func versionKey(record string, version uint64) []byte {
	key := make([]byte, len(record)+9)
	copy(key, record)
	binary.BigEndian.PutUint64(key[len(record)+1:], version)
	return key
}

func verify(path, record string, version uint64) error {
	if record == "" || version == 0 {
		return errors.New("exact record and nonzero version required")
	}
	db, err := bolt.Open(path, 0600, &bolt.Options{ReadOnly: true, Timeout: time.Second})
	if err != nil {
		return errors.New("read-only custody unavailable")
	}
	defer db.Close()
	return db.View(func(tx *bolt.Tx) error {
		revocations := tx.Bucket([]byte("revocations"))
		if revocations == nil {
			return errors.New("revocation bucket missing")
		}
		var entry struct {
			Reason string    `json:"reason"`
			At     time.Time `json:"at"`
		}
		if json.Unmarshal(revocations.Get(versionKey(record, version)), &entry) != nil || entry.Reason != "generated-proof" || entry.At.IsZero() {
			return errors.New("exact persisted version revocation missing")
		}
		if revocations.Get(versionKey(record, 0)) != nil {
			return errors.New("version revoke unexpectedly revoked whole record")
		}
		return nil
	})
}

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: verify-credential-revocation DATABASE RECORD VERSION")
		os.Exit(2)
	}
	version, err := strconv.ParseUint(os.Args[3], 10, 64)
	if err == nil {
		err = verify(os.Args[1], os.Args[2], version)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "persisted revocation proof failed")
		os.Exit(1)
	}
	fmt.Println("exact persisted version revocation verified; whole-record revocation absent")
}
