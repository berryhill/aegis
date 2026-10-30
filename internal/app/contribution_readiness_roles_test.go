package app

import (
	"context"
	"testing"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"github.com/berryhill/aegis/internal/registry"
)

type contributionReadinessRoleRepository struct {
	*fleetbadger.Store
	change func(registry.AgentRevision) (registry.AgentRevision, error)
}

func (r contributionReadinessRoleRepository) LatestAgentRevision(ctx context.Context, id string) (registry.AgentRevision, error) {
	a, err := r.Store.LatestAgentRevision(ctx, id)
	if err != nil || id != "contribution-builder" {
		return a, err
	}
	return r.change(a)
}

func TestContributionReadinessExactRoleDenials(t *testing.T) {
	for _, name := range []string{"missing", "foreign", "changed"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s, repo, sub, ref, _ := contributionAuthorityFixture(t)
			s.FleetRepository = contributionReadinessRoleRepository{Store: repo, change: func(a registry.AgentRevision) (registry.AgentRevision, error) {
				switch name {
				case "missing":
					return registry.AgentRevision{}, fleet.ErrNotFound
				case "foreign":
					a.Ownership.OwnerID = "foreign-owner"
				case "changed":
					a.Revision++
				}
				return a, nil
			}}
			got, err := s.ReadContributionReadinessAs(ctx, sub, ContributionReadinessInput{Loop: ref})
			if err != nil || len(got.Roles) != 3 || got.Roles[1].Reference.Reason != "exact_owned_role_required" || got.Roles[1].Selection.State != "not_checked" || got.PreparationEligible {
				t.Fatalf("role denial: %+v %v", got, err)
			}
			items, err := repo.ListQueueItems(ctx)
			if err != nil || len(items) != 0 {
				t.Fatal("role observation admitted invocation", err)
			}
			authorities, err := s.Authority.ListAuthorityContexts(ctx)
			if err != nil || len(authorities) != 0 {
				t.Fatal("role observation issued authority", err)
			}
		})
	}
}

func TestContributionReadinessUnmatchedRoleDeniesWithoutEffects(t *testing.T) {
	ctx := context.Background()
	s, repo, sub, ref, _ := contributionAuthorityFixture(t, func(c *core.Charter) {
		if c.AgentID == "contribution-builder" {
			c.Stanzas[0].Authentication.Selectors[0].SubjectIDs = []string{"not-the-authenticated-owner"}
		}
	})
	got, err := s.ReadContributionReadinessAs(ctx, sub, ContributionReadinessInput{Loop: ref})
	if err != nil || len(got.Roles) != 3 || got.Roles[1].Selection.State != "blocked" || got.Roles[1].Provider.State != "not_checked" || got.PreparationEligible {
		t.Fatalf("unmatched role: %+v %v", got, err)
	}
	items, err := repo.ListQueueItems(ctx)
	if err != nil || len(items) != 0 {
		t.Fatal("unmatched observation admitted invocation", err)
	}
	authorities, err := s.Authority.ListAuthorityContexts(ctx)
	if err != nil || len(authorities) != 0 {
		t.Fatal("unmatched observation issued authority", err)
	}
}

func TestContributionRoleCharterRejectsAmbiguityBeforePublication(t *testing.T) {
	s := testService(t)
	c := testCharter(s.Now())
	c.AgentID = "contribution-builder"
	duplicate := c.Stanzas[0]
	duplicate.ID = "another-matching-context"
	c.Stanzas = append(c.Stanzas, duplicate)
	if _, err := core.Canonicalize(c); err == nil {
		t.Fatal("ambiguous contribution role charter admitted to canonical publication")
	}
}
