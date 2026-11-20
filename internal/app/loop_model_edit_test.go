package app

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"github.com/berryhill/aegis/internal/registry"
)

func loopModelEditFixture(t *testing.T) (*Service, core.Subject, PublishLoopInput, PublishedLoop) {
	t.Helper()
	s := testService(t)
	ctx := context.Background()
	repo, err := fleetbadger.Open(ctx, filepath.Join(s.Config.StateDir, "persistence", "fleet-v1"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	configureLocalImportFleet(t, s, repo)
	sub := localImportPrincipal(s)
	c := testCharter(s.Now())
	c.Stanzas = c.Stanzas[:1]
	c.Stanzas[0].Grant, c.Stanzas[0].Scopes = core.Grant{}, core.Scopes{}
	c.Stanzas[0].Hermes = core.HermesConfig{Provider: "ollama", Model: "fixture-local", LocalInference: &core.LocalInference{Kind: "ollama", Endpoint: "http://127.0.0.1:11434", ModelDigest: "sha256:" + strings.Repeat("a", 64)}}
	canonical, err := core.Canonicalize(c)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Store.SaveCharter(canonical); err != nil {
		t.Fatal(err)
	}
	agent, err := registry.SealRevision(registry.AgentRevision{SchemaVersion: registry.AgentRevisionSchemaVersion, AgentID: c.AgentID, Revision: 1, Source: registry.FleetSource{FleetID: "fixture", Kind: "test", SourceID: "source"}, Runtime: registry.RuntimeBinding{Adapter: c.Runtime.Adapter, Runtime: c.Runtime.Runtime, Target: c.Runtime.Target}, Ownership: registry.Ownership{OwnerID: sub.PrincipalID, AccountabilityID: sub.PrincipalID}, Lifecycle: registry.LifecycleEnabled, Charter: RevisionReference(c.AgentID, c.Revision, canonical.Digest)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.RegisterAgent(ctx, registry.AgentRegistration{SchemaVersion: registry.AgentRegistrationSchemaVersion, AgentID: agent.AgentID, Source: agent.Source, InitialRevision: agentRevisionRef(agent)}, agent, fleet.AuditFact{Event: core.AuditEvent{Type: "agent.registered", SubjectID: sub.ID, PrincipalID: sub.PrincipalID, Outcome: "ok", Reason: "fixture"}}); err != nil {
		t.Fatal(err)
	}
	old, _, err := loop.NewDoerReusableRevision("owned-model-edit", 1, "", loop.DoerReusableContract{MaxAttempts: 2})
	if err != nil {
		t.Fatal(err)
	}
	published, err := s.PublishLoopAs(ctx, sub, PublishLoopInput{AgentID: agent.AgentID, Revision: old, IdempotencyKey: "original"})
	if err != nil {
		t.Fatal(err)
	}
	next, _, err := loop.NewDoerReusableRevision(old.LoopID, 2, old.Digest, loop.DoerReusableContract{MaxAttempts: 2})
	if err != nil {
		t.Fatal(err)
	}
	var lanes []loop.DoerStepLane
	for _, step := range next.Steps {
		if step.Executable != nil && step.Executable.Backend == "hermes" {
			lanes = append(lanes, loop.DoerStepLane{ID: step.ID, Provider: "openai-codex", Model: "gpt-6.2-sol", ExecutableProvider: "openai-codex", ExecutableModel: "gpt-6.2-sol"})
		}
	}
	next, _, err = loop.ApplyDoerStepLanes(next, lanes)
	if err != nil {
		t.Fatal(err)
	}
	if len(lanes) != 4 {
		t.Fatalf("expected four Hermes turns, got %d", len(lanes))
	}
	return s, sub, PublishLoopInput{AgentID: agent.AgentID, Revision: next, ExpectedPreviousDigest: old.Digest, IdempotencyKey: "model-edit"}, published
}

func TestOwnedLoopModelEditApprovedEnvelopePublishesNormally(t *testing.T) {
	s, sub, input, old := loopModelEditFixture(t)
	candidate, _, err := loop.NewDoerReusableRevision(input.Revision.LoopID, 2, old.Revision.Digest, loop.DoerReusableContract{MaxAttempts: 2})
	if err != nil {
		t.Fatal(err)
	}
	input.Revision = candidate
	result, err := s.SubmitOwnedLoopEditAs(context.Background(), sub, input)
	if err != nil || result.PendingEdit != nil || result.Revision.Digest != candidate.Digest {
		t.Fatalf("normal publication: %+v %v", result, err)
	}
	got, err := s.GetLoopAs(context.Background(), sub, candidate.LoopID, 2)
	if err != nil || got.Digest != candidate.Digest {
		t.Fatalf("publication readback: %v", err)
	}
	if _, err = s.ReadOwnedLoopEditAs(context.Background(), sub, loopEditID(sub.PrincipalID, input.IdempotencyKey)); err == nil {
		t.Fatal("approved envelope created operator work")
	}
}

func TestOwnedLoopModelEditIndependentDecisionAndExactContinuation(t *testing.T) {
	s, sub, input, old := loopModelEditFixture(t)
	ctx := context.Background()
	pending, err := s.SubmitOwnedLoopEditAs(ctx, sub, input)
	if err != nil {
		t.Fatal(err)
	}
	edit := *pending.PendingEdit
	if edit.MissingModel.Provider != "openai-codex" || edit.MissingModel.Model != "gpt-6.2-sol" {
		t.Fatal("missing model not exact")
	}
	original := edit.Original.Charter.Stanzas[0].Hermes
	proposed := edit.Proposed.Charter.Stanzas[0].Hermes
	if original.Provider != proposed.Provider || original.Model != proposed.Model || !reflect.DeepEqual(original.LocalInference, proposed.LocalInference) {
		t.Fatal("default changed")
	}
	// Neither the authenticated requester nor its borrowed peer principal can approve.
	if _, err = s.ConfirmOwnedLoopModelAs(ctx, sub, edit); !errors.Is(err, ErrDenied) {
		t.Fatalf("peer self-approval: %v", err)
	}
	if _, err = s.PublishOwnedLoopEditAs(ctx, sub, edit.ID); !errors.Is(err, ErrDenied) {
		t.Fatalf("unapproved continuation: %v", err)
	}
	charters, _ := s.ListCharters(input.AgentID)
	if len(charters) != 1 {
		t.Fatal("proposal or peer approval imported authority")
	}
	// Controlled fixture for the trusted Console adapter; actual password/CSRF
	// and single-use receipt behavior is exercised separately in internal/api.
	human := core.Subject{ID: "password:independent-fixture", Kind: "principal", PrincipalID: sub.PrincipalID, Issuer: "aegis-principal-auth", Method: "password", AuthenticatedAt: s.Now(), ExpiresAt: sub.ExpiresAt}
	s.DashboardActionAuthentication = func(got core.Subject) error {
		if !reflect.DeepEqual(got, human) {
			return ErrDenied
		}
		return nil
	}
	tampered := edit
	tampered.Proposed.Digest = edit.Original.Digest
	if _, err = s.ConfirmOwnedLoopModelAs(ctx, human, tampered); !errors.Is(err, ErrConflict) {
		t.Fatalf("tampered review: %v", err)
	}
	agent, err := s.ConfirmOwnedLoopModelAs(ctx, human, edit)
	if err != nil {
		t.Fatal(err)
	}
	if agent.Revision.CharterSuccessor == nil || agent.Revision.CharterSuccessor.ApprovedBy != human.PrincipalID || agent.Revision.Charter.Digest != edit.Proposed.Digest {
		t.Fatal("missing exact independent decision provenance")
	}
	if _, err = s.GetLoopAs(ctx, sub, input.Revision.LoopID, 2); !errors.Is(err, fleet.ErrNotFound) {
		t.Fatal("approval published edit")
	}
	replay, err := s.SubmitOwnedLoopEditAs(ctx, sub, input)
	if err != nil || replay.PendingEdit == nil || replay.PendingEdit.Status != "approved" || !reflect.DeepEqual(replay.PendingEdit.Input, edit.Input) {
		t.Fatalf("proposal replay lost exact revision: %v", err)
	}
	published, err := s.PublishOwnedLoopEditAs(ctx, sub, edit.ID)
	if err != nil || published.PendingEdit != nil || published.Revision.Digest != input.Revision.Digest {
		t.Fatalf("continuation: %v", err)
	}
	again, err := s.PublishOwnedLoopEditAs(ctx, sub, edit.ID)
	if err != nil || !again.Decision.Idempotent {
		t.Fatalf("publication replay: %v", err)
	}
	got, err := s.GetLoopViewAs(ctx, sub, old.Revision.LoopID, 1)
	if err != nil || got.Revision.Digest != old.Revision.Digest || len(got.History) != 0 || got.Provenance.Charter.Revision != 1 {
		t.Fatal("original publication/history changed")
	}
	items, err := s.FleetRepository.ListQueueItems(ctx)
	if err != nil || len(items) != 0 {
		t.Fatal("edit/approval executed work")
	}
}

func TestOwnedLoopModelEditRetainsDeniedRevision(t *testing.T) {
	s, sub, input, old := loopModelEditFixture(t)
	ctx := context.Background()
	// The original publication boundary must remain denied, not be relaxed.
	if _, err := s.PublishLoopAs(ctx, sub, input); !errors.Is(err, ErrDenied) {
		t.Fatalf("baseline lane denial=%v", err)
	}
	t.Log("prior behavior reproduced: exact owned r2 denied by approved Ollama-only charter")
	editor, ok := any(s).(interface {
		SubmitOwnedLoopEditAs(context.Context, core.Subject, PublishLoopInput) (PublishedLoop, error)
	})
	if !ok {
		t.Fatal("missing product-owned retained edit continuation after model denial")
	}
	result, err := editor.SubmitOwnedLoopEditAs(ctx, sub, input)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(result)
	if !strings.Contains(string(raw), `"pending_edit"`) || !strings.Contains(string(raw), "gpt-6.2-sol") {
		t.Fatalf("missing exact pending edit: %s", raw)
	}
	got, err := s.GetLoopAs(ctx, sub, old.Revision.LoopID, 1)
	if err != nil || got.Digest != old.Revision.Digest {
		t.Fatal("original revision lost")
	}
	if _, err = s.GetLoopAs(ctx, sub, input.Revision.LoopID, 2); !errors.Is(err, fleet.ErrNotFound) {
		t.Fatalf("pending edit published: %v", err)
	}
}
