package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
)

func TestAuthorityRepositoryTypedRoundTripAndCreateOnlyHistory(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	mandate, authority := authorityRepositoryBinding("mandate-1", "authority-1", "session-1")

	if err = s.CreateMandate(ctx, mandate); err != nil {
		t.Fatal(err)
	}
	if err = s.CreateAuthorityContext(ctx, authority); err != nil {
		t.Fatal(err)
	}
	if err = s.CreateMandate(ctx, mandate); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("mandate replacement error = %v, want ErrAlreadyExists", err)
	}
	if err = s.CreateAuthorityContext(ctx, authority); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("authority context replacement error = %v, want ErrAlreadyExists", err)
	}

	gotMandate, err := s.GetMandate(ctx, mandate.ID)
	if err != nil {
		t.Fatal(err)
	}
	gotAuthority, err := s.GetAuthorityContext(ctx, authority.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotMandate.ID != mandate.ID || gotAuthority.Digest != authority.Digest || gotAuthority.SessionID != authority.SessionID {
		t.Fatal("typed authority records changed across repository round trip")
	}
	mandates, err := s.ListMandates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	authorities, err := s.ListAuthorityContexts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(mandates) != 1 || len(authorities) != 1 {
		t.Fatalf("typed lists returned %d mandates and %d contexts, want one each", len(mandates), len(authorities))
	}
}

func TestAuthorityRepositoryAllowsExactlyOneImmutableContextPerSession(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	mandate, first := authorityRepositoryBinding("mandate-1", "authority-1", "session-1")
	if err = s.CreateMandate(ctx, mandate); err != nil {
		t.Fatal(err)
	}
	if err = s.CreateAuthorityContext(ctx, first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.ID = "authority-2"
	second.Digest = core.AuthorityContextDigest(second)
	if err = s.CreateAuthorityContext(ctx, second); err == nil {
		t.Fatal("a second authority context was accepted for the same runtime session")
	}
	stored, err := s.ListAuthorityContexts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].ID != first.ID {
		t.Fatal("rejected session authority replacement changed immutable history")
	}
}

