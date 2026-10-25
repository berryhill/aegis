package app

import (
	"context"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/persistence/fleet"
)

// Called by the real worker only after exact profile/host/readiness preflight,
// immediately before the canonical initial claim transaction.
func (s *Service) reserveContributionInitialAttempt(ctx context.Context, sub core.Subject, c contribution.Contract, attempt string) error {
	if sub.PrincipalID != c.OwnerID || s.requireFleetPrincipal(sub) != nil || attempt == "" {
		return ErrDenied
	}
	repo, ok := s.FleetRepository.(fleet.ContributionContinuityRepository)
	if !ok {
		return ErrDenied
	}
	appendFact := func(kind, counter, operation string, limits contribution.Counters) error {
		state, _, err := repo.GetContributionContinuity(ctx, c.QueueItemID)
		if err != nil {
			return err
		}
		e, err := contribution.SealContinuityEvent(contribution.ContinuityEvent{SchemaVersion: contribution.ContinuitySchema, QueueItemID: c.QueueItemID, ContractDigest: c.Digest, OwnerID: c.OwnerID, SessionID: c.SessionID, Sequence: state.Sequence + 1, PreviousDigest: state.Digest, Kind: kind, Counter: counter, OperationID: operation, Limits: limits, At: s.Now()})
		if err != nil {
			return err
		}
		_, err = repo.AppendContributionContinuity(ctx, e)
		return err
	}
	state, _, err := repo.GetContributionContinuity(ctx, c.QueueItemID)
	if err != nil {
		return err
	}
	if state.Sequence == 0 {
		if err = appendFact("initialize", "", "", contribution.Counters{Candidates: uint32(c.CandidateBudget), QueueAttempts: 1, RuntimeTurns: 128, InfrastructureRechecks: 2, DecisionDeliveries: 8}); err != nil {
			return err
		}
	}
	return appendFact("reserve", "queue-attempt", attempt, contribution.Counters{})
}
