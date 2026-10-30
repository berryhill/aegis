package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/reference"
)

func TestContributionReadinessObservesWithoutAllocating(t *testing.T) {
	ctx := context.Background()
	s, repo, sub, ref, reader := contributionFixture(t)
	before, err := s.Authority.ListAuthorityContexts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	absent, err := s.ReadContributionReadinessAs(ctx, sub, ContributionReadinessInput{})
	if err != nil || absent.Profile.Reason != "contribution_profile_absent" || absent.PreparationEligible || absent.Service.ExecutableDigest == "" {
		t.Fatalf("absent: %+v %v", absent, err)
	}
	missing := ref
	missing.ID = "missing-profile"
	absent, err = s.ReadContributionReadinessAs(ctx, sub, ContributionReadinessInput{Loop: missing})
	if err != nil || absent.Profile.Reason != "contribution_profile_absent" {
		t.Fatalf("missing: %+v %v", absent, err)
	}
	legacy, err := s.ReadContributionReadinessAs(ctx, sub, ContributionReadinessInput{Loop: ref})
	if err != nil || legacy.Profile.Reason != "intake_only_unqualified_profile" || len(legacy.Roles) != 3 || legacy.PreparationEligible {
		t.Fatalf("legacy: %+v %v", legacy, err)
	}
	for _, role := range legacy.Roles {
		if role.Reference.State != "ready" || role.Selection.Reason != "exact_charter_unavailable" {
			t.Fatalf("role: %+v", role)
		}
	}
	wrong := ref
	wrong.Digest = "sha256:" + strings.Repeat("d", 64)
	drift, err := s.ReadContributionReadinessAs(ctx, sub, ContributionReadinessInput{Loop: wrong})
	if err != nil || drift.Profile.Reason != "contribution_profile_digest_changed" {
		t.Fatalf("drift: %+v %v", drift, err)
	}
	image := CurrentContributionServiceIdentity()
	image.ExecutableDigest = "sha256:" + strings.Repeat("e", 64)
	mismatch, err := s.ReadContributionReadinessAs(ctx, sub, ContributionReadinessInput{Loop: ref, ExpectedService: &image})
	if err != nil || mismatch.Provenance.Reason != "candidate_service_mismatch" || mismatch.Profile.State != "not_checked" {
		t.Fatalf("mismatch: %+v %v", mismatch, err)
	}
	for _, actor := range []core.Subject{{}, func() core.Subject { v := sub; v.PrincipalID = "foreign"; return v }()} {
		if _, err = s.ReadContributionReadinessAs(ctx, actor, ContributionReadinessInput{Loop: ref}); !errors.Is(err, ErrDenied) {
			t.Fatalf("foreign readiness: %v", err)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = s.ReadContributionReadinessAs(cancelled, sub, ContributionReadinessInput{Loop: ref}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	after, err := s.Authority.ListAuthorityContexts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	items, err := repo.ListQueueItems(ctx)
	if err != nil || len(items) != 0 || len(before) != len(after) || reader.calls != 0 {
		t.Fatal("readiness allocated authority, Queue or source read", err)
	}
}

func TestContributionReadinessPreservesHistoricalProviderContract(t *testing.T) {
	ctx := context.Background()
	s, repo, sub, ref, _ := contributionAuthorityFixture(t)
	got, err := s.ReadContributionReadinessAs(ctx, sub, ContributionReadinessInput{Loop: ref})
	if err != nil || len(got.Roles) != 3 {
		t.Fatalf("readiness: %+v %v", got, err)
	}
	for _, role := range got.Roles {
		if role.Selection.State != "ready" || role.Provider.State != "configured" || role.Provider.Reason != "ordinary_provider_credential_admission_required" {
			t.Fatalf("historical provider reclassified: %+v", role)
		}
	}
	items, err := repo.ListQueueItems(ctx)
	if err != nil || len(items) != 0 {
		t.Fatal("provider observation created invocation", err)
	}
}

func TestContributionRunProvenanceRetainsExactImageAndDeniesDrift(t *testing.T) {
	ctx := context.Background()
	s, _, sub, ref, reader := contributionFixture(t)
	c, err := s.admitContributionAs(ctx, sub, "original", ref, reader.source.URL.URL, "image-intent", "", reader)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.ReadContributionRunProvenanceAs(ctx, sub, c.Key)
	if err != nil || p != nil {
		t.Fatal("historical intake invented execution image", err)
	}
	if err = s.retainContributionRunProvenance(c); err != nil {
		t.Fatal(err)
	}
	if err = s.retainContributionRunProvenance(c); err != nil {
		t.Fatal(err)
	}
	p, err = s.ReadContributionRunProvenanceAs(ctx, sub, c.Key)
	if err != nil || p == nil || p.ContractDigest != c.Digest || p.Loop != ref || p.Service != CurrentContributionServiceIdentity() {
		t.Fatalf("image: %+v %v", p, err)
	}
	saved := *p
	// Reopen/read the same durable record, never replace unknown execution evidence.
	if err = s.Store.Load("contribution-image", c.RunID, &saved); err != nil || saved != *p {
		t.Fatal(err)
	}
	saved.Service.SourceRevision = "different-candidate"
	if err = s.Store.Save("contribution-image", c.RunID, saved); err != nil {
		t.Fatal(err)
	}
	if err = s.retainContributionRunProvenance(c); !errors.Is(err, ErrConflict) {
		t.Fatalf("image drift admitted: %v", err)
	}
	changed := c
	changed.Loop = reference.RevisionRef{}
	if err = s.retainContributionRunProvenance(changed); !errors.Is(err, ErrConflict) {
		t.Fatal("contract drift admitted", err)
	}
	again, _, err := s.ReadContributionAs(ctx, sub, c.Key)
	if err != nil || again.Digest != c.Digest {
		t.Fatal("image check changed contract", err)
	}
}
