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
func TestManagerJoinedContributionHeld(t *testing.T) { retainedDeliveryNativeComposition(t, true) }

// Controlled manager/native hold fixture, not the production current-request
// authentication bridge or a complete contribution journey. Its cold-subject
// lookup denies rather than silently authenticating or renewing authority.
func managerJoinedHeld(t *testing.T, s *app.Service, sub core.Subject, loop reference.RevisionRef, issue contribution.IssueURL) {
	t.Helper()
	ctx := context.Background()
	policy := &app.ContributionDeliveryServerPolicy{Convention: "contribution-publication.v1", Actor: "fixture", Author: delivery.Identity{Name: "Fixture", Email: "fixture@example.invalid"}, Committer: delivery.Identity{Name: "Fixture", Email: "fixture@example.invalid"}, ProtectionPolicy: "reviewed-fixture", RequiredChecks: []string{"test"}, RequiredReviewers: []string{"reviewer"}, PostMergeChecks: []string{"test"}, MergeMethod: "squash"}
	history, err := app.NewContributionDeliveryPolicyHistory(s)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := delivery.NewGitHub("https://api.github.com", managerJourneyDenyRemoteCustody{}, &managerJourneyDenyRemotePrivacy{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = app.InstallContributionDeliveryPublicationOwner(s, history, remote, func(context.Context) (core.Subject, error) { return core.Subject{}, app.ErrDenied }); err != nil {
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
	if first.Data["completed"] != false || first.Data["held_stage"] != "disposition" || first.Data["held_reason"] != "completion_owner_unavailable_issue_open" || first.Data["delivery"] == nil {
		t.Fatalf("actual controller hold mislabeled: %+v", first)
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
	if err != nil || status.Data["completed"] != false {
		t.Fatal("held status became final success", err)
	}
	foreign := sub
	foreign.PrincipalID = "foreign"
	if _, err = gateway.Execute(ctx, foreign, id, token, "/contribute status "+key); err == nil {
		t.Fatal("foreign caller observed retained work")
	}
}
