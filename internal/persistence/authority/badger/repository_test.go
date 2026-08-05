package badger

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	badgerdb "github.com/dgraph-io/badger/v4"
)

func TestAuthorityRepositoryRoundTripAndCreateOnlyRecords(t *testing.T) {
	store := openAuthorityStore(t)
	ctx := context.Background()
	mandate, authority := badgerAuthorityBinding("mandate-1", "authority-1", "session-1")

	if err := store.CreateMandate(ctx, mandate); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateAuthorityContext(ctx, authority); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateMandate(ctx, mandate); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("replace mandate error = %v, want ErrAlreadyExists", err)
	}
	if err := store.CreateAuthorityContext(ctx, authority); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("replace authority context error = %v, want ErrAlreadyExists", err)
	}

	gotMandate, err := store.GetMandate(ctx, mandate.ID)
	if err != nil {
		t.Fatal(err)
	}
	gotAuthority, err := store.GetAuthorityContext(ctx, authority.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotMandate.ID != mandate.ID || gotAuthority.ID != authority.ID || gotAuthority.Digest != authority.Digest {
		t.Fatalf("repository round trip changed authority: mandate=%+v authority=%+v", gotMandate, gotAuthority)
	}
	mandates, err := store.ListMandates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	authorities, err := store.ListAuthorityContexts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(mandates) != 1 || len(authorities) != 1 {
		t.Fatalf("list lengths = mandates %d, contexts %d", len(mandates), len(authorities))
	}
}

func TestAuthorityRepositorySessionIndexAndContextCreationAreAtomic(t *testing.T) {
	store := openAuthorityStore(t)
	ctx := context.Background()
	mandate, first := badgerAuthorityBinding("mandate-1", "authority-1", "session-1")
	if err := store.CreateMandate(ctx, mandate); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateAuthorityContext(ctx, first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.ID = "authority-2"
	second.Digest = core.AuthorityContextDigest(second)
	if err := store.CreateAuthorityContext(ctx, second); err == nil {
		t.Fatal("second authority context for one runtime session was accepted")
	}
	if _, err := store.GetAuthorityContext(ctx, second.ID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected context left a partial record: %v", err)
	}
	stored, err := store.GetAuthorityContext(ctx, first.ID)
	if err != nil || stored.SessionID != first.SessionID {
		t.Fatalf("original immutable session binding changed: authority=%+v err=%v", stored, err)
	}
}

func TestConcurrentAuthorityCreationNeverCreatesAmbiguousSession(t *testing.T) {
	store := openAuthorityStore(t)
	ctx := context.Background()
	mandate, template := badgerAuthorityBinding("mandate-race", "authority-race", "session-race")
	if err := store.CreateMandate(ctx, mandate); err != nil {
		t.Fatal(err)
	}

	const candidates = 16
	start := make(chan struct{})
	results := make(chan error, candidates)
	var wait sync.WaitGroup
	for candidate := 0; candidate < candidates; candidate++ {
		wait.Add(1)
		go func(candidate int) {
			defer wait.Done()
			<-start
			authority := template
			authority.ID = fmt.Sprintf("authority-race-%d", candidate)
			authority.Digest = core.AuthorityContextDigest(authority)
			results <- store.CreateAuthorityContext(ctx, authority)
		}(candidate)
	}
	close(start)
	wait.Wait()
	close(results)

	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent session authority creations succeeded %d times, want exactly one", successes)
	}
	authorities, err := store.ListAuthorityContexts(ctx)
	if err != nil || len(authorities) != 1 || authorities[0].SessionID != template.SessionID {
		t.Fatalf("concurrent authority state is ambiguous: authorities=%+v err=%v", authorities, err)
	}
}

