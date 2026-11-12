package app

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

func TestDoerDraftCandidateStepLanesPinDigestAndReview(t *testing.T) {
	s, sub, d, _ := controllerParticipantFixture(t)
	ctx := context.Background()
	lanes := []loop.DoerStepLane{
		{ID: "completion", Provider: "ollama", Model: "qwen3.5:4b", ExecutableProvider: "ollama", ExecutableModel: "qwen3.5:4b"},
		{ID: "implement", Provider: "ollama", Model: "qwen3.5:4b", ExecutableProvider: "ollama", ExecutableModel: "qwen3.5:4b"},
	}
	input := DoerDraftInput{
		ID:              d.ID,
		ExpectedVersion: d.Version,
		Name:            "Step-lane review",
		CandidateSteps:  lanes,
		Agent:           d.Agent,
		LoopID:          d.LoopID,
		Revision:        d.Revision,
		PreviousDigest:  d.PreviousDigest,
		Contract:        d.Contract,
	}
	updated, err := s.SaveDoerDraftAs(ctx, sub, input)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != d.Version+1 || !reflect.DeepEqual(updated.CandidateSteps, lanes) {
		t.Fatalf("step-lane draft CAS/steps mismatch: %+v", updated)
	}
	candidate, _, err := updated.Candidate()
	if err != nil {
		t.Fatal(err)
	}
	defaultCandidate, _, err := loop.NewDoerRevision(d.LoopID, d.Revision, d.PreviousDigest, d.Contract)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Digest == defaultCandidate.Digest || !reflect.DeepEqual(loop.ExtractDoerStepLanes(candidate.Steps), lanes) {
		t.Fatalf("candidate did not bind step lanes: %+v", candidate)
	}
	input.ExpectedVersion = updated.Version
	unchanged, err := s.SaveDoerDraftAs(ctx, sub, input)
	if err != nil || !reflect.DeepEqual(unchanged, updated) {
		t.Fatalf("unchanged step-lane draft invalidated review: %v", err)
	}
	badInput := input
	badInput.CandidateSteps = []loop.DoerStepLane{{ID: "unknown", Provider: "ollama", Model: "qwen3.5:4b"}}
	if _, err := s.SaveDoerDraftAs(ctx, sub, badInput); !errors.Is(err, ErrDoerDraftInvalid) {
		t.Fatalf("invalid CandidateSteps accepted: %v", err)
	}
	prepared, agent, err := s.PrepareDoerHostReviewAs(ctx, sub, updated.ID, updated.Version)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.ExpectedCandidateDigest != candidate.Digest {
		t.Fatalf("host review preflight lost step-lane digest: %+v", prepared)
	}
	wrong := prepared
	wrong.ExpectedCandidateDigest = defaultCandidate.Digest
	if _, err := s.ApproveDoerHostWriteAs(ctx, sub, wrong); !errors.Is(err, ErrConflict) {
		t.Fatalf("default-lane digest accepted for step-lane draft: %v", err)
	}
	approved, err := s.ApproveDoerHostWriteAs(ctx, sub, prepared)
	if err != nil {
		t.Fatalf("ApproveDoerHostWriteAs failed: %v", err)
	}
	if approved.Candidate != loopRevisionRef(candidate) || !reflect.DeepEqual(approved.CandidateSteps, lanes) {
		t.Fatalf("signed host approval lost step lanes: %+v", approved)
	}
	got, err := s.ReadDoerHostWriteApprovalAs(ctx, sub, updated.Agent, updated.Contract)
	if err != nil || !reflect.DeepEqual(got, approved) {
		t.Fatalf("signed step-lane approval readback failed: %v", err)
	}
	requester := sub
	requester.Kind, requester.Issuer, requester.Method = "principal", "aegis-principal-auth", "password"
	requester.ExpiresAt = s.Now().Add(s.Config.Principal.AuthTTL)
	intent := DoerContinuationIntent{
		DraftID:         updated.ID,
		DraftVersion:    updated.Version,
		CandidateDigest: candidate.Digest,
		Agent:           updated.Agent,
		Loop:            loopRevisionRef(candidate),
		Charter:         agent.Charter,
		PublicationKey:  updated.PublicationKey,
		Requester:       requester,
		SessionID:       "step-lanes-session",
		DeploymentID:    s.Config.Credentials.Authority.DeploymentID,
		ConfigIdentity:  s.DoerContinuationConfigIdentity(),
		ExpiresAt:       s.Now().Add(time.Minute),
		Run:             QueueLoopInput{Agent: updated.Agent, Loop: loopRevisionRef(candidate), IdempotencyKey: "step-lanes-run"},
	}
	if err := s.validateDoerContinuationIntent(ctx, requester, intent.SessionID, intent); err != nil {
		t.Fatalf("step-lane continuation denied: %v", err)
	}
	stale := intent
	stale.CandidateDigest = defaultCandidate.Digest
	if err := s.validateDoerContinuationIntent(ctx, requester, intent.SessionID, stale); !errors.Is(err, ErrConflict) {
		t.Fatalf("default-lane continuation accepted: %v", err)
	}
}

