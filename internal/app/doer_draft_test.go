package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	"github.com/berryhill/aegis/internal/store"
)

func draftFixture(t *testing.T) (*Service, core.Subject, DoerDraftInput) {
	t.Helper()
	s := testService(t)
	subject, err := s.Authenticate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	input := DoerDraftInput{Agent: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "agent", Revision: 1, Digest: "sha256:" + strings.Repeat("a", 64)}, LoopID: "draft-loop", Revision: 1, Contract: loop.DoerContract{Task: "Write a result", Workspace: "/workspace/project", WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", MaxAttempts: 2}}
	return s, subject, input
}

func TestDoerDraftPersistenceAndStableIdentity(t *testing.T) {
	s, subject, input := draftFixture(t)
	empty := ""
	input.Contract.ExpectedText = &empty
	original, err := s.SaveDoerDraftAs(context.Background(), subject, input)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(s.Store.Root())
	if err != nil {
		t.Fatal(err)
	}
	s.Store = reopened
	got, err := s.ReadDoerDraftAs(context.Background(), subject, original.ID)
	if err != nil || got.PublicationKey != original.PublicationKey || got.Version != 1 || got.Contract.ExpectedText == nil || *got.Contract.ExpectedText != "" {
		t.Fatalf("readback=%+v err=%v", got, err)
	}
	input.ID, input.ExpectedVersion = got.ID, got.Version
	input.Contract.Task = "Write an updated result"
	updated, err := s.SaveDoerDraftAs(context.Background(), subject, input)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != original.ID || updated.PublicationKey != original.PublicationKey || updated.Version != 2 || updated.Agent != original.Agent || !updated.CreatedAt.Equal(original.CreatedAt) || !updated.ExpiresAt.Equal(original.ExpiresAt) {
		t.Fatalf("identity changed: %+v", updated)
	}
	input.Contract.WritableFiles[0] = "mutated.txt"
	read, err := s.ReadDoerDraftAs(context.Background(), subject, original.ID)
	if err != nil || read.Contract.WritableFiles[0] != "result.txt" {
		t.Fatalf("caller alias persisted: %+v %v", read, err)
	}
	if _, err := os.Stat(s.Store.Root() + "/queue-loop-intent"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("execution intent created: %v", err)
	}
}

func TestDoerDraftDenialsPreserveRecord(t *testing.T) {
	s, subject, input := draftFixture(t)
	draft, err := s.SaveDoerDraftAs(context.Background(), subject, input)
	if err != nil {
		t.Fatal(err)
	}
	other := subject
	other.PrincipalID = "other"
	if _, err := s.ReadDoerDraftAs(context.Background(), other, draft.ID); !errors.Is(err, ErrDenied) {
		t.Fatalf("cross-principal read: %v", err)
	}
	if _, err := s.SaveDoerDraftAs(context.Background(), other, input); !errors.Is(err, ErrDenied) {
		t.Fatalf("cross-principal write: %v", err)
	}
	// Even another configured-principal service sharing this Store cannot adopt it.
	otherService := &Service{Config: s.Config, Store: s.Store, Now: s.Now}
	otherService.Config.Principal.ID = other.PrincipalID
	input.ID, input.ExpectedVersion = draft.ID, draft.Version
	if _, err := otherService.SaveDoerDraftAs(context.Background(), other, input); err == nil {
		t.Fatal("foreign principal adopted draft")
	}
	if _, err := otherService.ReadDoerDraftAs(context.Background(), other, draft.ID); err == nil {
		t.Fatal("foreign principal read draft")
	}
	input.ExpectedVersion = 2
	if _, err := s.SaveDoerDraftAs(context.Background(), subject, input); !errors.Is(err, ErrConflict) {
		t.Fatalf("version conflict: %v", err)
	}
	input.ExpectedVersion = 1
	input.Agent.Revision++
	if _, err := s.SaveDoerDraftAs(context.Background(), subject, input); !errors.Is(err, ErrConflict) {
		t.Fatalf("retarget: %v", err)
	}
	input.Agent = draft.Agent
	input.Contract.VerifyFile = "../escape"
	if _, err := s.SaveDoerDraftAs(context.Background(), subject, input); !errors.Is(err, ErrDoerDraftInvalid) {
		t.Fatalf("malformed contract: %v", err)
	}
	got, err := s.ReadDoerDraftAs(context.Background(), subject, draft.ID)
	if err != nil || got.Version != 1 || got.Agent != draft.Agent {
		t.Fatalf("denial mutated draft: %+v %v", got, err)
	}
	s.Now = func() time.Time { return draft.ExpiresAt }
	subject.ExpiresAt = draft.ExpiresAt.Add(time.Hour)
	input.Contract = draft.Contract
	if _, err := s.ReadDoerDraftAs(context.Background(), subject, draft.ID); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired read: %v", err)
	}
	if _, err := s.SaveDoerDraftAs(context.Background(), subject, input); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired write: %v", err)
	}
	var raw json.RawMessage
	if err := s.Store.Load(doerDraftKind, doerDraftSlot(subject.PrincipalID), &raw); err != nil || !strings.Contains(string(raw), draft.ID) {
		t.Fatalf("expired record deleted: %s %v", raw, err)
	}
}

