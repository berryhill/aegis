package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func TestExactPersistedRevocation(t *testing.T) {
	for _, tc := range []struct {
		name, record, reason       string
		version                    uint64
		whole, missingTime, wantOK bool
	}{
		{"exact", "record-a", "generated-proof", 1, false, false, true},
		{"wrong-record", "record-b", "generated-proof", 1, false, false, false},
		{"wrong-version", "record-a", "generated-proof", 2, false, false, false},
		{"wrong-reason", "record-a", "different", 1, false, false, false},
		{"whole-record", "record-a", "generated-proof", 1, true, false, false},
		{"missing-time", "record-a", "generated-proof", 1, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "authority.db")
			db, err := bolt.Open(path, 0600, nil)
			if err != nil {
				t.Fatal(err)
			}
			err = db.Update(func(tx *bolt.Tx) error {
				bucket, err := tx.CreateBucket([]byte("revocations"))
				if err != nil {
					return err
				}
				at := time.Now().UTC()
				if tc.missingTime {
					at = time.Time{}
				}
				value, err := json.Marshal(map[string]any{"reason": tc.reason, "at": at})
				if err != nil {
					return err
				}
				if tc.whole {
					if err = bucket.Put(versionKey(tc.record, 0), value); err != nil {
						return err
					}
				}
				return bucket.Put(versionKey(tc.record, tc.version), value)
			})
			if err != nil {
				t.Fatal(err)
			}
			if err = db.Close(); err != nil {
				t.Fatal(err)
			}
			if err = verify(path, "record-a", 1); (err == nil) != tc.wantOK {
				t.Fatalf("readback accepted=%v, want %v", err == nil, tc.wantOK)
			}
		})
	}
}
