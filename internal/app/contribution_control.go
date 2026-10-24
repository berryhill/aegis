package app

import (
	"context"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/persistence/fleet"
)

func (s *Service) ReadContributionControlAs(ctx context.Context, subject core.Subject, key string) (contribution.Control, error) {
	c, _, err := s.ReadContributionAs(ctx, subject, key)
	if err != nil {
		return contribution.Control{}, err
	}
	repo, ok := s.FleetRepository.(fleet.ContributionRepository)
	if !ok {
		return contribution.Control{}, ErrDenied
	}
	return repo.GetContributionControl(ctx, c.QueueItemID)
}

func (s *Service) ControlContributionAs(ctx context.Context, subject core.Subject, session, key string, stop bool, delivery string) error {
	c, _, e := s.ReadContributionAs(ctx, subject, key)
	if e != nil {
		return e
	}
	repo, ok := s.FleetRepository.(fleet.ContributionRepository)
	if !ok || session == "" {
		return ErrDenied
	}
	prior, e := repo.GetContributionControl(ctx, c.QueueItemID)
	if e != nil {
		return e
	}
	if prior.Stop && stop {
		return nil
	}
	base := c.Effect
	if prior.Sequence > 0 {
		base = prior.Delivery
	}
	target, e := contribution.RestrictDelivery(base, delivery)
	if e != nil {
		return e
	}
	return repo.AppendContributionControl(ctx, contribution.Control{QueueItemID: c.QueueItemID, ContractDigest: c.Digest, OwnerID: subject.PrincipalID, SessionID: session, Sequence: prior.Sequence + 1, PreviousDigest: prior.Digest, Stop: stop, Delivery: target, At: s.Now()})
}
