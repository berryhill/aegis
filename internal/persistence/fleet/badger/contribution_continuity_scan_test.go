package badger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/contribution"
	badgerdb "github.com/dgraph-io/badger/v4"
)

// Seed a valid retained chain in one fixture transaction. This is not runtime
// authority: normal owning-service tests independently exercise admission.
func TestContributionContinuityScanRetainsEveryCanonicalByte(t *testing.T) {
	s, c := continuityStore(t)
	ctx := context.Background()
	var state contribution.Continuity
	wires := make([][]byte, 256)
	for i := range wires {
		kind := "reserve"
		if i == 0 {
			kind = "initialize"
		}
		e := continuityEvent(t, c, state, kind)
		if i > 0 {
			e.OperationID = "same-retained-turn"
			var err error
			e, err = contribution.SealContinuityEvent(e)
			if err != nil {
				t.Fatal(err)
			}
		}
		var err error
		state, err = contribution.ApplyContinuity(c, state, e)
		if err != nil {
			t.Fatal(err)
		}
		wires[i], err = json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := s.update(ctx, func(tx *badgerdb.Txn) error {
		for i, wire := range wires {
			if err := create(tx, key(familyContributionContinuity, c.QueueItemID, fmt.Sprint(i+1)), wire); err != nil {
				return err
			}
		}
		return tx.Set(key(familyContributionContinuity, c.QueueItemID, "head"), wires[len(wires)-1])
	}); err != nil {
		t.Fatal(err)
	}
	warm, events, err := s.GetContributionContinuity(ctx, c.QueueItemID)
	if err != nil || !reflect.DeepEqual(warm, state) || len(events) != len(wires) {
		t.Fatal("complete cold reconstruction failed", err)
	}
	started := time.Now()
	for range 64 {
		got, history, err := s.GetContributionContinuity(ctx, c.QueueItemID)
		if err != nil || !reflect.DeepEqual(got, warm) || !reflect.DeepEqual(history, events) {
			t.Fatal("warm reconstruction drift", err)
		}
	}
	t.Logf("full-canonical-byte-scan entries=%d reloads=64 elapsed=%s", len(wires), time.Since(started))

	for _, fault := range []string{"old-byte-substitution", "missing-old-key", "extra-key"} {
		t.Run(fault, func(t *testing.T) {
			k := key(familyContributionContinuity, c.QueueItemID, "2")
			if err := s.update(ctx, func(tx *badgerdb.Txn) error {
				switch fault {
				case "old-byte-substitution":
					return tx.Set(k, bytes.Replace(wires[1], []byte("same-retained-turn"), []byte("fake-retained-turn"), 1))
				case "missing-old-key":
					return tx.Delete(k)
				default:
					return tx.Set(key(familyContributionContinuity, c.QueueItemID, "unexpected"), wires[1])
				}
			}); err != nil {
				t.Fatal(err)
			}
			if _, _, err := s.GetContributionContinuity(ctx, c.QueueItemID); err == nil {
				t.Fatal("warm scan hid altered canonical history")
			}
			if err := s.update(ctx, func(tx *badgerdb.Txn) error {
				if fault == "extra-key" {
					return tx.Delete(key(familyContributionContinuity, c.QueueItemID, "unexpected"))
				}
				return tx.Set(k, wires[1])
			}); err != nil {
				t.Fatal(err)
			}
			if _, _, err := s.GetContributionContinuity(ctx, c.QueueItemID); err != nil {
				t.Fatal("exact restored fixture did not reconstruct", err)
			}
		})
	}
}
