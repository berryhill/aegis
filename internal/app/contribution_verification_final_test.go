package app

import (
	"context"
	"testing"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/reference"
)

func TestFinalContributionOpaqueAuthorizationRejectsForgery(t *testing.T) {
	s, _, sub, _, _ := contributionFixture(t)
	p := ContributionDeliveryPolicy{}
	resolver := &reviewedDeliveryFixture{policy: p}
	ref := reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: "reviewed", Digest: p.Digest()}
	verifier, err := NewContributionFinalVerifier(s, ref, resolver)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range []*ContributionPreClosureAuthorization{nil, {}, {service: s, key: "key", owner: sub.PrincipalID}} {
		if verifier.RevalidatePreClosureAs(context.Background(), sub, "key", a) == nil {
			t.Fatal("forged closure authority accepted")
		}
	}
	if _, err := NewContributionFinalVerifier(s, reference.DigestRef{}, resolver); err == nil {
		t.Fatal("invalid policy accepted")
	}
}

func TestFinalContributionContinuityRequiresRealNativeJoin(t *testing.T) {
	s, _, subject, loop, reader := contributionFixture(t)
	c, err := s.admitContributionAs(context.Background(), subject, "session", loop, reader.source.URL.URL, "final-negative", "local-only", reader)
	if err != nil {
		t.Fatal(err)
	}
	event, err := contribution.SealContinuityEvent(contribution.ContinuityEvent{SchemaVersion: contribution.ContinuitySchema, QueueItemID: c.QueueItemID, ContractDigest: c.Digest, OwnerID: c.OwnerID, SessionID: c.SessionID, Sequence: 1, Kind: "initialize", Limits: contribution.ContinuityLimits{Candidates: 1, QueueAttempts: 1, RuntimeTurns: 8, DecisionDeliveries: 1}, At: c.AdmittedAt})
	if err != nil {
		t.Fatal(err)
	}
	state, err := contribution.ApplyContinuity(c, contribution.Continuity{}, event)
	if err != nil {
		t.Fatal(err)
	}
	if verifyFinalContinuity(c, state, []contribution.ContinuityEvent{event}) == nil {
		t.Fatal("initialization treated as native Wait evidence")
	}
	event.Digest = contribution.Digest([]byte("corrupt"))
	if verifyFinalContinuity(c, state, []contribution.ContinuityEvent{event}) == nil {
		t.Fatal("corrupt event accepted")
	}
	if verifyFinalContinuity(c, state, nil) == nil {
		t.Fatal("absent immutable history accepted")
	}
}
