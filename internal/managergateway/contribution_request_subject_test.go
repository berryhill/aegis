package managergateway

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/credentials"
	"github.com/berryhill/aegis/internal/delivery"
	"testing"
)

func TestContributionForgedManagerOpenCannotAuthenticateDelivery(t *testing.T) {
	s := contributionApp(t)
	sub, err := s.Authenticate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := New(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	// The trusted public transport entry accepts a Subject; that is not proof of
	// an independently authenticated caller. Reproduce the dangerous promotion.
	sub.Method = "unverifiable-transport"
	opened, err := gateway.Open(context.Background(), sub)
	if err != nil {
		t.Fatal(err)
	}
	if opened.ID == "" {
		t.Fatal("counterexample did not reach manager registry")
	}
	p := &app.ContributionDeliveryServerPolicy{Convention: "contribution-publication.v1", Actor: "fixture", Author: delivery.Identity{Name: "Fixture", Email: "fixture@example.invalid"}, Committer: delivery.Identity{Name: "Fixture", Email: "fixture@example.invalid"}, ProtectionPolicy: "fixture", RequiredChecks: []string{"ci"}, RequiredReviewers: []string{"reviewer"}, PostMergeChecks: []string{"post"}, MergeMethod: "squash"}
	controller, err := app.NewContributionDeliveryController(s, p, &credentials.Authority{}, delivery.PublicationPrivacy{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = controller.RunAs(context.Background(), sub, "fixture", "session"); !errors.Is(err, delivery.ErrDenied) {
		t.Fatalf("manager Open promoted asserted subject: %v", err)
	}
}
