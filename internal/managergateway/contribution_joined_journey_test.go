package managergateway

import (
	"context"
	"crypto/sha256"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/credentials"
	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/reference"
	"testing"
)

type managerJourneyDenyRemotePrivacy struct{}
type managerJourneyDenyRemoteCustody struct{}

func (managerJourneyDenyRemoteCustody) Use(context.Context, delivery.Intent, func([]byte) error) error {
	return delivery.ErrDenied
}

func (*managerJourneyDenyRemotePrivacy) Review(context.Context, delivery.Intent, delivery.Plan) error {
	return delivery.ErrDenied
}
func TestManagerJoinedContributionCompletion(t *testing.T) {
	retainedDeliveryNativeComposition(t, true)
}

// Controlled in-process manager/native fixture using #378's request boundary
// and installed controller. Actual HTTP transport and provider proof remain
// separate; no callback authenticates, renews or fabricates a terminal result.
func managerJoinedHeld(t *testing.T, ctx context.Context, s *app.Service, sub core.Subject, loop reference.RevisionRef, issue contribution.IssueURL) {
	t.Helper()
	policy := &app.ContributionDeliveryServerPolicy{Convention: "contribution-publication.v1", Actor: "fixture", Author: delivery.Identity{Name: "Fixture", Email: "fixture@example.invalid"}, Committer: delivery.Identity{Name: "Fixture", Email: "fixture@example.invalid"}, ProtectionPolicy: "reviewed-fixture", RequiredChecks: []string{"test"}, RequiredReviewers: []string{"reviewer"}, PostMergeChecks: []string{"test"}, MergeMethod: "squash"}
	history, err := app.NewContributionDeliveryPolicyHistory(s)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := delivery.NewGitHub("https://api.github.com", managerJourneyDenyRemoteCustody{}, &managerJourneyDenyRemotePrivacy{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = app.InstallContributionDeliveryPublicationOwner(s, history, remote, s.CurrentContributionDeliverySubject); err != nil {
		t.Fatal(err)
	}
	if _, err := app.InstallContributionDeliveryController(s, policy, &credentials.Authority{}, &managerJourneyDenyRemotePrivacy{}); err != nil {
		t.Fatal(err)
	}
	gateway, err := New(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	id, token := "joined-manager", "isolated-joined-token"
	gateway.sessions[id] = session{id: id, token: sha256.Sum256([]byte(token)), subject: sub, issuedAt: s.Now(), expires: sub.ExpiresAt, mode: "degraded", contributionLoop: loop}
	command := "/contribute run " + issue.URL + " local-only"
	first, err := gateway.Execute(ctx, sub, id, token, command)
	if err != nil {
		t.Fatal(err)
	}
	if first.Data["completed"] != true || first.Data["terminal"] != true || first.Data["whole_contribution_succeeded"] != false || first.Data["outcome"] != "narrower-target-completed" {
		t.Fatalf("actual controller final readback missing: %+v", first)
	}
	key := app.ContributionIntentKey(loop, issue.URL, "local-only")
	before, err := s.ReadContributionExecutionAs(ctx, sub, key)
	if err != nil || before.State != "candidate_accepted" {
		t.Fatal("real native producer not accepted", err)
	}
	if _, err = gateway.Execute(ctx, sub, id, token, command); err != nil {
		t.Fatal(err)
	}
	after, err := s.ReadContributionExecutionAs(ctx, sub, key)
	if err != nil || after.Consumed != before.Consumed || len(after.Passes) != len(before.Passes) {
		t.Fatal("duplicate run repeated native execution", err)
	}
	status, err := gateway.Execute(ctx, sub, id, token, "/contribute status "+key)
	if err != nil || status.Data["completed"] != true || status.Data["whole_contribution_succeeded"] != false {
		t.Fatal("narrower terminal status lost", err)
	}
	foreign := sub
	foreign.PrincipalID = "foreign"
	if _, err = gateway.Execute(ctx, foreign, id, token, "/contribute status "+key); err == nil {
		t.Fatal("foreign caller observed retained work")
	}
}