func TestAuthorityRepositoryTransitionFactAndRootCommitAtomically(t *testing.T) {
	store := openAuthorityStore(t)
	ctx := context.Background()
	mandate, authority := badgerAuthorityBinding("mandate-1", "authority-1", "session-1")
	if err := store.CreateMandate(ctx, mandate); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateAuthorityContext(ctx, authority); err != nil {
		t.Fatal(err)
	}

	invalid := badgerTransitionFact("fact-invalid", 2, authority, "", core.AuthorityStateActive, authority.IssuedAt, "")
	if _, err := store.AppendAuthorityTransitionFact(ctx, invalid); err == nil {
		t.Fatal("out-of-order initial transition was accepted")
	}
	facts, err := store.AuthorityTransitionFacts(ctx, authority.ID)
	if err != nil || len(facts) != 0 {
		t.Fatalf("rejected transition left facts: len=%d err=%v", len(facts), err)
	}
	root, err := store.AuthorityTransitionRoot(ctx, authority.ID)
	if err != nil || root != (core.AuthorityTransitionRoot{}) {
		t.Fatalf("rejected transition left a root: root=%+v err=%v", root, err)
	}

	active := badgerTransitionFact("fact-1", 1, authority, "", core.AuthorityStateActive, authority.IssuedAt, "")
	active.Digest = "caller-controlled"
	root, err = store.AppendAuthorityTransitionFact(ctx, active)
	if err != nil {
		t.Fatal(err)
	}
	if root.Sequence != 1 || root.State != core.AuthorityStateActive || root.LastFactDigest == "caller-controlled" {
		t.Fatalf("unexpected materialized root: %+v", root)
	}
	facts, err = store.AuthorityTransitionFacts(ctx, authority.ID)
	if err != nil || len(facts) != 1 || facts[0].Digest != root.LastFactDigest {
		t.Fatalf("fact/root commit mismatch: facts=%+v root=%+v err=%v", facts, root, err)
	}

	wrongMandate := badgerTransitionFact("fact-2", 2, authority, core.AuthorityStateActive, core.AuthorityStateRevoked, authority.IssuedAt.Add(time.Minute), root.LastFactDigest)
	wrongMandate.MandateID = "mandate-other"
	if _, err = store.AppendAuthorityTransitionFact(ctx, wrongMandate); err == nil {
		t.Fatal("cross-mandate transition was accepted")
	}
	facts, err = store.AuthorityTransitionFacts(ctx, authority.ID)
	if err != nil || len(facts) != 1 {
		t.Fatalf("denied cross-mandate transition changed history: len=%d err=%v", len(facts), err)
	}
	unchanged, err := store.AuthorityTransitionRoot(ctx, authority.ID)
	if err != nil || unchanged.Digest != root.Digest {
		t.Fatalf("denied transition changed root: root=%+v err=%v", unchanged, err)
	}
}

func TestAuthorityRepositoryFailsClosedOnKeyValueIdentityMismatch(t *testing.T) {
	store := openAuthorityStore(t)
	ctx := context.Background()
	first, _ := badgerAuthorityBinding("mandate-1", "authority-1", "session-1")
	second, _ := badgerAuthorityBinding("mandate-2", "authority-2", "session-2")
	if err := store.CreateMandate(ctx, first); err != nil {
		t.Fatal(err)
	}
	encoded, err := core.EncodeMandateCanonical(second)
	if err != nil {
		t.Fatal(err)
	}
	key, err := encodeKey(KeyMandate, []string{first.ID}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.db.Update(func(txn *badgerdb.Txn) error { return txn.Set(key, encoded) }); err != nil {
		t.Fatal(err)
	}
	if _, err = store.GetMandate(ctx, first.ID); !errors.Is(err, ErrCorruptRecord) {
		t.Fatalf("identity mismatch error = %v, want ErrCorruptRecord", err)
	}
	if _, err = store.ListMandates(ctx); !errors.Is(err, ErrCorruptRecord) {
		t.Fatalf("list identity mismatch error = %v, want ErrCorruptRecord", err)
	}
}

func TestAuthorityRepositoryFailsClosedOnMalformedRegisteredFamilyKey(t *testing.T) {
	store := openAuthorityStore(t)
	malformed := []byte{keyVersionV1, byte(KeyMandate), 0}
	if err := store.db.Update(func(txn *badgerdb.Txn) error { return txn.Set(malformed, []byte("{}")) }); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListMandates(context.Background()); !errors.Is(err, ErrCorruptRecord) {
		t.Fatalf("malformed family key error = %v, want ErrCorruptRecord", err)
	}
}

func TestAuthorityRepositoryRejectsMalformedTruncatedAndSubstitutedState(t *testing.T) {
	t.Run("malformed and truncated mandate", func(t *testing.T) {
		store := openAuthorityStore(t)
		mandate, _ := badgerAuthorityBinding("mandate-corrupt", "authority-corrupt", "session-corrupt")
		if err := store.CreateMandate(context.Background(), mandate); err != nil {
			t.Fatal(err)
		}
		key, err := encodeKey(KeyMandate, []string{mandate.ID}, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, corrupt := range [][]byte{[]byte("{"), []byte(`{"id":"mandate-corrupt"}`)} {
			if err = store.db.Update(func(txn *badgerdb.Txn) error { return txn.Set(key, corrupt) }); err != nil {
				t.Fatal(err)
			}
			if _, err = store.GetMandate(context.Background(), mandate.ID); !errors.Is(err, ErrCorruptRecord) {
				t.Fatalf("corrupt mandate error=%v, want ErrCorruptRecord", err)
			}
		}
	})

	t.Run("substituted session index", func(t *testing.T) {
		store := openAuthorityStore(t)
		mandate, authority := badgerAuthorityBinding("mandate-index", "authority-index", "session-index")
		if err := store.CreateMandate(context.Background(), mandate); err != nil {
			t.Fatal(err)
		}
		if err := store.CreateAuthorityContext(context.Background(), authority); err != nil {
			t.Fatal(err)
		}
		indexKey, err := encodeKey(KeyContextBySession, []string{authority.SessionID}, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err = store.db.Update(func(txn *badgerdb.Txn) error { return txn.Set(indexKey, []byte("authority-substituted")) }); err != nil {
			t.Fatal(err)
		}
		if _, err = store.GetAuthorityContext(context.Background(), authority.ID); !errors.Is(err, ErrCorruptRecord) {
			t.Fatalf("substituted index error=%v, want ErrCorruptRecord", err)
		}
	})

	t.Run("transition root without fact", func(t *testing.T) {
		store := openAuthorityStore(t)
		mandate, authority := badgerAuthorityBinding("mandate-root", "authority-root", "session-root")
		if err := store.CreateMandate(context.Background(), mandate); err != nil {
			t.Fatal(err)
		}
		if err := store.CreateAuthorityContext(context.Background(), authority); err != nil {
			t.Fatal(err)
		}
		fact := badgerTransitionFact("fact-root", 1, authority, "", core.AuthorityStateActive, authority.IssuedAt, "")
		if _, err := store.AppendAuthorityTransitionFact(context.Background(), fact); err != nil {
			t.Fatal(err)
		}
		factKey, err := encodeKey(KeyTransitionFact, []string{authority.ID}, 1)
		if err != nil {
			t.Fatal(err)
		}
		if err = store.db.Update(func(txn *badgerdb.Txn) error { return txn.Delete(factKey) }); err != nil {
			t.Fatal(err)
		}
		if _, err = store.AuthorityTransitionRoot(context.Background(), authority.ID); !errors.Is(err, ErrCorruptRecord) {
			t.Fatalf("orphan transition root error=%v, want ErrCorruptRecord", err)
		}
	})
}

func TestAuthorityRepositoryHonorsCanceledContextAndClosedStore(t *testing.T) {
	store := openAuthorityStore(t)
	mandate, _ := badgerAuthorityBinding("mandate-1", "authority-1", "session-1")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.CreateMandate(ctx, mandate); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled create error = %v, want context.Canceled", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateMandate(context.Background(), mandate); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed create error = %v, want ErrClosed", err)
	}
}

func openAuthorityStore(t *testing.T) *Store {
	t.Helper()
	root := authorityRoot(t)
	if _, err := Initialize(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	store, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close authority store: %v", err)
		}
	})
	return store
}

