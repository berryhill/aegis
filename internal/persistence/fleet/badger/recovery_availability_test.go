package badger

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	authoritybadger "github.com/berryhill/aegis/internal/persistence/authority/badger"
	badgerdb "github.com/dgraph-io/badger/v4"
)

func TestFleetRecoveryAvailabilityNormalWriter(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), schemaVersion))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	registration, agent := agentFixture(t)
	if _, err := s.RegisterAgent(ctx, registration, agent, audit("agent.registered", agent.AgentID)); err != nil {
		t.Fatal(err)
	}
	r, _ := loopFixture(t)
	r.LoopID = "doer-reusable"
	r, v, err := loop.NewRevision(r)
	if err != nil {
		t.Fatal(err)
	}
	p := loop.PublishRequest{Revision: r, Validation: v, Provenance: loopPublicationProvenance(t, r, v), IdempotencyKey: "recovery-publication"}
	if _, err := s.PublishLoop(ctx, p, audit("loop.published", r.LoopID)); err != nil {
		t.Fatal(err)
	}
	first := availabilityEvent(t, p, nil, "archive")
	appendAvailability(t, s, first)
	appendAvailability(t, s, availabilityEvent(t, p, &first, "restore"))
	if _, _, err := verifyFleetRecovery(context.Background(), s.db); err != nil {
		t.Fatal(err)
	}
}

