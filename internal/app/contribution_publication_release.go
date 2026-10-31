package app

import (
	"context"
	"errors"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"github.com/berryhill/aegis/internal/runtime/hermes"
)

// releaseTerminalPublication is safety cleanup, not renewed execution authority.
// A terminal record alone never releases custody: all owning publication calls
// must have returned and the exact controller process must acknowledge Stop.
func (s *Service) releaseTerminalPublication(ctx context.Context, sub core.Subject, key string) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	ctx = cleanup
	c, _, err := s.ReadContributionAs(ctx, sub, key)
	if err != nil {
		return err
	}
	if _, err = s.FleetRepository.GetDispositionByGraphRun(ctx, c.RunID); errors.Is(err, fleet.ErrNotFound) {
		return nil
	} else if err != nil {
		return err
	}
	if owner, ok := s.ContributionPublicationQuiescer.(*ContributionDeliveryPublicationOwner); ok {
		if owner.service != s {
			return delivery.ErrDenied
		}
		owner.mu.Lock()
		active := owner.active[key]
		owner.mu.Unlock()
		if active != 0 {
			return nil
		} // The last exact owning leave performs cleanup.
	}
	_, state, _, err := s.ObserveContributionContinuityAs(cleanup, sub, key)
	if err != nil {
		return err
	}
	_, err = s.stopContributionRole(cleanup, c, state, "controller", ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: state.Sequence, ExpectedDigest: state.Digest})
	if errors.Is(err, hermes.ErrContributionStopUnavailable) && s.QueueWorker != nil {
		err = s.QueueWorker.WithTerminalPublicationRecovery(cleanup, sub, c.QueueItemID, func(locked context.Context) error {
			_, current, _, e := s.ObserveContributionContinuityAs(locked, sub, key)
			if e != nil {
				return e
			}
			_, e = s.stopContributionRoleMode(locked, c, current, "controller", ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: current.Sequence, ExpectedDigest: current.Digest}, true)
			return e
		})
	}
	if err != nil {
		return err
	}
	store, ok := s.FleetRepository.(*fleetbadger.Store)
	if !ok {
		return ErrContributionDeliveryUnavailable
	}
	return store.ReleaseContributionPublication(cleanup, c.OwnerID, c.Key)
}

func (s *Service) preparePublicationHandoff(ctx context.Context, sub core.Subject, binding fleetbadger.ContributionDeliveryBinding) error {
	store, ok := s.FleetRepository.(*fleetbadger.Store)
	if !ok {
		return ErrContributionDeliveryUnavailable
	}
	c, _, err := s.ReadContributionAs(ctx, sub, binding.Key)
	if err != nil {
		return err
	}
	root, err := store.GetContributionPublicationOwner(ctx, c.Source.RepositoryID, binding.Base)
	if errors.Is(err, fleet.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if root.OwnerID != sub.PrincipalID {
		return delivery.ErrConflict
	}
	if root.Run == binding.Run {
		return s.releaseTerminalPublication(ctx, sub, binding.Key)
	}
	return s.releaseTerminalPublication(ctx, sub, root.Key)
}