func badgerAuthorityBinding(mandateID, authorityID, sessionID string) (core.Mandate, core.AuthorityContext) {
	issuedAt := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	expiresAt := issuedAt.Add(time.Hour)
	runtime := core.RuntimeDescriptor{Name: "Hermes Agent", Runtime: "hermes-agent", Version: "0.18.2"}
	mandate := core.Mandate{
		ID:      mandateID,
		Subject: core.Subject{ID: "subject-1", Kind: "human", PrincipalID: "principal", Issuer: "local-os", Method: "local-os", AuthenticatedAt: issuedAt, ExpiresAt: expiresAt},
		AgentID: "agent-1", StanzaID: "principal", CharterRevision: 1, CharterDigest: "sha256:charter", Runtime: runtime,
		Capabilities: []string{"chat"}, Tools: []string{"no_mcp"}, Scopes: core.Scopes{Memory: []string{"private"}, Credentials: []string{"provider:test"}},
		Hermes: core.HermesConfig{Toolsets: []string{"no_mcp"}}, IssuedAt: issuedAt, ExpiresAt: expiresAt,
	}
	authority := core.AuthorityContext{
		ID: authorityID, MandateID: mandate.ID, SessionID: sessionID, SubjectID: mandate.Subject.ID, AgentID: mandate.AgentID,
		CharterRevision: mandate.CharterRevision, CharterDigest: mandate.CharterDigest, Runtime: runtime,
		Authority: core.EffectiveAuthority{StanzaID: mandate.StanzaID, Capabilities: []string{"chat"}, Tools: []string{"no_mcp"}, Memory: []string{"private"}, Credentials: []string{"provider:test"}, Hermes: mandate.Hermes},
		IssuedAt:  issuedAt, ExpiresAt: expiresAt,
	}
	authority.Digest = core.AuthorityContextDigest(authority)
	return mandate, authority
}

func badgerTransitionFact(id string, sequence uint64, authority core.AuthorityContext, from, to core.AuthorityState, occurredAt time.Time, previousDigest string) core.AuthorityTransitionFact {
	fact := core.AuthorityTransitionFact{
		ID: id, Sequence: sequence, MandateID: authority.MandateID, AuthorityContextID: authority.ID,
		From: from, To: to, OccurredAt: occurredAt, RecordedBy: "principal", Reason: "operator decision", PreviousDigest: previousDigest,
	}
	fact.Digest = core.AuthorityTransitionFactDigest(fact)
	return fact
}
