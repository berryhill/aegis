package command

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/delivery"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
)

func TestContributionDeliveryLocalStartupComposition(t *testing.T) {
	ctx := context.Background()
	store, err := fleetbadger.Open(ctx, filepath.Join(t.TempDir(), "fleet-v1"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s := &app.Service{FleetRepository: store}
	if err = configureContributionDelivery(ctx, s, nil, nil, nil); err != nil || s.ContributionPublicationQuiescer != nil {
		t.Fatal("absence enabled delivery", err)
	}
	p := &config.ContributionDelivery{Convention: "contribution-publication.v1", Actor: "controller", Author: config.ContributionGitIdentity{Name: "Author", Email: "author@example.invalid"}, Committer: config.ContributionGitIdentity{Name: "Committer", Email: "committer@example.invalid"}, RequiredChecks: []string{"ci"}, RequiredReviewers: []string{"reviewer"}, PostMergeChecks: []string{"postmerge"}, MergeMethod: "squash", ProtectionPolicy: "exact-protection"}
	calls := 0
	fresh := func(context.Context) (core.Subject, error) {
		calls++
		return core.Subject{}, errors.New("no authenticated request")
	}
	if err = configureContributionDelivery(ctx, s, p, nil, fresh); err != nil {
		t.Fatal(err)
	}
	if s.ContributionPublicationQuiescer == nil || calls != 0 || s.CredentialAuthority != nil {
		t.Fatal("startup minted identity or required credential installation")
	}
	// Fresh request identity is obtained only when the existing quiesce port runs.
	if err = s.ContributionPublicationQuiescer.QuiesceContributionPublication(ctx, app.ContributionPublicationBinding{}); err == nil || calls != 1 {
		t.Fatal("missing dynamic request read", err, calls)
	}
	remote := unavailableContributionRemote{}
	if !errors.Is(remote.Review(ctx, delivery.Intent{}, delivery.Plan{}), ErrContributionRemotePrivacyUnavailable) {
		t.Fatal("remote privacy accepted")
	}
	if _, err = remote.ResolveContributionDeliveryPolicyDigest(ctx, "any"); !errors.Is(err, ErrContributionHistoricalPolicyUnavailable) {
		t.Fatal("historical policy fallback")
	}
}
