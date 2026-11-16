package badger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	badgerdb "github.com/dgraph-io/badger/v4"
)

func setContractWire(t *testing.T, s *Store, id string, wire []byte) {
	t.Helper()
	if err := s.update(context.Background(), func(tx *badgerdb.Txn) error { return tx.Set(key(familyContribution, id), wire) }); err != nil {
		t.Fatal(err)
	}
}

func TestContributionDecodeCacheFreshBytes(t *testing.T) {
	s, c := continuityStore(t)
	ctx := context.Background()
	wire, err := contribution.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	warm := func() {
		t.Helper()
		setContractWire(t, s, c.QueueItemID, wire)
		got, e := s.GetContributionForQueue(ctx, c.QueueItemID)
		if e != nil || !reflect.DeepEqual(got, c) {
			t.Fatalf("warm: %v", e)
		}
		if !bytes.Equal(s.contributionReplay.decoded.wire, wire) {
			t.Fatal("not populated")
		}
	}
	for name, bad := range map[string][]byte{
		"malformed":         []byte("{"),
		"trailing":          append(bytes.Clone(wire), []byte(" {}")...),
		"duplicate":         append([]byte(`{"owner_id":"owner",`), wire[1:]...),
		"unknown":           append([]byte(`{"unrecognized":true,`), wire[1:]...),
		"same-length-drift": bytes.Replace(wire, []byte(`"repair"`), []byte(`"REPAIR"`), 1),
		"digest-drift":      bytes.Replace(wire, []byte(c.Digest), []byte(contribution.Digest([]byte("different"))), 1),
	} {
		t.Run(name, func(t *testing.T) {
			warm()
			if _, e := contribution.Unmarshal(bad); e == nil {
				t.Fatal("bad fixture passes cold validator")
			}
			setContractWire(t, s, c.QueueItemID, bad)
			for range 2 {
				if _, e := s.GetContributionForQueue(ctx, c.QueueItemID); !errors.Is(e, fleet.ErrCorrupt) {
					t.Fatalf("warm accepted drift: %v", e)
				}
				if _, _, e := s.GetContributionContinuity(ctx, c.QueueItemID); !errors.Is(e, fleet.ErrCorrupt) {
					t.Fatalf("continuity accepted drift: %v", e)
				}
				if _, e := s.GetContributionControl(ctx, c.QueueItemID); !errors.Is(e, fleet.ErrCorrupt) {
					t.Fatalf("control accepted drift: %v", e)
				}
			}
		})
	}
	warm()
	if err := s.update(ctx, func(tx *badgerdb.Txn) error { return tx.Delete(key(familyContribution, c.QueueItemID)) }); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetContributionForQueue(ctx, c.QueueItemID); !errors.Is(err, fleet.ErrNotFound) {
		t.Fatalf("cached deletion: %v", err)
	}
	warm()
	setContractWire(t, s, "other", wire)
	if _, err := s.GetContributionForQueue(ctx, "other"); !errors.Is(err, fleet.ErrCorrupt) {
		t.Fatalf("cached wrong key: %v", err)
	}
	// Request-index and owner checks stay fresh even for an exact warm value.
	if err := s.update(ctx, func(tx *badgerdb.Txn) error {
		if err := tx.Set(key(familyContributionRequest, c.OwnerID, c.Key), []byte(c.QueueItemID)); err != nil {
			return err
		}
		return tx.Set(key(familyContributionRequest, "foreign-owner", c.Key), []byte(c.QueueItemID))
	}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.GetContribution(ctx, c.OwnerID, c.Key); err != nil || got.OwnerID != c.OwnerID {
		t.Fatalf("owner read: %v", err)
	}
	if _, err := s.GetContribution(ctx, "foreign-owner", c.Key); !errors.Is(err, fleet.ErrCorrupt) {
		t.Fatalf("cached foreign owner: %v", err)
	}
	if err := s.update(ctx, func(tx *badgerdb.Txn) error { return tx.Delete(key(familyContributionRequest, c.OwnerID, c.Key)) }); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetContribution(ctx, c.OwnerID, c.Key); !errors.Is(err, fleet.ErrNotFound) {
		t.Fatalf("cached request index: %v", err)
	}
	// A valid changed value is decoded anew, even with the same key/owner.
	c.Outcome = "replacement"
	c, err = contribution.Seal(c)
	if err != nil {
		t.Fatal(err)
	}
	wire, _ = contribution.Marshal(c)
	warm()
	// Noncanonical but valid whitespace retains exactly the cold codec behavior.
	wire = append([]byte(" \n"), wire...)
	warm()
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.GetContributionForQueue(cancelled, c.QueueItemID); !errors.Is(err, context.Canceled) {
		t.Fatalf("cached canceled read: %v", err)
	}
}

func TestContributionDecodeCacheIsolationLifetimeAndBound(t *testing.T) {
	s, c := continuityStore(t)
	ctx := context.Background()
	c.Invariants = []string{"immutable"}
	c, err := contribution.Seal(c)
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := contribution.Marshal(c)
	setContractWire(t, s, c.QueueItemID, wire)
	first, err := s.GetContributionForQueue(ctx, c.QueueItemID)
	if err != nil {
		t.Fatal(err)
	}
	first.Invariants[0] = "poison-cold"
	first.Roles[0].Runtime.Target = "poison-cold"
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			for range 8 {
				got, e := s.GetContributionForQueue(ctx, c.QueueItemID)
				if e != nil || !reflect.DeepEqual(got, c) {
					t.Errorf("isolated read: %v", e)
					return
				}
				got.Invariants[0] = "poison-warm"
				got.Roles[0].Runtime.Target = "poison-warm"
			}
		})
	}
	wg.Wait()
	// Independent stores never share parse objects, even for identical keys.
	other, _ := continuityStore(t)
	setContractWire(t, other, c.QueueItemID, []byte("invalid"))
	if _, err := other.GetContributionForQueue(ctx, c.QueueItemID); !errors.Is(err, fleet.ErrCorrupt) {
		t.Fatalf("cross-store leak: %v", err)
	}
	root := s.root
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if s.contributionReplay.decoded.wire != nil {
		t.Fatal("close retained contract")
	}
	s, err = Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.contributionReplay.decoded.wire != nil {
		t.Fatal("restart retained contract")
	}
	got, err := s.GetContributionForQueue(ctx, c.QueueItemID)
	if err != nil || !reflect.DeepEqual(got, c) {
		t.Fatalf("restart: %v", err)
	}
	// The original validator permits large invariant strings. Retention must be
	// bounded without changing validation or rejecting an otherwise valid value.
	c.Invariants = []string{strings.Repeat("x", contributionDecodeMaxBytes)}
	c, err = contribution.Seal(c)
	if err != nil {
		t.Fatal(err)
	}
	wire, _ = contribution.Marshal(c)
	setContractWire(t, s, c.QueueItemID, wire)
	got, err = s.GetContributionForQueue(ctx, c.QueueItemID)
	if err != nil || got.Digest != c.Digest {
		t.Fatalf("oversized cold: %v", err)
	}
	if s.contributionReplay.decoded.wire != nil {
		t.Fatal("oversized value retained")
	}
}

