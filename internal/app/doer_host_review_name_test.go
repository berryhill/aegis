package app

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/loop"
)

func TestNamedDraftHostReviewPinsSameCandidate(t *testing.T) {
	s, sub, input := draftFixture(t)
	input.Name = "Review selected output"
	d, err := s.SaveDoerDraftAs(context.Background(), sub, input)
	if err != nil {
		t.Fatal(err)
	}
	named, _, err := d.Candidate()
	if err != nil {
		t.Fatal(err)
	}
	reviewed, err := s.doerHostReviewCandidate(context.Background(), sub, d)
	if err != nil || !reflect.DeepEqual(reviewed, named) {
		t.Fatalf("host review lost exact retained candidate: got %+v, want %+v, err %v", reviewed, named, err)
	}
}

// Synthetic controller participant authority only; no runtime or host writes.
func TestNamedDraftHostApprovalReadbackAndContinuation(t *testing.T) {
	s, sub, d, repo := controllerParticipantFixture(t)
	ctx := context.Background()
	input := DoerDraftInput{ID: d.ID, ExpectedVersion: d.Version, Name: "Review selected output", Agent: d.Agent, LoopID: d.LoopID, Revision: d.Revision, PreviousDigest: d.PreviousDigest, Contract: d.Contract}
	d, err := s.SaveDoerDraftAs(ctx, sub, input)
	if err != nil {
		t.Fatal(err)
	}
	named, _, err := d.Candidate()
	if err != nil {
		t.Fatal(err)
	}
	prepared, agent, err := s.PrepareDoerHostReviewAs(ctx, sub, d.ID, d.Version)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := d.Contract.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if prepared.ExpectedCandidateDigest != named.Digest || prepared.ExpectedContractDigest != digest || agentRevisionRef(agent) != d.Agent {
		t.Fatalf("public preflight did not pin named candidate: %+v", prepared)
	}
	unnamed, _, err := loop.NewDoerRevision(d.LoopID, d.Revision, d.PreviousDigest, d.Contract)
	if err != nil {
		t.Fatal(err)
	}
	wrong := prepared
	wrong.ExpectedCandidateDigest = unnamed.Digest
	if _, err := s.ApproveDoerHostWriteAs(ctx, sub, wrong); !errors.Is(err, ErrConflict) {
		t.Fatalf("unnamed digest accepted: %v", err)
	}
	approved, err := s.ApproveDoerHostWriteAs(ctx, sub, prepared)
	if err != nil {
		t.Fatalf("exact named approval: %v", err)
	}
	if approved.Candidate != loopRevisionRef(named) || approved.CandidateName != d.Name || approved.OwnerID != repo.latest.Ownership.OwnerID || approved.PrincipalID != sub.PrincipalID {
		t.Fatalf("signed identity/ownership changed: %+v", approved)
	}
	got, err := s.ReadDoerHostWriteApprovalAs(ctx, sub, d.Agent, d.Contract)
	if err != nil || !reflect.DeepEqual(got, approved) {
		t.Fatalf("signed custody readback: %v", err)
	}
	input.ExpectedVersion = d.Version
	retained, err := s.SaveDoerDraftAs(ctx, sub, input)
	if err != nil || !reflect.DeepEqual(retained, d) {
		t.Fatalf("unchanged return invalidated review: %v", err)
	}
	if s.doerEphemeralPreparationAllowed(ctx, sub, named, repo.latest) {
		t.Fatal("host consent granted separate provisioning")
	}

	// The browser continuation keeps the very same named digest after returning
	// from independent host review. No native continuation is signed by this test.
	requester := sub
	requester.Kind, requester.Issuer, requester.Method = "principal", "aegis-principal-auth", "password"
	requester.ExpiresAt = s.Now().Add(s.Config.Principal.AuthTTL)
	intent := DoerContinuationIntent{DraftID: d.ID, DraftVersion: d.Version, CandidateDigest: named.Digest, Agent: d.Agent, Loop: loopRevisionRef(named), Charter: agent.Charter, PublicationKey: d.PublicationKey, Requester: requester, SessionID: "named-review-session", DeploymentID: s.Config.Credentials.Authority.DeploymentID, ConfigIdentity: s.DoerContinuationConfigIdentity(), ExpiresAt: s.Now().Add(time.Minute)}
	intent.Run = QueueLoopInput{Agent: intent.Agent, Loop: intent.Loop, IdempotencyKey: "named-review-run"}
	if err := s.validateDoerContinuationIntent(ctx, requester, intent.SessionID, intent); err != nil {
		t.Fatalf("named continuation denied: %v", err)
	}
	stale := intent
	stale.CandidateDigest = unnamed.Digest
	if err := s.validateDoerContinuationIntent(ctx, requester, intent.SessionID, stale); !errors.Is(err, ErrConflict) {
		t.Fatalf("unnamed continuation accepted: %v", err)
	}
	input.Name = "Renamed review"
	if _, err := s.SaveDoerDraftAs(ctx, sub, input); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.PrepareDoerHostReviewAs(ctx, sub, d.ID, d.Version); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale review version accepted: %v", err)
	}
	if _, err := s.ApproveDoerHostWriteAs(ctx, sub, prepared); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale named approval accepted: %v", err)
	}
	if err := s.validateDoerContinuationIntent(ctx, requester, intent.SessionID, intent); !errors.Is(err, ErrConflict) {
		t.Fatalf("renamed continuation accepted: %v", err)
	}
	got, err = s.ReadDoerHostWriteApprovalAs(ctx, sub, d.Agent, d.Contract)
	if err != nil || !reflect.DeepEqual(got, approved) {
		t.Fatalf("rename rewrote historical consent: %v", err)
	}
}
