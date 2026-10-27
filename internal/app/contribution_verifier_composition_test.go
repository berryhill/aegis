package app

import (
	"context"
	"errors"
	"testing"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/reference"
)

func TestContributionCompletionFrameRequiresActualLiveCustody(t *testing.T) {
	s, _, subject, loop, reader := contributionFixture(t)
	c, err := s.admitContributionAs(context.Background(), subject, "session", loop, reader.source.URL.URL, "frame-negative", "local-only", reader)
	if err != nil {
		t.Fatal(err)
	}
	ref := reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: "projected", Digest: contribution.Digest([]byte("not a retained policy"))}
	if _, err = s.NewContributionCompletionInFrame(context.Background(), subject, c.Key, ref); err == nil {
		t.Fatal("no private source frame accepted")
	}
	proof := &contributionCustodyProof{service: s, contract: c.Digest, candidate: "candidate", session: "session"}
	expired := context.WithValue(context.Background(), contributionCustodyKey{}, proof)
	if _, err = s.NewContributionCompletionInFrame(expired, subject, c.Key, ref); err == nil {
		t.Fatal("expired frame accepted")
	}
	a := &ContributionCompletionAdapter{verifier: &ContributionFinalVerifier{service: s}, frame: &contributionCompletionFrame{proof: proof}}
	if a.validateCompletionFrame(expired, subject) == nil {
		t.Fatal("cleared effect frame remained authority")
	}
}

func TestContributionFinalNavigationLegacyCompatibility(t *testing.T) {
	s, _, subject, loop, reader := contributionFixture(t)
	c, err := s.admitContributionAs(context.Background(), subject, "session", loop, reader.source.URL.URL, "navigation", "local-only", reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetFinalContributionResultAs(context.Background(), subject, c.Key); !errors.Is(err, fleet.ErrNotFound) {
		t.Fatal("legacy intake promoted or broken", err)
	}
	got, err := s.ResolveContributionNavigationAs(context.Background(), subject, c.Key)
	if err != nil || got.Digest != c.Digest {
		t.Fatal("exact retained navigation", err)
	}
	foreign := subject
	foreign.PrincipalID = "foreign"
	if _, err = s.ResolveContributionNavigationAs(context.Background(), foreign, c.Key); err == nil {
		t.Fatal("foreign navigation")
	}
}