func TestContributionDecodeCacheFreshControlAndHistory(t *testing.T) {
	s, c := continuityStore(t)
	ctx := context.Background()
	state, err := s.AppendContributionContinuity(ctx, continuityEvent(t, c, contribution.Continuity{}, "initialize"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.GetContributionContinuity(ctx, c.QueueItemID); err != nil {
		t.Fatal(err)
	}
	if s.contributionReplay.decoded.wire == nil {
		t.Fatal("not warm")
	}
	if _, err = s.AppendContributionContinuity(ctx, continuityEvent(t, c, state, "cancel-requested")); err != nil {
		t.Fatal(err)
	}
	control, err := s.GetContributionControl(ctx, c.QueueItemID)
	if err != nil || !control.Stop {
		t.Fatalf("stale control: %+v %v", control, err)
	}
	state, _, err = s.GetContributionContinuity(ctx, c.QueueItemID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AppendContributionContinuity(ctx, continuityEvent(t, c, state, "reserve")); err == nil {
		t.Fatal("cached authority ignored cancellation")
	}
	// Warm replay cannot hide a changed contract even when replacement is sealed.
	c.OwnerID = "replacement-owner"
	c, err = contribution.Seal(c)
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(c)
	setContractWire(t, s, c.QueueItemID, wire)
	if _, _, err = s.GetContributionContinuity(ctx, c.QueueItemID); !errors.Is(err, fleet.ErrCorrupt) {
		t.Fatalf("stale history: %v", err)
	}
	if _, err = s.GetContributionControl(ctx, c.QueueItemID); !errors.Is(err, fleet.ErrCorrupt) {
		t.Fatalf("stale contract control: %v", err)
	}
}