func TestFleetRecoveryAvailabilityMalformed(t *testing.T) {
	for _, scenario := range []string{"head-json", "head-short", "head-wrong-digest", "head-missing", "head-extra-key-part", "head-orphan", "event-json", "event-wrong-key", "event-extra-key-part", "event-missing", "event-chain-gap", "event-owner", "event-publication"} {
		t.Run(scenario, func(t *testing.T) {
			s, p := availabilityFixture(t)
			e := availabilityEvent(t, p, nil, "archive")
			appendAvailability(t, s, e)
			hk, ek := key(familyLoopAvailabilityHead, e.LoopID), key(familyLoopAvailabilityEvent, e.EventID)
			err := s.db.Update(func(tx *badgerdb.Txn) error {
				switch scenario {
				case "head-json":
					return tx.Set(hk, []byte(`{"digest":"wrong"}`))
				case "head-short":
					return tx.Set(hk, []byte("sha256:abcd"))
				case "head-wrong-digest":
					return tx.Set(hk, []byte("sha256:"+strings.Repeat("a", 64)))
				case "head-missing":
					return tx.Delete(hk)
				case "head-extra-key-part":
					return tx.Set(key(familyLoopAvailabilityHead, e.LoopID, "extra"), []byte(e.Digest))
				case "head-orphan":
					return tx.Set(key(familyLoopAvailabilityHead, "orphan"), []byte(e.Digest))
				case "event-json":
					return tx.Set(ek, []byte(`{}`))
				case "event-missing":
					return tx.Delete(ek)
				}
				b, err := get(tx, ek)
				if err != nil {
					return err
				}
				switch scenario {
				case "event-wrong-key":
					return tx.Set(key(familyLoopAvailabilityEvent, "wrong"), b)
				case "event-extra-key-part":
					return tx.Set(key(familyLoopAvailabilityEvent, e.EventID, "extra"), b)
				}
				var r availabilityRecord
				if err := json.Unmarshal(b, &r); err != nil {
					return err
				}
				switch scenario {
				case "event-chain-gap":
					r.Event.Sequence = 2
					r.Event.PreviousDigest = "sha256:" + strings.Repeat("b", 64)
				case "event-owner":
					r.Event.OwnerID = "wrong-owner"
				case "event-publication":
					r.Event.Head.Digest = "sha256:" + strings.Repeat("a", 64)
				}
				r.Event, err = loop.NewAvailabilityEvent(r.Event)
				if err != nil {
					return err
				}
				// Keep both layers canonically sealed: semantic corruption must still deny.
				r.Digest = ""
				r.Digest = core.Digest(r)
				b, err = json.Marshal(r)
				if err != nil {
					return err
				}
				if err = tx.Set(hk, []byte(r.Event.Digest)); err != nil {
					return err
				}
				return tx.Set(ek, b)
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = verifyFleetRecovery(context.Background(), s.db); err == nil {
				t.Fatal("malformed availability admitted")
			}
		})
	}
}

// A byte-for-byte logical inventory includes every family, not just public readback.
func recoveryInventory(t *testing.T, s *Store) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	if err := s.db.View(func(tx *badgerdb.Txn) error {
		it := tx.NewIterator(badgerdb.DefaultIteratorOptions)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			b, err := it.Item().ValueCopy(nil)
			if err != nil {
				return err
			}
			out[string(it.Item().KeyCopy(nil))] = b
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestFleetRepresentativeRecoveryCrashChild(t *testing.T) {
	root := os.Getenv("AEGIS_REPRESENTATIVE_RECOVERY_CHILD")
	if root == "" {
		return
	}
	syscall.Umask(0077)
	ctx := context.Background()
	authority := filepath.Join(filepath.Dir(root), "authority-v1")
	if _, err := authoritybadger.Initialize(ctx, authority); err != nil {
		t.Fatal(err)
	}
	if _, err := authoritybadger.Open(ctx, authority); err != nil {
		t.Fatal(err)
	}
	s, completion, evidenceStore := completionFixture(t, ctx, root)
	if err := s.CompleteQueueItem(ctx, completion, completionAudit(completion), evidenceStore); err != nil {
		t.Fatal(err)
	}
	r, v := loopFixture(t)
	p := loop.PublishRequest{Revision: r, Validation: v, Provenance: loopPublicationProvenance(t, r, v)}
	activation, err := loop.NewLifecycleEvent(loop.LifecycleEvent{
		EventID: "recovery-activation", LoopID: r.LoopID, State: loop.LifecycleActive, Revision: p.Provenance.Loop,
		PublisherAgent: p.Provenance.PublisherAgent, Authority: p.Provenance.Authority,
		MandateID: p.Provenance.MandateID, StanzaID: p.Provenance.StanzaID, OccurredAt: time.Unix(100, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.AppendLoopLifecycle(ctx, loop.LifecycleRequest{Event: activation}, audit("loop.activated", r.LoopID)); err != nil {
		t.Fatal(err)
	}
	first := availabilityEvent(t, p, nil, "recovery-archive")
	appendAvailability(t, s, first)
	appendAvailability(t, s, availabilityEvent(t, p, &first, "recovery-restore"))
	// Encode binary keys as byte slices, not JSON strings (invalid UTF-8 is lossy).
	type entry struct{ Key, Value []byte }
	entries := []entry{}
	for k, v := range recoveryInventory(t, s) {
		entries = append(entries, entry{[]byte(k), v})
	}
	b, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(filepath.Dir(root), "fleet-before.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("AEGIS_REPRESENTATIVE_RECOVERY_MALFORMED") == "1" {
		setRawRecord(t, s, key(familyLoopAvailabilityHead, r.LoopID), []byte("sha256:malformed"))
	}
	if err = s.db.Sync(); err != nil {
		t.Fatal(err)
	}
	// Actual interrupted Badger writer; no Close or manufactured DIRTY marker.
	os.Exit(0)
}

func TestFleetRepresentativeInterruptedRecovery(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		name := "normal"
		if malformed {
			name = "malformed"
		}
		t.Run(name, func(t *testing.T) { representativeInterruptedRecovery(t, malformed) })
	}
}

func representativeInterruptedRecovery(t *testing.T, malformed bool) {
	ctx := context.Background()
	private := t.TempDir()
	if err := os.Chmod(private, 0700); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(private, "persistence")
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "fleet-v1")
	childCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	child := exec.CommandContext(childCtx, os.Args[0], "-test.run=^TestFleetRepresentativeRecoveryCrashChild$")
	child.Env = append(os.Environ(), "TMPDIR="+parent, "AEGIS_REPRESENTATIVE_RECOVERY_CHILD="+root)
	if malformed {
		child.Env = append(child.Env, "AEGIS_REPRESENTATIVE_RECOVERY_MALFORMED=1")
	}
	if b, err := child.CombinedOutput(); err != nil {
		t.Fatalf("child: %v: %s", err, b)
	}
	// A completed authority stage is retried before the still-interrupted fleet.
	authority := filepath.Join(parent, "authority-v1")
	a, err := authoritybadger.RecoverInterruptedAs(ctx, authority, "fixture-owner")
	if err != nil || !a.Recovered {
		t.Fatalf("authority: %+v %v", a, err)
	}
	again, err := authoritybadger.RecoverInterruptedAs(ctx, authority, "fixture-owner")
	if err != nil || again.EvidencePath != a.EvidencePath || again.Generation != a.Generation {
		t.Fatalf("authority retry: %+v %v", again, err)
	}
	if s, err := Open(ctx, root); err == nil {
		s.Close()
		t.Fatal("dirty store admitted")
	}
	recovered, err := RecoverInterruptedAs(ctx, root, "fixture-owner")
	if malformed {
		if err == nil {
			t.Fatal("malformed interrupted store admitted")
		}
		if _, err := os.Stat(filepath.Join(root, "CLEAN")); !os.IsNotExist(err) {
			t.Fatal("malformed recovery published CLEAN")
		}
		if _, err := RecoverInterruptedAs(ctx, root, "fixture-owner"); err == nil {
			t.Fatal("retry admitted malformed store")
		}
		last, err := authoritybadger.RecoverInterruptedAs(ctx, authority, "fixture-owner")
		if err != nil || !reflect.DeepEqual(a, last) {
			t.Fatalf("fleet failure changed prior authority recovery: %+v %v", last, err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	retry, err := RecoverInterruptedAs(ctx, root, "fixture-owner")
	if err != nil || !reflect.DeepEqual(recovered, retry) {
		t.Fatalf("fleet retry: %+v %v", retry, err)
	}
	if _, err = RecoverInterruptedAs(ctx, root, "other-owner"); err == nil {
		t.Fatal("wrong recovery principal admitted")
	}
	s, err := Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	b, err := os.ReadFile(filepath.Join(parent, "fleet-before.json"))
	if err != nil {
		t.Fatal(err)
	}
	var entries []struct{ Key, Value []byte }
	if err = json.Unmarshal(b, &entries); err != nil {
		t.Fatal(err)
	}
	before := map[string][]byte{}
	for _, e := range entries {
		before[string(e.Key)] = e.Value
	}
	if after := recoveryInventory(t, s); !reflect.DeepEqual(before, after) || recovered.Records != uint64(len(before)) {
		t.Fatal("recovery changed logical inventory")
	}
	families := map[byte]bool{}
	for k := range before {
		if len(k) > 1 && k[0] == 2 {
			families[k[1]] = true
		}
	}
	for _, family := range []byte{familyRegistration, familySource, familyAgentRevision, familyAgentLatest, familyLoopRevision, familyLoopValidation, familyLoopProvenance, familyLoopLatest, familyLoopLifecycleEvent, familyLoopLifecycleCurrent, familyLoopAvailabilityEvent, familyLoopAvailabilityHead, familyGraphRevision, familyGraphLatest, familySnapshot, familySubmission, familyQueueItem, familyClaim, familyAttempt, familyRuntimeArtifact, familyVerificationReceipt, familyDisposition} {
		if !families[family] {
			t.Fatalf("missing representative family %x", family)
		}
	}
	r, _ := loopFixture(t)
	status, err := s.GetLoopAvailability(ctx, r.LoopID)
	if err != nil || status.State != loop.AvailabilityPublished || status.Sequence != 2 {
		t.Fatalf("availability: %+v %v", status, err)
	}
	history, err := s.ListLoopLifecycleEvents(ctx)
	if err != nil || len(history) != 1 {
		t.Fatalf("lifecycle: %+v %v", history, err)
	}
	if _, _, err = verifyFleetRecovery(ctx, s.db); err != nil {
		t.Fatal(err)
	}
	t.Logf("preserved %d records across %d fleet families; authority and fleet retry passed", len(before), len(families))
}
