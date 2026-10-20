package badger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	badgerdb "github.com/dgraph-io/badger/v4"
)

func availabilityFixture(t *testing.T) (*Store, loop.PublishRequest) {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), schemaVersion))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if !s.closed {
			_ = s.Close()
		}
	})
	registration, agent := agentFixture(t)
	if _, err = s.RegisterAgent(context.Background(), registration, agent, audit("agent.registered", agent.AgentID)); err != nil {
		t.Fatal(err)
	}
	r, v := loopFixture(t)
	p := loop.PublishRequest{Revision: r, Validation: v, Provenance: loopPublicationProvenance(t, r, v), IdempotencyKey: "availability-publication"}
	if _, err = s.PublishLoop(context.Background(), p, audit("loop.published", r.LoopID)); err != nil {
		t.Fatal(err)
	}
	return s, p
}

func availabilityEvent(t *testing.T, p loop.PublishRequest, previous *loop.AvailabilityEvent, id string) loop.AvailabilityEvent {
	t.Helper()
	v := loop.AvailabilityEvent{EventID: id, LoopID: p.Revision.LoopID, Sequence: 1, Head: p.Provenance.Loop, ExpectedState: loop.AvailabilityPublished, State: loop.AvailabilityArchived, PublisherAgent: p.Provenance.PublisherAgent, OwnerID: "operator-primary", PrincipalID: "operator-primary", Authority: p.Provenance.Authority, OccurredAt: time.Unix(100, 0)}
	if previous != nil {
		v.Sequence = previous.Sequence + 1
		v.PreviousDigest = previous.Digest
		v.ExpectedState = previous.State
		if previous.State == loop.AvailabilityArchived {
			v.State = loop.AvailabilityPublished
		}
	}
	e, err := loop.NewAvailabilityEvent(v)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func availabilityAudit(e loop.AvailabilityEvent) fleet.AuditFact {
	name := "loop.archived"
	if e.State == loop.AvailabilityPublished {
		name = "loop.unarchived"
	}
	return audit(name, e.LoopID)
}

func appendAvailability(t *testing.T, s *Store, e loop.AvailabilityEvent) {
	t.Helper()
	out, replay, err := s.AppendLoopAvailability(context.Background(), e, availabilityAudit(e))
	if err != nil || replay || out != e {
		t.Fatalf("append: out=%+v replay=%v err=%v", out, replay, err)
	}
}

func TestLoopAvailabilityDurableLogicalHistory(t *testing.T) {
	ctx := context.Background()
	s, p := availabilityFixture(t)
	status, err := s.GetLoopAvailability(ctx, p.Revision.LoopID)
	if err != nil || status.State != loop.AvailabilityPublished || status.Sequence != 0 || status.Digest != "" {
		t.Fatalf("legacy: %+v %v", status, err)
	}
	if _, err = s.GetLoopAvailability(ctx, "unknown"); !errors.Is(err, fleet.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
	e := availabilityEvent(t, p, nil, "archive")
	appendAvailability(t, s, e)
	// A successor publication does not reset logical availability.
	next := p.Revision
	next.Revision, next.PreviousDigest = 2, p.Revision.Digest
	next, validation, err := loop.NewRevision(next)
	if err != nil {
		t.Fatal(err)
	}
	p2 := loop.PublishRequest{Revision: next, Validation: validation, Provenance: loopPublicationProvenance(t, next, validation), ExpectedPreviousDigest: p.Revision.Digest, IdempotencyKey: "successor"}
	if _, err = s.PublishLoop(ctx, p2, audit("loop.published", next.LoopID)); err != nil {
		t.Fatal(err)
	}
	status, err = s.GetLoopAvailability(ctx, next.LoopID)
	if err != nil || status.State != loop.AvailabilityArchived || status.Digest != e.Digest {
		t.Fatalf("successor escaped archive: %+v %v", status, err)
	}
	// Old publication head cannot authorize a new state transition.
	stale := availabilityEvent(t, p, &e, "stale-unarchive")
	if _, _, err = s.AppendLoopAvailability(ctx, stale, availabilityAudit(stale)); !errors.Is(err, fleet.ErrConflict) {
		t.Fatalf("stale head: %v", err)
	}
	restore := availabilityEvent(t, p2, &e, "restore")
	appendAvailability(t, s, restore)
	root := s.root
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	history, err := reopened.ListLoopAvailabilityEvents(ctx, next.LoopID)
	if err != nil || len(history) != 2 || history[0] != e || history[1] != restore {
		t.Fatalf("durable history: %+v %v", history, err)
	}
	status, err = reopened.GetLoopAvailability(ctx, next.LoopID)
	if err != nil || status.State != loop.AvailabilityPublished || status.Digest != restore.Digest {
		t.Fatalf("restored: %+v %v", status, err)
	}
	old, err := reopened.GetLoopRevision(ctx, p.Revision.LoopID, 1)
	if err != nil || old.Digest != p.Revision.Digest {
		t.Fatalf("immutable publication changed: %+v %v", old, err)
	}
	lifecycle, err := reopened.ListLoopLifecycleEvents(ctx)
	if err != nil || len(lifecycle) != 0 {
		t.Fatalf("availability altered lifecycle: %+v %v", lifecycle, err)
	}
	events, err := reopened.AuditEvents(ctx)
	if err != nil || len(events) != 5 || events[2].Metadata["availability_digest"] != e.Digest || events[4].Metadata["availability_digest"] != restore.Digest {
		t.Fatalf("durable audit: %+v %v", events, err)
	}
	// Reconcile the original intent even after publication/state have advanced.
	retry := e
	retry.OccurredAt = time.Unix(200, 0)
	retry, err = loop.NewAvailabilityEvent(retry)
	if err != nil {
		t.Fatal(err)
	}
	out, replay, err := reopened.AppendLoopAvailability(ctx, retry, availabilityAudit(retry))
	if err != nil || !replay || out != e {
		t.Fatalf("durable replay: %+v %v %v", out, replay, err)
	}
}

func TestLoopAvailabilityRejectsConflictsWithoutMutation(t *testing.T) {
	s, p := availabilityFixture(t)
	e := availabilityEvent(t, p, nil, "archive")
	appendAvailability(t, s, e)
	cases := map[string]loop.AvailabilityEvent{}
	v := e
	v.OwnerID = "other-owner"
	cases["owner"] = v
	v = e
	v.PublisherAgent.ID = "other-agent"
	cases["publisher"] = v
	v = e
	v.Authority.Digest = "sha256:" + strings.Repeat("c", 64)
	cases["intent"] = v
	v = availabilityEvent(t, p, &e, "wrong-state")
	v.ExpectedState, v.State = loop.AvailabilityPublished, loop.AvailabilityArchived
	cases["expected-state"] = v
	v = availabilityEvent(t, p, &e, "wrong-cas")
	v.PreviousDigest = "sha256:" + strings.Repeat("c", 64)
	cases["cas"] = v
	v = availabilityEvent(t, p, &e, "wrong-publication")
	v.Head.Digest = "sha256:" + strings.Repeat("c", 64)
	cases["publication"] = v
	for name, candidate := range cases {
		t.Run(name, func(t *testing.T) {
			candidate, err := loop.NewAvailabilityEvent(candidate)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = s.AppendLoopAvailability(context.Background(), candidate, availabilityAudit(candidate)); !errors.Is(err, fleet.ErrConflict) {
				t.Fatalf("expected conflict: %v", err)
			}
		})
	}
	changedAudit := availabilityAudit(e)
	changedAudit.Event.Reason = "changed intent"
	if _, _, err := s.AppendLoopAvailability(context.Background(), e, changedAudit); !errors.Is(err, fleet.ErrConflict) {
		t.Fatalf("audit intent: %v", err)
	}
	// Invalid authoritative audit rolls back both event and head.
	restore := availabilityEvent(t, p, &e, "restore")
	badAudit := availabilityAudit(restore)
	badAudit.Event.Reason = ""
	if _, _, err := s.AppendLoopAvailability(context.Background(), restore, badAudit); err == nil {
		t.Fatal("missing audit accepted")
	}
	history, err := s.ListLoopAvailabilityEvents(context.Background(), e.LoopID)
	if err != nil || len(history) != 1 {
		t.Fatalf("denial mutated history: %+v %v", history, err)
	}
	audits, err := s.AuditEvents(context.Background())
	if err != nil || len(audits) != 3 {
		t.Fatalf("denial mutated audit: %+v %v", audits, err)
	}
	appendAvailability(t, s, restore)
	// ABA: Published again does not let an old Published expectation through.
	aba := availabilityEvent(t, p, nil, "aba")
	if _, _, err = s.AppendLoopAvailability(context.Background(), aba, availabilityAudit(aba)); !errors.Is(err, fleet.ErrConflict) {
		t.Fatalf("ABA: %v", err)
	}
}

func TestLoopAvailabilityConcurrentCASAndReplay(t *testing.T) {
	for _, sameID := range []bool{true, false} {
		t.Run(fmt.Sprint(sameID), func(t *testing.T) {
			s, p := availabilityFixture(t)
			base := availabilityEvent(t, p, nil, "archive")
			var wg sync.WaitGroup
			var mu sync.Mutex
			created, replays, conflicts := 0, 0, 0
			for i := 0; i < 8; i++ {
				e := base
				if !sameID {
					e.EventID = fmt.Sprintf("archive-%d", i)
				}
				e, err := loop.NewAvailabilityEvent(e)
				if err != nil {
					t.Fatal(err)
				}
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, replay, err := s.AppendLoopAvailability(context.Background(), e, availabilityAudit(e))
					mu.Lock()
					defer mu.Unlock()
					if errors.Is(err, fleet.ErrConflict) {
						conflicts++
					} else if err != nil {
						t.Errorf("append: %v", err)
					} else if replay {
						replays++
					} else {
						created++
					}
				}()
			}
			wg.Wait()
			if created != 1 || (sameID && replays != 7) || (!sameID && conflicts != 7) {
				t.Fatalf("created=%d replay=%d conflicts=%d", created, replays, conflicts)
			}
			audits, err := s.AuditEvents(context.Background())
			if err != nil || len(audits) != 3 {
				t.Fatalf("audits=%d err=%v", len(audits), err)
			}
		})
	}
}

func TestLoopAvailabilityCorruptRecordsFailClosed(t *testing.T) {
	for _, mode := range []string{"json", "head", "missing-head", "missing-event", "sequence", "publication", "owner", "audit-binding", "key"} {
		t.Run(mode, func(t *testing.T) {
			s, p := availabilityFixture(t)
			e := availabilityEvent(t, p, nil, "archive")
			appendAvailability(t, s, e)
			switch mode {
			case "json":
				setRawRecord(t, s, key(familyLoopAvailabilityEvent, e.EventID), []byte(`{}`))
			case "head":
				setRawRecord(t, s, key(familyLoopAvailabilityHead, e.LoopID), []byte("wrong"))
			case "missing-head":
				if err := s.db.Update(func(txn *badgerdb.Txn) error { return txn.Delete(key(familyLoopAvailabilityHead, e.LoopID)) }); err != nil {
					t.Fatal(err)
				}
			case "missing-event":
				if err := s.db.Update(func(txn *badgerdb.Txn) error { return txn.Delete(key(familyLoopAvailabilityEvent, e.EventID)) }); err != nil {
					t.Fatal(err)
				}
			case "publication":
				setRawRecord(t, s, key(familyLoopProvenance, e.LoopID, revisionPart(1)), []byte(`{}`))
			default:
				r := availabilityRecord{Event: e, Audit: availabilityAudit(e).Event}
				if mode == "sequence" {
					r.Event.Sequence = 2
					r.Event.PreviousDigest = "sha256:" + strings.Repeat("c", 64)
				}
				if mode == "owner" {
					r.Event.OwnerID = "other-owner"
				}
				r.Event, _ = loop.NewAvailabilityEvent(r.Event)
				r.Digest = core.Digest(r)
				if mode == "audit-binding" {
					r.Audit.EventDigest = "tampered"
				}
				wire, err := json.Marshal(r)
				if err != nil {
					t.Fatal(err)
				}
				recordID := e.EventID
				if mode == "key" {
					recordID = "wrong-key"
				}
				setRawRecord(t, s, key(familyLoopAvailabilityEvent, recordID), wire)
			}
			if _, err := s.GetLoopAvailability(context.Background(), e.LoopID); !errors.Is(err, fleet.ErrCorrupt) {
				t.Fatalf("read must fail closed: %v", err)
			}
			if _, _, err := s.AppendLoopAvailability(context.Background(), e, availabilityAudit(e)); !errors.Is(err, fleet.ErrCorrupt) {
				t.Fatalf("replay must fail closed: %v", err)
			}
			audits, err := s.AuditEvents(context.Background())
			if err != nil || len(audits) != 3 {
				t.Fatalf("corruption denial changed audits: %d %v", len(audits), err)
			}
		})
	}
}
