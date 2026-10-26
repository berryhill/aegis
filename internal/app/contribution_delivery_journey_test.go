package app

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/credentials"
	"github.com/berryhill/aegis/internal/delivery"
	"sync"
	"testing"
)

type journeyPrivacy struct{}

func (*journeyPrivacy) Review(context.Context, delivery.Intent, delivery.Plan) error {
	return delivery.ErrDenied
}
func TestContributionDeliveryControllerInstall(t *testing.T) {
	p := &ContributionDeliveryServerPolicy{Convention: "contribution-publication.v1", Actor: "fixture", Author: delivery.Identity{Name: "Fixture", Email: "fixture@example.invalid"}, Committer: delivery.Identity{Name: "Fixture", Email: "fixture@example.invalid"}, ProtectionPolicy: "trusted", RequiredChecks: []string{"check"}, RequiredReviewers: []string{"review"}, PostMergeChecks: []string{"post"}, MergeMethod: "squash"}
	a, privacy := &credentials.Authority{}, &journeyPrivacy{}
	s := &Service{}
	if _, e := InstallContributionDeliveryController(s, p, a, privacy); !errors.Is(e, ErrContributionDeliveryControllerUnavailable) {
		t.Fatal(e)
	}
	owner := &ContributionDeliveryPublicationOwner{service: s}
	s.ContributionPublicationQuiescer = owner
	c, e := InstallContributionDeliveryController(s, p, a, privacy)
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, e := InstallContributionDeliveryController(s, p, a, privacy)
			if e != nil || got != c {
				t.Error("non-idempotent install", e)
			}
		}()
	}
	wg.Wait()
	p.RequiredChecks[0] = "changed"
	if c.policy.RequiredChecks[0] != "check" {
		t.Fatal("policy not snapshotted")
	}
	if _, e := InstallContributionDeliveryController(s, p, a, privacy); !errors.Is(e, ErrContributionDeliveryControllerConflict) {
		t.Fatal("conflicting policy", e)
	}
	p.RequiredChecks[0] = "check"
	if _, e := InstallContributionDeliveryController(s, p, &credentials.Authority{}, privacy); !errors.Is(e, ErrContributionDeliveryControllerConflict) {
		t.Fatal("authority replacement", e)
	}
	other := &Service{ContributionPublicationQuiescer: owner}
	if _, e := InstallContributionDeliveryController(other, p, a, privacy); !errors.Is(e, ErrContributionDeliveryControllerUnavailable) {
		t.Fatal("foreign owner", e)
	}
	if owner.controller != c {
		t.Fatal("controller changed")
	}
}
