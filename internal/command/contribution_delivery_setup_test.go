package command

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/credentials"
	"github.com/berryhill/aegis/internal/delivery"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	canonicalstore "github.com/berryhill/aegis/internal/store"
)

func TestContributionDeliveryLocalStartupComposition(t *testing.T) {
	ctx := context.Background()
	store, err := fleetbadger.Open(ctx, filepath.Join(t.TempDir(), "fleet-v1"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	blobs, err := canonicalstore.Open(filepath.Join(t.TempDir(), "canonical"))
	if err != nil {
		t.Fatal(err)
	}
	s := &app.Service{FleetRepository: store, Store: blobs}
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
	historyForCustody, err := app.NewContributionDeliveryPolicyHistory(s)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := app.NewContributionDeliveryColdCustody(s, historyForCustody, &credentials.Authority{}, fresh)
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(remote.Use(ctx, delivery.Intent{}, func([]byte) error { t.Fatal("unpermitted secret use"); return nil }), delivery.ErrDenied) {
		t.Fatal("cold direct use accepted")
	}
	history, err := app.NewContributionDeliveryPolicyHistory(s)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = history.ResolveContributionDeliveryPolicyDigest(ctx, "any"); !errors.Is(err, delivery.ErrDenied) {
		t.Fatal("historical policy fallback")
	}
}
