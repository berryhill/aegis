package app

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestContributionRoleAuthorityServiceDeniesLabelsAndStaleOwner(t *testing.T) {
	ctx := context.Background()
	s, repo, sub, ref, reader := contributionFixture(t)
	now := s.Now()
	s.Now = func() time.Time { return now }
	c, err := s.admitContributionAs(ctx, sub, "original", ref, reader.source.URL.URL, "roles", "local-only", reader)
	if err != nil {
		t.Fatal(err)
	}
	state, err := s.RecordContributionContinuityAs(ctx, sub, "original", c.Key, ContributionContinuityInput{ContractDigest: c.Digest, Kind: "initialize"})
	if err != nil {
		t.Fatal(err)
	}
	in := ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: state.Sequence, ExpectedDigest: state.Digest}
	if _, err = s.AttachContributionObservationAs(ctx, sub, "original", c.Key, in); !errors.Is(err, ErrDenied) {
		t.Fatalf("label impersonated session %v", err)
	}
	for _, role := range []string{"controller", "builder", "tester", "arbitrary"} {
		if _, err = s.AdmitContributionRoleAs(ctx, sub, "original", c.Key, role, in); !errors.Is(err, ErrDenied) {
			t.Fatalf("%s inherited root %v", role, err)
		}
		if _, err = s.CheckContributionRoleAs(ctx, sub, "original", c.Key, role, in); !errors.Is(err, ErrDenied) {
			t.Fatalf("%s checker accepted absent authority %v", role, err)
		}
	}
	foreign := sub
	foreign.PrincipalID = "other"
	if _, err = s.AdmitContributionRoleAs(ctx, foreign, "new", c.Key, "builder", in); err == nil {
		t.Fatal("owner transfer")
	}
	stale := sub
	stale.ExpiresAt = now
	if _, err = s.AdmitContributionRoleAs(ctx, stale, "new", c.Key, "builder", in); err == nil {
		t.Fatal("stale principal")
	}
	got, events, err := repo.GetContributionContinuity(ctx, c.QueueItemID)
	if err != nil || got.Digest != state.Digest || len(events) != 1 || len(got.RoleAuthorities) != 0 {
		t.Fatal("denial mutated lineage", err)
	}
	if _, err = repo.GetQueueRuntimeBinding(ctx, c.QueueItemID); err == nil {
		t.Fatal("role denial manufactured root binding")
	}
}
