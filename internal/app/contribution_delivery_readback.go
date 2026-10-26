package app

import (
	"context"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/delivery"
)

type contributionDeliveryHistoryReader interface {
	GetContributionDeliveryHistory(context.Context, string, string) (delivery.HistorySnapshot, error)
}

// ReadContributionDeliveryHistoryAs needs fresh same-principal read admission,
// never a historical runtime mandate or caller-selected intent/record list.
func (s *Service) ReadContributionDeliveryHistoryAs(ctx context.Context, subject core.Subject, key string) (delivery.HistorySnapshot, error) {
	c, _, err := s.ReadContributionAs(ctx, subject, key)
	if err != nil {
		return delivery.HistorySnapshot{}, err
	}
	if c.OwnerID != subject.PrincipalID {
		return delivery.HistorySnapshot{}, ErrDenied
	}
	owner, ok := s.FleetRepository.(contributionDeliveryHistoryReader)
	if !ok {
		return delivery.HistorySnapshot{}, ErrContributionDeliveryUnavailable
	}
	out, err := owner.GetContributionDeliveryHistory(ctx, subject.PrincipalID, c.Key)
	if err != nil {
		return delivery.HistorySnapshot{}, err
	}
	if delivery.ValidateHistorySnapshot(out) != nil || out.Contract.OwnerID != subject.PrincipalID || out.Contract.Key != c.Key || out.Contract.QueueItemID != c.QueueItemID || out.Contract.Digest != c.Digest || out.RootRun != c.RunID || out.Root.OwnerID != subject.PrincipalID || out.Root.Contract != c.Digest || out.Root.Run != c.RunID {
		return delivery.HistorySnapshot{}, ErrDenied
	}
	return out, nil
}
