package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

// Isolated application regression, not a real-provider or live pilot receipt.
func controllerParticipantFixture(t *testing.T) (*Service, core.Subject, DoerDraft, *hostApprovalRepository) {
	t.Helper()
	s := testService(t)
	ctx := context.Background()
	sub, err := s.Authenticate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s.Config.Credentials.Authority.DeploymentID = "controller-participant-test"
	c := testCharter(s.Now())
	c.Stanzas = c.Stanzas[:1]
	c.Stanzas[0].Grant = core.Grant{}
	c.Stanzas[0].Scopes = core.Scopes{}
	c.Stanzas[0].Hermes = core.HermesConfig{Model: "proof-no-key", Provider: "none"}
	raw, _ := json.Marshal(c)
	canonical, err := s.ImportCharterAs(ctx, sub, raw)
	if err != nil {
		t.Fatal(err)
	}
	a, err := registry.SealRevision(registry.AgentRevision{SchemaVersion: registry.AgentRevisionSchemaVersion, AgentID: c.AgentID, Revision: 1, Source: registry.FleetSource{FleetID: "fixture", Kind: "test", SourceID: "separately-admitted"}, Runtime: registry.RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent", Target: c.Runtime.Target}, Ownership: registry.Ownership{OwnerID: "recorded-participant-owner", AccountabilityID: "recorded-participant-owner"}, Lifecycle: registry.LifecycleEnabled, Charter: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: c.AgentID, Revision: c.Revision, Digest: canonical.Digest}})
	if err != nil {
		t.Fatal(err)
	}
	repo := &hostApprovalRepository{initial: a, latest: a, registration: registry.AgentRegistration{SchemaVersion: registry.AgentRegistrationSchemaVersion, AgentID: a.AgentID, Source: a.Source, InitialRevision: agentRevisionRef(a)}}
	s.FleetRepository = repo
	text := "hello"
	d, err := s.SaveDoerDraftAs(ctx, sub, DoerDraftInput{Agent: agentRevisionRef(a), LoopID: "controller-selected-file", Revision: 1, Contract: loop.DoerContract{Task: "Write the exact selected file", Workspace: t.TempDir(), WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", ExpectedText: &text, ExactBytes: true, MaxAttempts: 2}})
	if err != nil {
		t.Fatal(err)
	}
	return s, sub, d, repo
}

func TestIssue351ControllerReviewedParticipantHost(t *testing.T) {
	s, sub, draft, repo := controllerParticipantFixture(t)
	ctx := context.Background()
	candidate, _, err := loop.NewDoerRevision(draft.LoopID, draft.Revision, draft.PreviousDigest, draft.Contract)
	if err != nil {
		t.Fatal(err)
	}
	if s.doerEphemeralPreparationAllowed(ctx, sub, candidate, repo.latest) {
		t.Fatal("unreviewed foreign owner prepared")
	}
	digest, _ := draft.Contract.Digest()
	approved, err := s.ApproveDoerHostWriteAs(ctx, sub, DoerHostApprovalInput{DraftID: draft.ID, DraftVersion: draft.Version, ExpectedCandidateDigest: candidate.Digest, ExpectedContractDigest: digest, Decision: "approve-host-write"})
	if err != nil {
		t.Fatalf("independent exact controller review denied recorded participant: %v", err)
	}
	if approved.OwnerID != repo.latest.Ownership.OwnerID || approved.PrincipalID != sub.PrincipalID {
		t.Fatal("ownership rewritten")
	}
	got, err := s.ReadDoerHostWriteApprovalAs(ctx, sub, draft.Agent, draft.Contract)
	if err != nil || got.SignedDigest() != approved.SignedDigest() {
		t.Fatalf("recorded-owner readback: %v", err)
	}
	if s.doerEphemeralPreparationAllowed(ctx, sub, candidate, repo.latest) {
		t.Fatal("host review bypassed separate provisioning")
	}
	review, err := s.PreviewPlanAs(ctx, sub, repo.latest.Charter.ID, repo.latest.Charter.Revision, core.Environment{Name: "local"})
	if err != nil {
		t.Fatal(err)
	}
	approval, err := s.RequestApprovalAs(ctx, sub, review.Plan.ID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DecideApprovalAs(ctx, sub, approval.ID, true); err != nil {
		t.Fatal(err)
	}
	receipt, err := s.ApplyAs(ctx, sub, review.Plan.ID, approval.ID)
	if err != nil || receipt.Status != "verified" {
		t.Fatal("isolated exact provisioning failed", err)
	}
	if !s.doerEphemeralPreparationAllowed(ctx, sub, candidate, repo.latest) {
		t.Fatal("separately provisioned and reviewed participant cannot prepare")
	}
	if err := s.resolveDoerHostApproval(ctx, draft.Contract, repo.latest); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ValidateDoerDraftPublicationAs(ctx, sub, draft.ID, draft.Version); err == nil {
		t.Fatal("execution review granted definition mutation")
	}
	if _, err := os.Stat(filepath.Join(draft.Contract.Workspace, "result.txt")); !os.IsNotExist(err) {
		t.Fatal("host review executed output", err)
	}
}
