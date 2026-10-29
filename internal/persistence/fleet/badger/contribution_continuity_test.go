package badger

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	badgerdb "github.com/dgraph-io/badger/v4"
)

func TestContributionContinuityFamilyDoesNotAliasOtherFacts(t *testing.T) {
	for _, family := range []byte{familyContribution, familyContributionRequest, familyContributionIssue, familyContributionControl, familyQueueArchive, familyQueueArchiveHead, familyQueuePresentationRequest, familyQueuePreferences, familyQueuePreferencesHead, familyQueueRuntimeBinding, familyDisposition} {
		if family == familyContributionContinuity {
			t.Fatalf("continuity aliases existing family %x", family)
		}
	}
}

// Engine-level fault fixture; application tests independently admit through the
// real owning service. No synthetic stop/authority/approval receipt is seeded.
func continuityStore(t *testing.T) (*Store, contribution.Contract) {
	t.Helper()
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "fleet-v1"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	now := time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC)
	ref := func(id string) reference.RevisionRef {
		return reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: id, Revision: 1, Digest: contribution.Digest([]byte(id))}
	}
	roles := []contribution.RoleBinding{}
	for _, id := range []string{"controller", "builder", "tester"} {
		roles = append(roles, contribution.RoleBinding{Agent: ref(id), Charter: ref("charter-" + id), Runtime: registry.RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent", Target: "profile/" + id}})
	}
	url, _ := contribution.ParseIssueURL("https://github.com/owner/repo/issues/1")
	c, err := contribution.Seal(contribution.Contract{SchemaVersion: contribution.Schema, OwnerID: "owner", SessionID: "session", Key: "intent", PayloadDigest: contribution.Digest([]byte("payload")), QueueItemID: "queue", SubmissionID: "submission", RunID: "run", Loop: ref("loop"), Controller: roles[0].Agent, Builder: roles[1].Agent, Tester: roles[2].Agent, Source: contribution.Source{URL: url, RepositoryID: 1, IssueID: 1, ContentDigest: contribution.Digest([]byte("issue")), Author: contribution.Author{ID: 1, Login: "owner"}, State: "open", CreatedAt: now, ObservedAt: now, UpdatedAt: now, Discussion: []contribution.Comment{}}, Outcome: "repair", Acceptance: "real behavior", Invariants: []string{}, Exclusions: []string{}, Repository: "owner/repo", Base: "base", Worktree: "/isolated/worktree", Effect: "local-only", Roles: roles, TemplateDigest: contribution.Digest([]byte("template")), CandidateBudget: 2, AdmittedAt: now, Deadline: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	b, err := contribution.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.update(ctx, func(txn *badgerdb.Txn) error { return create(txn, key(familyContribution, c.QueueItemID), b) }); err != nil {
		t.Fatal(err)
	}
	return s, c
}
func continuityEvent(t *testing.T, c contribution.Contract, prior contribution.Continuity, kind string) contribution.ContinuityEvent {
	t.Helper()
	e := contribution.ContinuityEvent{SchemaVersion: contribution.ContinuitySchema, QueueItemID: c.QueueItemID, ContractDigest: c.Digest, OwnerID: c.OwnerID, SessionID: c.SessionID, Sequence: prior.Sequence + 1, PreviousDigest: prior.Digest, Kind: kind, At: c.AdmittedAt.Add(time.Second)}
	if kind == "initialize" {
		e.Limits = contribution.Counters{Candidates: 2, QueueAttempts: 1, RuntimeTurns: 2, InfrastructureRechecks: 2, DecisionDeliveries: 2}
	}
	if kind == "reserve" {
		e.Counter = "runtime-turn"
	}
	e, err := contribution.SealContinuityEvent(e)
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func TestContributionContinuityConcurrentCAS(t *testing.T) {
	s, c := continuityStore(t)
	ctx := context.Background()
	state, err := s.AppendContributionContinuity(ctx, continuityEvent(t, c, contribution.Continuity{}, "initialize"))
	if err != nil {
		t.Fatal(err)
	}
	event := continuityEvent(t, c, state, "reserve")
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.AppendContributionContinuity(ctx, event); results <- err }()
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	got, events, err := s.GetContributionContinuity(ctx, c.QueueItemID)
	if err != nil || successes != 1 || got.Used.RuntimeTurns != 1 || len(events) != 2 {
		t.Fatalf("double spend: %+v success=%d err=%v", got, successes, err)
	}
}
func TestContributionContinuityHistoryCorruption(t *testing.T) {
	for _, fault := range []string{"missing-head", "rollback-head", "missing-event", "tampered-event", "tampered-prefix", "unknown-key"} {
		t.Run(fault, func(t *testing.T) {
			s, c := continuityStore(t)
			ctx := context.Background()
			first := continuityEvent(t, c, contribution.Continuity{}, "initialize")
			state, err := s.AppendContributionContinuity(ctx, first)
			if err != nil {
				t.Fatal(err)
			}
			second := continuityEvent(t, c, state, "reserve")
			if _, err = s.AppendContributionContinuity(ctx, second); err != nil {
				t.Fatal(err)
			}
			// Warm the complete validated prefix before each disk fault. A cache
			// must not hide corruption in any old event, not merely the head.
			if _, _, err = s.GetContributionContinuity(ctx, c.QueueItemID); err != nil {
				t.Fatal(err)
			}
			err = s.update(ctx, func(txn *badgerdb.Txn) error {
				switch fault {
				case "missing-head":
					return txn.Delete(key(familyContributionContinuity, c.QueueItemID, "head"))
				case "rollback-head":
					b, _ := json.Marshal(first)
					return txn.Set(key(familyContributionContinuity, c.QueueItemID, "head"), b)
				case "missing-event":
					return txn.Delete(key(familyContributionContinuity, c.QueueItemID, "1"))
				case "tampered-prefix":
					first.OwnerID = "substituted"
					b, _ := json.Marshal(first)
					return txn.Set(key(familyContributionContinuity, c.QueueItemID, "1"), b)
				case "tampered-event":
					second.Counter = "candidate"
					b, _ := json.Marshal(second)
					return txn.Set(key(familyContributionContinuity, c.QueueItemID, "2"), b)
				default:
					return txn.Set(key(familyContributionContinuity, c.QueueItemID, "extra"), []byte("{}"))
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = s.GetContributionContinuity(ctx, c.QueueItemID); !errors.Is(err, fleet.ErrCorrupt) {
				t.Fatalf("%s accepted %v", fault, err)
			}
		})
	}
}
func TestContributionContinuityRejectsUnsupportedAuthorityAndBindings(t *testing.T) {
	s, c := continuityStore(t)
	ctx := context.Background()
	state, err := s.AppendContributionContinuity(ctx, continuityEvent(t, c, contribution.Continuity{}, "initialize"))
	if err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(*contribution.ContinuityEvent){
		"wrong-owner": func(e *contribution.ContinuityEvent) { e.OwnerID = "another" },
		"wrong-contract": func(e *contribution.ContinuityEvent) {
			e.ContractDigest = contribution.Digest([]byte("substituted-scope"))
		},
		"new-session":           func(e *contribution.ContinuityEvent) { e.SessionID = "fresh-login-not-admission" },
		"stop-status-not-proof": func(e *contribution.ContinuityEvent) { e.Kind = "stop-acknowledged"; e.Counter = "" },
		"renewal":               func(e *contribution.ContinuityEvent) { e.Kind = "initialize"; e.Counter = ""; e.Limits = state.Limits },
		"mixed-payload":         func(e *contribution.ContinuityEvent) { e.DecisionOutcome = "approved" },
		"clock-rollback":        func(e *contribution.ContinuityEvent) { e.At = c.AdmittedAt },
		"deadline":              func(e *contribution.ContinuityEvent) { e.At = c.Deadline },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			e := continuityEvent(t, c, state, "reserve")
			mutate(&e)
			e, err = contribution.SealContinuityEvent(e)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.AppendContributionContinuity(ctx, e); err == nil {
				t.Fatal("unsupported admission accepted")
			}
			got, _, err := s.GetContributionContinuity(ctx, c.QueueItemID)
			if err != nil || got.Digest != state.Digest || got.Used != state.Used {
				t.Fatal("denial mutated history")
			}
		})
	}
}

func TestContributionContinuityAuditFailureRollsBackFence(t *testing.T) {
	s, c := continuityStore(t)
	ctx := context.Background()
	// A corrupt existing audit tail makes authoritative append fail after the
	// cancellation control has been staged in the same Badger transaction.
	if err := s.update(ctx, func(txn *badgerdb.Txn) error { return txn.Set(key(familyAudit, revisionPart(1)), []byte("bad-audit")) }); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendContributionContinuity(ctx, continuityEvent(t, c, contribution.Continuity{}, "cancel-requested")); err == nil {
		t.Fatal("audit failure ignored")
	}
	state, events, err := s.GetContributionContinuity(ctx, c.QueueItemID)
	if err != nil || state.Sequence != 0 || len(events) != 0 {
		t.Fatalf("partial continuity write %+v %v", state, err)
	}
	control, err := s.GetContributionControl(ctx, c.QueueItemID)
	if err != nil || control.Stop || control.Sequence != 0 {
		t.Fatalf("partial control fence %+v %v", control, err)
	}
}
