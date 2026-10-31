package app

import (
	"context"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/credentials"
	"github.com/berryhill/aegis/internal/delivery"
	"reflect"
	"sync/atomic"
)

type contributionHistoricalPermitKey struct{}
type contributionHistoricalPermit struct {
	owner   *ContributionDeliveryPublicationOwner
	subject core.Subject
	key     string
	intent  delivery.Intent
	active  atomic.Bool
}

// ContributionDeliveryColdCustody can consume credentials only inside the installed
// owner's synchronous historical reconciliation. It cannot authorize Apply.
type ContributionDeliveryColdCustody struct {
	service   *Service
	policies  *ContributionDeliveryPolicyHistory
	authority *credentials.Authority
	fresh     func(context.Context) (core.Subject, error)
}

func NewContributionDeliveryColdCustody(s *Service, h *ContributionDeliveryPolicyHistory, a *credentials.Authority, fresh func(context.Context) (core.Subject, error)) (*ContributionDeliveryColdCustody, error) {
	if s == nil || h == nil || h.blobs != s.Store || a == nil || fresh == nil {
		return nil, ErrContributionDeliveryUnavailable
	}
	return &ContributionDeliveryColdCustody{s, h, a, fresh}, nil
}
func (c *ContributionDeliveryColdCustody) Use(ctx context.Context, i delivery.Intent, fn func([]byte) error) error {
	if c == nil || ctx == nil || delivery.CheckHistoricalReadMode(ctx, i) != nil || c.authority != c.service.CredentialAuthority || ctx.Err() != nil || fn == nil {
		return delivery.ErrDenied
	}
	switch i.Operation {
	case delivery.Publish, delivery.PullRequest, delivery.UpdatePullRequest, delivery.CheckDelivery, delivery.Merge, delivery.ReconcileIssue:
	default:
		return delivery.ErrDenied
	}
	permit, ok := ctx.Value(contributionHistoricalPermitKey{}).(*contributionHistoricalPermit)
	if !ok || permit == nil || !permit.active.Load() || permit.intent != i || permit.owner.service != c.service || c.service.ContributionPublicationQuiescer != permit.owner {
		return delivery.ErrDenied
	}
	authorize := func() error {
		if !permit.active.Load() || ctx.Err() != nil || delivery.CheckHistoricalReadMode(ctx, i) != nil {
			return delivery.ErrDenied
		}
		sub, err := c.fresh(ctx)
		if err != nil || !reflect.DeepEqual(sub, permit.subject) {
			return delivery.ErrDenied
		}
		return (contributionColdAuthority{c.service, sub, permit.key, i}).Admit(ctx, i, false)
	}
	if authorize() != nil {
		return delivery.ErrDenied
	}
	p, err := c.policies.ResolveContributionDeliveryPolicyDigest(ctx, i.Policy)
	if err != nil || p.Digest() != i.Policy || p.Binding.Reference != i.Reference || p.Binding.OwnerID != permit.subject.PrincipalID || p.Binding.Key != permit.key || p.Binding.Base != i.Base || p.Binding.RoleBinding != i.RoleBinding || p.Binding.FenceEpoch != i.FenceEpoch || p.Actor != i.Actor || p.Branch != i.Branch || p.CredentialPin == nil {
		return delivery.ErrDenied
	}
	// The canonical latest UNKNOWN must still be this entire original intent.
	h, err := c.service.ReadContributionDeliveryHistoryAs(ctx, permit.subject, permit.key)
	if err != nil || h.Contract.RunID != i.Run || h.Contract.Digest != i.Contract {
		return delivery.ErrDenied
	}
	found := false
	for _, chain := range h.Histories {
		if len(chain) > 0 {
			old := chain[len(chain)-1].Record
			if old.Intent == i && old.Outcome == delivery.Unknown {
				found = true
			}
		}
	}
	if !found {
		return delivery.ErrDenied
	}
	pin := p.CredentialPin
	if pin.Actor != i.Actor || pin.RepositoryID != i.RepositoryID {
		return delivery.ErrDenied
	}
	custody, err := delivery.NewCredentialCustody(c.authority, pin.Key, pin.Destination, pin.SecretRecord, pin.Version, pin.BindingRevision, h.Contract.Repository, pin.Actor, pin.RepositoryID, pin.Operations)
	if err != nil || authorize() != nil {
		return delivery.ErrDenied
	}
	return custody.Use(ctx, i, func(secret []byte) error {
		if authorize() != nil {
			return delivery.ErrDenied
		}
		return fn(secret)
	})
}
