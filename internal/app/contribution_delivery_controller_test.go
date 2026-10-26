package app

import (
	"context"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/credentials"
	"github.com/berryhill/aegis/internal/delivery"
	"testing"
)

func TestTrustedDeliveryControllerMissingComposition(t *testing.T) {
	for _, p := range []*ContributionDeliveryServerPolicy{nil, {Convention: "contribution-publication.v1"}} {
		if c, e := NewContributionDeliveryController(&Service{}, p, nil, nil); e == nil || c != nil {
			t.Fatal("missing credential/policy/privacy accepted")
		}
		if c, e := NewContributionDeliveryController(&Service{}, p, &credentials.Authority{}, nil); e == nil || c != nil {
			t.Fatal("missing binding/privacy accepted")
		}
	}
	var c *ContributionDeliveryController
	if _, e := c.RunAs(context.Background(), core.Subject{}, "key", "session"); e == nil {
		t.Fatal("nil controller accepted")
	}
	a := &productiveContributionAuthority{}
	// Productive CheckDelivery must not fall back to historical admission after
	// callback lifetime, independently of mutation flag; no Service access occurs.
	for _, writing := range []bool{false, true} {
		if a.Admit(context.Background(), delivery.Intent{Operation: delivery.CheckDelivery}, writing) == nil {
			t.Fatal("inactive productive authority accepted")
		}
	}
	s := &heldContributionDeliverySource{plan: delivery.Plan{Reference: delivery.Reference{Run: "run"}}}
	calls := 0
	fn := func(context.Context, delivery.Plan) error { calls++; return nil }
	if s.WithAccepted(context.Background(), s.plan.Reference, fn) != nil || s.WithAccepted(context.Background(), s.plan.Reference, fn) == nil || calls != 1 {
		t.Fatal("source reused")
	}
}
