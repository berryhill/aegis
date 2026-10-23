package app

import (
	"context"
	"errors"
	"os"
	"reflect"
	"sync"
	"testing"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

func TestRetainedDoerBlockedSetupDoesNotReserveEditableIntent(t *testing.T) {
	s, subject, input := draftFixture(t)
	s.Config.Credentials.Authority.DeploymentID = "freeze-fixture"
	ctx := context.Background()
	c := testCharter(s.Now())
	c.AgentID = input.Agent.ID
	c.Runtime.Target = "aegis-owned-ephemeral"
	c.Stanzas = c.Stanzas[:1]
	c.Stanzas[0].Grant = core.Grant{}
	c.Stanzas[0].Scopes = core.Scopes{}
	c.Stanzas[0].Hermes = core.HermesConfig{Model: "proof-no-key", Provider: "none"}
	canonical, err := core.Canonicalize(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Store.SaveCharter(canonical); err != nil {
		t.Fatal(err)
	}
	agent := registry.AgentRevision{AgentID: input.Agent.ID, Revision: input.Agent.Revision, Digest: input.Agent.Digest, Lifecycle: registry.LifecycleEnabled}
	agent.Charter = reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: c.AgentID, Revision: c.Revision, Digest: canonical.Digest}
	agent.Runtime = registry.RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent", Target: c.Runtime.Target}
	agent.Ownership.OwnerID = subject.PrincipalID
	agent.SchemaVersion = registry.AgentRevisionSchemaVersion
	agent.Source = registry.FleetSource{FleetID: "fixture", Kind: "test", SourceID: "source"}
	agent.Ownership.AccountabilityID = subject.PrincipalID
	agent, err = registry.SealRevision(agent)
	if err != nil {
		t.Fatal(err)
	}
	input.Agent = agentRevisionRef(agent)
	s.FleetRepository = &hostApprovalRepository{Repository: draftAgentRepository{exact: agent, latest: agent}, initial: agent, latest: agent, registration: registry.AgentRegistration{SchemaVersion: registry.AgentRegistrationSchemaVersion, AgentID: agent.AgentID, Source: agent.Source, InitialRevision: input.Agent}}
	s.Fleet = &orchestration.FleetService{}
	s.QueueWorker = &orchestration.QueueWorker{}
	draft, err := s.SaveDoerDraftAs(ctx, subject, input)
	if err != nil {
		t.Fatal(err)
	}
	request := ExecuteDoerTaskInput{ID: draft.ID, ExpectedVersion: draft.Version}
	out, err := s.ExecuteDoerTaskAs(ctx, subject, request)
	if err != nil || out.Status != "independent_setup_required" || out.Publication != nil || out.Run != nil {
		t.Fatalf("blocked execute: %+v %v", out, err)
	}
	if out.Readiness.Reason != "doer_runtime_unconfigured" {
		t.Fatalf("wrong setup boundary: %+v", out.Readiness)
	}
	got, err := s.ReadDoerDraftAs(ctx, subject, draft.ID)
	if err != nil || !reflect.DeepEqual(got, draft) {
		t.Fatalf("setup changed task: %+v %v", got, err)
	}
	for _, kind := range []string{"queue-loop-intent", "doer-host-approvals"} {
		if _, err := os.Stat(s.Store.Root() + "/" + kind); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("blocked setup created %s: %v", kind, err)
		}
	}
	input.ID, input.ExpectedVersion = draft.ID, draft.Version
	unchanged, err := s.SaveDoerDraftAs(ctx, subject, input)
	if err != nil || !reflect.DeepEqual(unchanged, draft) {
		t.Fatalf("unchanged save: %+v %v", unchanged, err)
	}
	replay, err := s.ExecuteDoerTaskAs(ctx, subject, request)
	if err != nil || !reflect.DeepEqual(replay.Task, draft) || replay.Status != out.Status {
		t.Fatalf("replay: %+v %v", replay, err)
	}
	input.Contract.Task = "Material edit before execution is reserved"
	edited, err := s.SaveDoerDraftAs(ctx, subject, input)
	if err != nil || edited.ID != draft.ID || edited.PublicationKey != draft.PublicationKey || edited.Version != draft.Version+1 || edited.ExecutionDigest != "" {
		t.Fatalf("safe editable setup: %+v %v", edited, err)
	}
	continued, err := s.ExecuteDoerTaskAs(ctx, subject, ExecuteDoerTaskInput{ID: edited.ID, ExpectedVersion: edited.Version})
	if err != nil || continued.Status != "independent_setup_required" || !reflect.DeepEqual(continued.Task, edited) {
		t.Fatalf("same task continuation: %+v %v", continued, err)
	}
	// Model a restart immediately after the owning-store freeze, before the
	// reservation write. Concurrent Saves must observe that durable fence.
	candidate, _, err := NewDoerLoopRevision(edited.LoopID, edited.Revision, edited.PreviousDigest, edited.Contract)
	if err != nil {
		t.Fatal(err)
	}
	err = s.Store.Update(doerDraftKind, doerDraftSlot(subject.PrincipalID), func(raw []byte) (any, error) {
		set, e := decodeDoerDraftSet(raw, subject.PrincipalID)
		if e != nil {
			return nil, e
		}
		set.Drafts[0].ExecutionDigest = candidate.Digest
		return set, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	frozen := edited
	frozen.ExecutionDigest = candidate.Digest
	input.ExpectedVersion = edited.Version
	input.Contract.Task = "Forbidden post-freeze edit"
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, e := s.SaveDoerDraftAs(ctx, subject, input); !errors.Is(e, ErrConflict) {
				t.Errorf("frozen concurrent edit: %v", e)
			}
		})
	}
	wg.Wait()
	got, err = s.ReadDoerDraftAs(ctx, subject, edited.ID)
	if err != nil || !reflect.DeepEqual(got, frozen) {
		t.Fatalf("interrupted freeze changed: %+v %v", got, err)
	}
	input.Contract = frozen.Contract
	if saved, e := s.SaveDoerDraftAs(ctx, subject, input); e != nil || !reflect.DeepEqual(saved, frozen) {
		t.Fatalf("frozen unchanged save: %+v %v", saved, e)
	}
	run := QueueLoopInput{Agent: frozen.Agent, Loop: RevisionReference(candidate.LoopID, candidate.Revision, candidate.Digest), IdempotencyKey: frozen.PublicationKey + "-run", Activate: true}
	for range 2 {
		if _, e := s.reserveExactDoerRun(ctx, subject, run); e != nil {
			t.Fatal(e)
		}
	}
	_, expected, err := normalizeLoopRunIdentity(run)
	if err != nil {
		t.Fatal(err)
	}
	var reserved string
	if e := s.Store.Load("queue-loop-intent", queueLoopRequestID(subject, run), &reserved); e != nil || reserved != expected {
		t.Fatalf("exact reservation: %q %v", reserved, e)
	}
	replayed, err := s.ExecuteDoerTaskAs(ctx, subject, ExecuteDoerTaskInput{ID: frozen.ID, ExpectedVersion: frozen.Version})
	if err != nil || !reflect.DeepEqual(replayed.Task, frozen) || replayed.Publication != nil || replayed.Run != nil {
		t.Fatalf("frozen blocked replay: %+v %v", replayed, err)
	}
}
