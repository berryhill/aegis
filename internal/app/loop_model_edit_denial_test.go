package app

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/registry"
	"github.com/berryhill/aegis/internal/store"
)

func TestOwnedLoopModelEditPersistenceConflictAndDenials(t *testing.T) {
	s, sub, input, _ := loopModelEditFixture(t)
	ctx := context.Background()
	result, err := s.SubmitOwnedLoopEditAs(ctx, sub, input)
	if err != nil {
		t.Fatal(err)
	}
	edit := *result.PendingEdit
	reopened, err := store.Open(s.Store.Root())
	if err != nil {
		t.Fatal(err)
	}
	s.Store = reopened
	got, err := s.ReadOwnedLoopEditAs(ctx, sub, edit.ID)
	if err != nil || !reflect.DeepEqual(got, edit) {
		t.Fatalf("durable retained bytes changed: %v", err)
	}
	changed := input
	changed.Revision.Name = "changed after request"
	changed.Revision, _, err = loop.NewRevision(changed.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SubmitOwnedLoopEditAs(ctx, sub, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("same-key changed payload: %v", err)
	}
	foreign := sub
	foreign.PrincipalID = "other"
	if _, err = s.ReadOwnedLoopEditAs(ctx, foreign, edit.ID); !errors.Is(err, ErrDenied) {
		t.Fatalf("foreign read: %v", err)
	}
	if _, err = s.SubmitOwnedLoopEditAs(ctx, foreign, input); !errors.Is(err, ErrDenied) {
		t.Fatalf("foreign write: %v", err)
	}
	human := core.Subject{ID: "password:fixture", Kind: "principal", PrincipalID: sub.PrincipalID, Issuer: "aegis-principal-auth", Method: "password", AuthenticatedAt: s.Now(), ExpiresAt: sub.ExpiresAt}
	s.DashboardActionAuthentication = func(core.Subject) error { return ErrDenied }
	if _, err = s.ConfirmOwnedLoopModelAs(ctx, human, edit); !errors.Is(err, ErrDenied) {
		t.Fatalf("revoked browser: %v", err)
	}
	s.DashboardActionAuthentication = func(core.Subject) error { return nil }
	stale := human
	stale.AuthenticatedAt = s.Now().Add(-s.Config.Principal.AuthTTL)
	if _, err = s.ConfirmOwnedLoopModelAs(ctx, stale, edit); !errors.Is(err, ErrDenied) {
		t.Fatalf("stale password: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = s.ConfirmOwnedLoopModelAs(cancelled, human, edit); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled approval: %v", err)
	}
	charters, _ := s.ListCharters(input.AgentID)
	if len(charters) != 1 {
		t.Fatal("denial imported a charter")
	}
	s.Now = func() time.Time { return edit.ExpiresAt }
	sub.AuthenticatedAt, sub.ExpiresAt = edit.ExpiresAt, edit.ExpiresAt.Add(time.Minute)
	if expired, e := s.ReadOwnedLoopEditAs(ctx, sub, edit.ID); e != nil || expired.Status != "expired" {
		t.Fatalf("expired proposal: %v", err)
	}
	var raw json.RawMessage
	if err = s.Store.Load(loopModelEditKind, doerDraftSlot(sub.PrincipalID), &raw); err != nil {
		t.Fatal("expired history lost")
	}
	retained, err := decodeLoopModelEdits(raw, sub.PrincipalID)
	if err != nil || len(retained.Edits) != 1 || !reflect.DeepEqual(retained.Edits[0].Input, edit.Input) || retained.Edits[0].ExpiresAt != edit.ExpiresAt {
		t.Fatal("expiry changed original history")
	}
}

func TestOwnedLoopModelEditCompetingRevisionDoesNotApprove(t *testing.T) {
	s, sub, input, old := loopModelEditFixture(t)
	ctx := context.Background()
	result, err := s.SubmitOwnedLoopEditAs(ctx, sub, input)
	if err != nil {
		t.Fatal(err)
	}
	edit := *result.PendingEdit
	other, _, err := loop.NewDoerReusableRevision(old.Revision.LoopID, 2, old.Revision.Digest, loop.DoerReusableContract{MaxAttempts: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PublishLoopAs(ctx, sub, PublishLoopInput{AgentID: input.AgentID, Revision: other, ExpectedPreviousDigest: old.Revision.Digest, IdempotencyKey: "competing-revision"}); err != nil {
		t.Fatal(err)
	}
	human := core.Subject{ID: "password:fixture", Kind: "principal", PrincipalID: sub.PrincipalID, Issuer: "aegis-principal-auth", Method: "password", AuthenticatedAt: s.Now(), ExpiresAt: sub.ExpiresAt}
	s.DashboardActionAuthentication = func(core.Subject) error { return nil }
	if _, err = s.ConfirmOwnedLoopModelAs(ctx, human, edit); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting Loop revision still approved: %v", err)
	}
	charters, _ := s.ListCharters(input.AgentID)
	if len(charters) != 1 {
		t.Fatal("stale Loop review granted authority")
	}
}

func TestOwnedLoopModelEditAgentDriftDoesNotApprove(t *testing.T) {
	s, sub, input, _ := loopModelEditFixture(t)
	ctx := context.Background()
	result, err := s.SubmitOwnedLoopEditAs(ctx, sub, input)
	if err != nil {
		t.Fatal(err)
	}
	edit := *result.PendingEdit
	if _, err = s.SetAgentLifecycleAs(ctx, sub, input.AgentID, SetAgentLifecycleInput{Expected: edit.Expected, Lifecycle: registry.LifecycleDisabled}); err != nil {
		t.Fatal(err)
	}
	human := core.Subject{ID: "password:fixture", Kind: "principal", PrincipalID: sub.PrincipalID, Issuer: "aegis-principal-auth", Method: "password", AuthenticatedAt: s.Now(), ExpiresAt: sub.ExpiresAt}
	s.DashboardActionAuthentication = func(core.Subject) error { return nil }
	if _, err = s.ConfirmOwnedLoopModelAs(ctx, human, edit); !errors.Is(err, ErrConflict) {
		t.Fatalf("Agent drift: %v", err)
	}
	charters, _ := s.ListCharters(input.AgentID)
	if len(charters) != 1 {
		t.Fatal("stale Agent granted model authority")
	}
	if _, err = s.PublishOwnedLoopEditAs(ctx, sub, edit.ID); err == nil {
		t.Fatal("disabled owner published")
	}
}

func TestOwnedLoopModelEditNoStanzaUnionOrSecondModel(t *testing.T) {
	s, sub, input, _ := loopModelEditFixture(t)
	ctx := context.Background()
	result, err := s.SubmitOwnedLoopEditAs(ctx, sub, input)
	if err != nil {
		t.Fatal(err)
	}
	original := result.PendingEdit.Original
	candidate, _, err := loop.NewRevision(input.Revision)
	if err != nil {
		t.Fatal(err)
	}
	for i := range candidate.Steps {
		if candidate.Steps[i].Executable != nil && candidate.Steps[i].Executable.Backend == "hermes" {
			candidate.Steps[i].Model = "second-model"
			candidate.Steps[i].Executable.Model = "second-model"
			break
		}
	}
	candidate, _, err = loop.NewRevision(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = loopEditCharter(original, candidate, 2); err == nil {
		t.Fatal("two missing models collapsed to one")
	}
	c := original.Charter
	other := c.Stanzas[0]
	other.ID = "other"
	other.Authentication.Selectors = []core.IdentitySelector{{SubjectIDs: []string{"other-subject"}, Issuers: []string{"local-os"}, Environments: []string{"local"}}}
	c.Stanzas = append(c.Stanzas, other)
	cc, err := core.Canonicalize(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = loopEditCharter(cc, input.Revision, 2); err == nil {
		t.Fatal("ambiguous stanzas unioned")
	}
}

func TestOwnedLoopModelUpdatePreservesOtherNodesAndExplicitEnvelope(t *testing.T) {
	s, sub, input, old := loopModelEditFixture(t)
	ctx := context.Background()
	result, err := s.UpdateOwnedLoopModelAs(ctx, sub, old.Revision.LoopID, 1, OwnedLoopModelUpdateInput{AgentID: input.AgentID, Provider: "ollama", Model: "fixture-local"})
	if err != nil || result.PendingEdit != nil {
		t.Fatalf("explicit approved selection: %v", err)
	}
	for i, step := range old.Revision.Steps {
		if step.Executable != nil && step.Executable.Backend == "hermes" {
			step.Provider, step.Model = "ollama", "fixture-local"
			executable := *step.Executable
			step.Executable = &executable
			step.Executable.Provider, step.Executable.Model = "ollama", "fixture-local"
		}
		if !reflect.DeepEqual(step, result.Revision.Steps[i]) {
			t.Fatalf("unrequested node changes: %s", step.ID)
		}
	}
	if _, err = s.UpdateOwnedLoopModelAs(ctx, sub, old.Revision.LoopID, 1, OwnedLoopModelUpdateInput{AgentID: input.AgentID, Provider: "ollama", Model: "fixture-local", Steps: []string{"unknown"}}); err == nil {
		t.Fatal("unknown step accepted")
	}
}
