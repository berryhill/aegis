package command

import (
	"context"
	"errors"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/credentials"
	"github.com/berryhill/aegis/internal/delivery"
)

// ErrContributionRemotePrivacyUnavailable describes the retained cold fallback,
// not the productive controller's real closed content guard.
var ErrContributionRemotePrivacyUnavailable = errors.New("contribution_remote_privacy_unavailable")
var ErrContributionRemoteCustodyUnavailable = errors.New("contribution_remote_cold_custody_unavailable")
var ErrContributionHistoricalPolicyUnavailable = errors.New("contribution_original_historical_policy_unavailable")

type unavailableContributionRemote struct{}

func (unavailableContributionRemote) Review(context.Context, delivery.Intent, delivery.Plan) error {
	return ErrContributionRemotePrivacyUnavailable
}
func (unavailableContributionRemote) Use(context.Context, delivery.Intent, func([]byte) error) error {
	return ErrContributionRemoteCustodyUnavailable
}
func (unavailableContributionRemote) ResolveContributionDeliveryPolicyDigest(context.Context, string) (app.ContributionDeliveryPolicy, error) {
	// Canonical effect history retains the digest, not the complete original policy.
	// Current startup policy must NEVER be used to retcon that missing history.
	return app.ContributionDeliveryPolicy{}, ErrContributionHistoricalPolicyUnavailable
}

// configureContributionDelivery is called after Fleet and credentials composition.
// fresh must extract the current authenticated request's existing subject; it must
// not call Authenticate, extend TTLs or capture one startup subject. Nil configuration
// is deliberately disabled. The current bridge supports LOCAL delivery only.
func configureContributionDelivery(ctx context.Context, s *app.Service, p *config.ContributionDelivery, repository credentials.Repository, fresh func(context.Context) (core.Subject, error)) error {
	if p == nil {
		return nil
	}
	if err := p.Validate(); err != nil {
		return err
	}
	if s == nil || fresh == nil {
		return app.ErrContributionDeliveryUnavailable
	}
	policy := &app.ContributionDeliveryServerPolicy{Convention: p.Convention, Actor: p.Actor, Author: delivery.Identity{Name: p.Author.Name, Email: p.Author.Email}, Committer: delivery.Identity{Name: p.Committer.Name, Email: p.Committer.Email}, ProtectionPolicy: p.ProtectionPolicy, RequiredChecks: p.RequiredChecks, RequiredReviewers: p.RequiredReviewers, PostMergeChecks: p.PostMergeChecks, MergeMethod: p.MergeMethod}
	authority := s.CredentialAuthority
	if p.CredentialBinding != nil {
		if repository == nil || authority == nil {
			return app.ErrContributionDeliveryUnavailable
		}
		resolved, err := repository.Resolve(ctx, p.CredentialBinding.Key())
		if err != nil {
			return err
		}
		if resolved.Binding.Key != p.CredentialBinding.Key() || credentials.ValidateBinding(resolved.Binding, p.CredentialBinding.DeploymentID) != nil || !resolved.Binding.Enabled {
			return delivery.ErrDenied
		}
		policy.Binding = resolved.Binding
	}
	// Empty local container is never consulted by Commit and cannot decrypt/use a
	// remote secret. Do not require installing credential custody to build locally.
	if authority == nil {
		authority = &credentials.Authority{}
	}
	remote := unavailableContributionRemote{}
	privacy := delivery.PublicationPrivacy{}
	github, err := delivery.NewGitHub("https://api.github.com", remote, privacy)
	if err != nil {
		return err
	}
	if _, err = app.NewContributionDeliveryController(s, policy, authority, privacy); err != nil {
		return err
	}
	if _, err = app.InstallContributionDeliveryPublicationOwner(s, remote, github, fresh); err != nil {
		return err
	}
	_, err = app.InstallContributionDeliveryController(s, policy, authority, privacy)
	return err
}