func TestReusableDoerStepLanesHostReviewAndRunPreparation(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	repo, err := fleetbadger.Open(ctx, filepath.Join(s.Config.StateDir, "persistence", "fleet-v1"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	configureLocalImportFleet(t, s, repo)
	s.Config.Credentials.Authority.DeploymentID = "reusable-step-lanes-fixture"
	sub := localImportPrincipal(s)
	c := testCharter(s.Now())
	c.Stanzas = c.Stanzas[:1]
	c.Stanzas[0].Authentication.Selectors[0].Issuers = []string{sub.Issuer}
	c.Stanzas[0].Grant = core.Grant{}
	c.Stanzas[0].Scopes = core.Scopes{Credentials: []string{"provider:openai-codex", "provider:anthropic"}}
	c.Stanzas[0].Hermes = core.HermesConfig{
		Model:    "gpt-5.4",
		Provider: "openai-codex",
		AllowedLanes: []core.HermesLane{
			{ID: "default", Provider: "openai-codex", Model: "gpt-5.4"},
			{ID: "alt", Provider: "anthropic", Model: "claude-sonnet-4-5"},
		},
	}
	c.Stanzas[0].Approval.RequiredOperations = []string{"provision"}
	canonical, err := core.Canonicalize(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Store.SaveCharter(canonical); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.Save("receipts", "ready-"+c.AgentID, core.Receipt{ID: "ready-" + c.AgentID, CharterDigest: canonical.Digest, Status: "verified"}); err != nil {
		t.Fatal(err)
	}
	agent, err := registry.SealRevision(registry.AgentRevision{
		SchemaVersion: registry.AgentRevisionSchemaVersion,
		AgentID:       c.AgentID,
		Revision:      1,
		Source:        registry.FleetSource{FleetID: "fixture", Kind: "test", SourceID: "source"},
		Runtime:       registry.RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent", Target: c.Runtime.Target},
		Ownership:     registry.Ownership{OwnerID: sub.PrincipalID, AccountabilityID: sub.PrincipalID},
		Lifecycle:     registry.LifecycleEnabled,
		Charter:       reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: c.AgentID, Revision: c.Revision, Digest: canonical.Digest},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RegisterAgent(ctx, registry.AgentRegistration{SchemaVersion: registry.AgentRegistrationSchemaVersion, AgentID: agent.AgentID, Source: agent.Source, InitialRevision: agentRevisionRef(agent)}, agent, fleet.AuditFact{Event: core.AuditEvent{Type: "agent.registered", SubjectID: sub.ID, PrincipalID: sub.PrincipalID, Outcome: "ok", Reason: "fixture"}}); err != nil {
		t.Fatal(err)
	}

	base, _, err := loop.NewDoerReusableRevision("reusable-step-lanes", 1, "", loop.DoerReusableContract{MaxAttempts: 2})
	if err != nil {
		t.Fatal(err)
	}
	base.Name = "Reusable Step Lanes"
	lanes := []loop.DoerStepLane{
		{ID: "completion", Provider: "anthropic", Model: "claude-sonnet-4-5", ExecutableProvider: "anthropic", ExecutableModel: "claude-sonnet-4-5"},
		{ID: "implement", Provider: "anthropic", Model: "claude-sonnet-4-5", ExecutableProvider: "anthropic", ExecutableModel: "claude-sonnet-4-5"},
	}
	candidate, _, err := loop.ApplyDoerStepLanes(base, lanes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PublishLoopAs(ctx, sub, PublishLoopInput{AgentID: agent.AgentID, Revision: candidate, IdempotencyKey: "pub-reusable-step-lanes"}); err != nil {
		t.Fatalf("PublishLoopAs failed: %v", err)
	}

	ws := t.TempDir()
	text := "AEGIS_STEP_LANES_OK"
	makeRunInput := func(key string) QueueLoopInput {
		in := QueueLoopInput{
			Agent:          agentRevisionRef(agent),
			Loop:           loopRevisionRef(candidate),
			IdempotencyKey: key,
		}
		for _, port := range []struct {
			id    string
			kind  graph.ValueType
			value any
		}{
			{"task", graph.TypeString, "Write step-lane proof"},
			{"workspace", graph.TypeString, ws},
			{"writable_files", graph.TypeArray, []string{"proof.txt"}},
			{"verify_file", graph.TypeString, "proof.txt"},
			{"expected_text", graph.TypeString, text},
		} {
			rawVal, _ := json.Marshal(port.value)
			in.Inputs = append(in.Inputs, graph.NormalizedInput{PortID: port.id, Type: port.kind, Value: rawVal})
		}
		return in
	}

	runInput := makeRunInput("reusable-step-lanes-review-1")
	normRun, _, err := normalizeLoopRunIdentity(runInput)
	if err != nil {
		t.Fatal(err)
	}
	rd := newDoerReadiness()
	rd.definitionSchema = loop.DoerReusableSchemaVersion
	qres := QueueLoopResult{
		RequestID: queueLoopRequestID(sub, normRun),
		Reason:    "doer_host_approval_absent",
		Readiness: &rd,
	}
	if err := s.retainQueueLoopSetupReview(ctx, sub, runInput, &qres); err != nil {
		t.Fatalf("retainQueueLoopSetupReview failed for v5 step-lane loop: %v", err)
	}
	if qres.SetupReviewTask == nil || !reflect.DeepEqual(qres.SetupReviewTask.CandidateSteps, lanes) {
		t.Fatalf("retained setup review draft missing CandidateSteps: %+v", qres.SetupReviewTask)
	}
	prepared, _, err := s.PrepareDoerHostReviewAs(ctx, sub, qres.SetupReviewTask.ID, qres.SetupReviewTask.Version)
	if err != nil {
		t.Fatalf("PrepareDoerHostReviewAs failed: %v", err)
	}
	if prepared.ExpectedCandidateDigest != candidate.Digest {
		t.Fatalf("expected candidate digest %s, got %s", candidate.Digest, prepared.ExpectedCandidateDigest)
	}
	approved, err := s.ApproveDoerHostWriteAs(ctx, sub, prepared)
	if err != nil {
		t.Fatalf("ApproveDoerHostWriteAs failed for v5 step-lane loop: %v", err)
	}
	if !reflect.DeepEqual(approved.CandidateSteps, lanes) || approved.RunRequestID != qres.RequestID {
		t.Fatalf("approved v5 record mismatch: %+v", approved)
	}
	boundCtx, err := bindDoerRun(ctx, normRun, candidate)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := queuedDoerContract(boundCtx, candidate)
	if err != nil {
		t.Fatal(err)
	}
	runCtx := context.WithValue(boundCtx, exactDoerRunKey{}, exactDoerRun{requestID: qres.RequestID, candidate: loopRevisionRef(candidate)})
	if err := s.resolveDoerHostApproval(runCtx, contract, agent); err != nil {
		t.Fatalf("resolveDoerHostApproval denied approved v5 step-lane run: %v", err)
	}
}