func TestDoerDraftStrictProposalBoundary(t *testing.T) {
	s, subject, input := draftFixture(t)
	wire, _ := json.Marshal(input)
	for _, field := range []string{"authority", "mandate", "run", "principal_id", "publication_key", "approval"} {
		forged := strings.TrimSuffix(string(wire), "}") + `,"` + field + `":"forged"}`
		var decoded DoerDraftInput
		if json.Unmarshal([]byte(forged), &decoded) == nil {
			t.Fatalf("accepted %s", field)
		}
	}
	var decoded DoerDraftInput
	if json.Unmarshal([]byte(`{"loop_id":"one","loop_id":"two"}`), &decoded) == nil {
		t.Fatal("accepted duplicate")
	}
	if json.Unmarshal([]byte(`null`), &decoded) == nil {
		t.Fatal("accepted null")
	}
	input.Contract.Task = strings.Repeat("x", 4097)
	if _, err := s.SaveDoerDraftAs(context.Background(), subject, input); !errors.Is(err, ErrDoerDraftInvalid) {
		t.Fatalf("oversized task: %v", err)
	}
	input.Contract.Task = "valid"
	input.Agent.Digest = "latest"
	if _, err := s.SaveDoerDraftAs(context.Background(), subject, input); !errors.Is(err, ErrDoerDraftInvalid) {
		t.Fatalf("inexact agent: %v", err)
	}
	subject.ExpiresAt = s.Now()
	if _, err := s.SaveDoerDraftAs(context.Background(), subject, input); !errors.Is(err, ErrDenied) {
		t.Fatalf("expired authentication: %v", err)
	}
}

func TestDoerDraftBoundedRetentionDoesNotRecycleExpired(t *testing.T) {
	s, subject, input := draftFixture(t)
	var draft DoerDraft
	for i := 0; i < DoerDraftMaxRetained; i++ {
		var err error
		draft, err = s.SaveDoerDraftAs(context.Background(), subject, input)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.SaveDoerDraftAs(context.Background(), subject, input); !errors.Is(err, ErrDoerDraftCapacity) {
		t.Fatalf("capacity: %v", err)
	}
	s.Now = func() time.Time { return draft.ExpiresAt }
	subject.ExpiresAt = draft.ExpiresAt.Add(time.Hour)
	if _, err := s.SaveDoerDraftAs(context.Background(), subject, input); !errors.Is(err, ErrDoerDraftCapacity) {
		t.Fatalf("implicit recycling: %v", err)
	}
}

func TestDoerDraftConcurrentOptimisticWrites(t *testing.T) {
	s, subject, input := draftFixture(t)
	draft, err := s.SaveDoerDraftAs(context.Background(), subject, input)
	if err != nil {
		t.Fatal(err)
	}
	otherStore, err := store.Open(s.Store.Root())
	if err != nil {
		t.Fatal(err)
	}
	other := &Service{Config: s.Config, Store: otherStore, Now: s.Now}
	input.ID, input.ExpectedVersion = draft.ID, draft.Version
	// This is a concurrent material edit; identical re-review is deliberately
	// idempotent so an already approved continuation retains its version.
	input.Contract.Task = "A deliberate concurrent edit"
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, service := range []*Service{s, other} {
		wg.Add(1)
		go func(service *Service) {
			defer wg.Done()
			_, err := service.SaveDoerDraftAs(context.Background(), subject, input)
			results <- err
		}(service)
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
}

type draftAgentRepository struct {
	fleet.Repository
	exact, latest registry.AgentRevision
}

func (r draftAgentRepository) GetAgentRevision(context.Context, string, uint64) (registry.AgentRevision, error) {
	return r.exact, nil
}
func (r draftAgentRepository) LatestAgentRevision(context.Context, string) (registry.AgentRevision, error) {
	return r.latest, nil
}

func TestDoerDraftPublicationRevalidatesExactAgentWithoutSubstitution(t *testing.T) {
	s, subject, input := draftFixture(t)
	draft, err := s.SaveDoerDraftAs(context.Background(), subject, input)
	if err != nil {
		t.Fatal(err)
	}
	agent := registry.AgentRevision{AgentID: input.Agent.ID, Revision: input.Agent.Revision, Digest: input.Agent.Digest, Lifecycle: registry.LifecycleEnabled}
	agent.Ownership.OwnerID = subject.PrincipalID
	s.FleetRepository = draftAgentRepository{exact: agent, latest: agent}
	if _, err := s.ValidateDoerDraftPublicationAs(context.Background(), subject, draft.ID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ValidateDoerDraftPublicationAs(context.Background(), subject, draft.ID, 2); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale draft: %v", err)
	}
	for _, change := range []string{"revision", "disabled", "owner", "digest"} {
		latest := agent
		switch change {
		case "revision":
			latest.Revision++
		case "disabled":
			latest.Lifecycle = registry.LifecycleDisabled
		case "owner":
			latest.Ownership.OwnerID = "other"
		case "digest":
			latest.Digest = "sha256:" + strings.Repeat("b", 64)
		}
		s.FleetRepository = draftAgentRepository{exact: agent, latest: latest}
		if _, err := s.ValidateDoerDraftPublicationAs(context.Background(), subject, draft.ID, 1); !errors.Is(err, ErrDenied) {
			t.Fatalf("%s: %v", change, err)
		}
		got, err := s.ReadDoerDraftAs(context.Background(), subject, draft.ID)
		if err != nil || got.Agent != input.Agent || got.PublicationKey != draft.PublicationKey {
			t.Fatalf("substituted stale Agent: %+v %v", got, err)
		}
	}
}