func TestAuthorityRepositoryAppendsAndReplaysTransitionFacts(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	mandate, authority := authorityRepositoryBinding("mandate-1", "authority-1", "session-1")
	if err = s.CreateMandate(ctx, mandate); err != nil {
		t.Fatal(err)
	}
	if err = s.CreateAuthorityContext(ctx, authority); err != nil {
		t.Fatal(err)
	}

	active := repositoryTransitionFact("fact-1", 1, authority, "", core.AuthorityStateActive, authority.IssuedAt, "")
	active.Digest = "caller-controlled-value"
	root, err := s.AppendAuthorityTransitionFact(ctx, active)
	if err != nil {
		t.Fatal(err)
	}
	if root.State != core.AuthorityStateActive || root.Sequence != 1 || root.LastFactDigest == "caller-controlled-value" {
		t.Fatalf("unexpected activation root: %+v", root)
	}
	revoked := repositoryTransitionFact("fact-2", 2, authority, core.AuthorityStateActive, core.AuthorityStateRevoked, authority.IssuedAt.Add(time.Minute), root.LastFactDigest)
	root, err = s.AppendAuthorityTransitionFact(ctx, revoked)
	if err != nil {
		t.Fatal(err)
	}
	if root.State != core.AuthorityStateRevoked || root.Sequence != 2 || root.Digest != core.AuthorityTransitionRootDigest(root) {
		t.Fatalf("unexpected revocation root: %+v", root)
	}

	facts, err := s.AuthorityTransitionFacts(ctx, authority.ID)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := core.ReplayAuthorityTransitions(facts)
	if err != nil {
		t.Fatal(err)
	}
	loadedRoot, err := s.AuthorityTransitionRoot(ctx, authority.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 2 || replayed.Digest != root.Digest || loadedRoot.Digest != root.Digest {
		t.Fatal("repository replay did not reconstruct the authoritative transition root")
	}

	third := repositoryTransitionFact("fact-3", 3, authority, core.AuthorityStateRevoked, core.AuthorityStateActive, authority.IssuedAt.Add(2*time.Minute), root.LastFactDigest)
	if _, err = s.AppendAuthorityTransitionFact(ctx, third); err == nil {
		t.Fatal("terminal authority history was reactivated")
	}
	facts, err = s.AuthorityTransitionFacts(ctx, authority.ID)
	if err != nil || len(facts) != 2 {
		t.Fatalf("denied transition changed append-only history: len=%d err=%v", len(facts), err)
	}
}

func TestAuthorityRepositoryDeniesCrossMandateAndBrokenStoredFacts(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	mandate, authority := authorityRepositoryBinding("mandate-1", "authority-1", "session-1")
	if err = s.CreateMandate(ctx, mandate); err != nil {
		t.Fatal(err)
	}
	if err = s.CreateAuthorityContext(ctx, authority); err != nil {
		t.Fatal(err)
	}

	wrongMandate := repositoryTransitionFact("fact-cross", 1, authority, "", core.AuthorityStateActive, authority.IssuedAt, "")
	wrongMandate.MandateID = "mandate-2"
	if _, err = s.AppendAuthorityTransitionFact(ctx, wrongMandate); err == nil {
		t.Fatal("cross-mandate authority transition was accepted")
	}
	active := repositoryTransitionFact("fact-1", 1, authority, "", core.AuthorityStateActive, authority.IssuedAt, "")
	if _, err = s.AppendAuthorityTransitionFact(ctx, active); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(s.Root(), authorityTransitionsKind, transitionStorageID(authority.ID, 1)+".json")
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for index := range encoded {
		if encoded[index] == 'p' {
			encoded[index] = 'q'
			break
		}
	}
	if err = os.WriteFile(path, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AuthorityTransitionRoot(ctx, authority.ID); err == nil {
		t.Fatal("tampered retained authority fact produced an authoritative root")
	}
}

func TestAuthorityRepositoryRejectsTransitionStoredUnderWrongSequenceKey(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	mandate, authority := authorityRepositoryBinding("mandate-1", "authority-1", "session-1")
	if err = s.CreateMandate(ctx, mandate); err != nil {
		t.Fatal(err)
	}
	if err = s.CreateAuthorityContext(ctx, authority); err != nil {
		t.Fatal(err)
	}
	active := repositoryTransitionFact("fact-1", 1, authority, "", core.AuthorityStateActive, authority.IssuedAt, "")
	if _, err = s.AppendAuthorityTransitionFact(ctx, active); err != nil {
		t.Fatal(err)
	}

	directory := filepath.Join(s.Root(), authorityTransitionsKind)
	correct := filepath.Join(directory, transitionStorageID(authority.ID, 1)+".json")
	wrong := filepath.Join(directory, transitionStorageID(authority.ID, 2)+".json")
	if err = os.Rename(correct, wrong); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AuthorityTransitionRoot(ctx, authority.ID); err == nil {
		t.Fatal("authority fact stored under a mismatched sequence key was accepted")
	}
}

func TestAuthorityRepositorySeparatesContextIDsWithSharedPrefixes(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	firstMandate, first := authorityRepositoryBinding("mandate-1", "authority", "session-1")
	secondMandate, second := authorityRepositoryBinding("mandate-2", "authority-child", "session-2")
	for _, mandate := range []core.Mandate{firstMandate, secondMandate} {
		if err = s.CreateMandate(ctx, mandate); err != nil {
			t.Fatal(err)
		}
	}
	for _, authority := range []core.AuthorityContext{first, second} {
		if err = s.CreateAuthorityContext(ctx, authority); err != nil {
			t.Fatal(err)
		}
		active := repositoryTransitionFact("fact-"+authority.ID, 1, authority, "", core.AuthorityStateActive, authority.IssuedAt, "")
		if _, err = s.AppendAuthorityTransitionFact(ctx, active); err != nil {
			t.Fatal(err)
		}
	}

	for _, authority := range []core.AuthorityContext{first, second} {
		facts, readErr := s.AuthorityTransitionFacts(ctx, authority.ID)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if len(facts) != 1 || facts[0].AuthorityContextID != authority.ID {
			t.Fatalf("facts for %q crossed a context boundary: %+v", authority.ID, facts)
		}
	}
}

func TestAuthorityRepositorySerializesConcurrentTransitionAppends(t *testing.T) {
	root := t.TempDir()
	const contenders = 8
	stores := make([]*Store, contenders)
	for index := range stores {
		var err error
		stores[index], err = Open(root)
		if err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	mandate, authority := authorityRepositoryBinding("mandate-1", "authority-1", "session-1")
	if err := stores[0].CreateMandate(ctx, mandate); err != nil {
		t.Fatal(err)
	}
	if err := stores[0].CreateAuthorityContext(ctx, authority); err != nil {
		t.Fatal(err)
	}
	active := repositoryTransitionFact("fact-1", 1, authority, "", core.AuthorityStateActive, authority.IssuedAt, "")
	activeRoot, err := stores[0].AppendAuthorityTransitionFact(ctx, active)
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	results := make(chan error, contenders)
	var wg sync.WaitGroup
	for index, candidateStore := range stores {
		wg.Add(1)
		go func(index int, candidateStore *Store) {
			defer wg.Done()
			<-start
			fact := repositoryTransitionFact("terminal-"+string(rune('a'+index)), 2, authority, core.AuthorityStateActive, core.AuthorityStateRevoked, authority.IssuedAt.Add(time.Minute), activeRoot.LastFactDigest)
			_, appendErr := candidateStore.AppendAuthorityTransitionFact(ctx, fact)
			results <- appendErr
		}(index, candidateStore)
	}
	close(start)
	wg.Wait()
	close(results)

	successes := 0
	for appendErr := range results {
		if appendErr == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful concurrent terminal transitions = %d, want 1", successes)
	}
	facts, err := stores[0].AuthorityTransitionFacts(ctx, authority.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 2 {
		t.Fatalf("retained transition facts = %d, want 2", len(facts))
	}
}

func TestAuthorityRepositoryHonorsCanceledContext(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	mandate, _ := authorityRepositoryBinding("mandate-1", "authority-1", "session-1")
	if err = s.CreateMandate(ctx, mandate); !errors.Is(err, context.Canceled) {
		t.Fatalf("CreateMandate error = %v, want context.Canceled", err)
	}
}

func authorityRepositoryBinding(mandateID, authorityID, sessionID string) (core.Mandate, core.AuthorityContext) {
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

func repositoryTransitionFact(id string, sequence uint64, authority core.AuthorityContext, from, to core.AuthorityState, occurredAt time.Time, previousDigest string) core.AuthorityTransitionFact {
	fact := core.AuthorityTransitionFact{
		ID: id, Sequence: sequence, MandateID: authority.MandateID, AuthorityContextID: authority.ID,
		From: from, To: to, OccurredAt: occurredAt, RecordedBy: "principal", Reason: "operator decision", PreviousDigest: previousDigest,
	}
	fact.Digest = core.AuthorityTransitionFactDigest(fact)
	return fact
}
