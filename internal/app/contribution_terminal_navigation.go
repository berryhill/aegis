package app

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
)

type ContributionNavigationContract = contribution.Contract

// Error classification, not a producer verdict or an execution grant.
func IsContributionResultNotFound(err error) bool { return errors.Is(err, fleet.ErrNotFound) }

// GetFinalContributionResultAs is definition-aware terminal navigation. Legacy
// non-final profiles retain their old execution readback, never a promoted final
// success. Only exact not-found permits the manager to continue execution.
func (s *Service) GetFinalContributionResultAs(ctx context.Context, sub core.Subject, key string) (ContributionResult, error) {
	c, _, _, err := s.ObserveContributionContinuityAs(ctx, sub, key)
	if err != nil {
		return ContributionResult{}, err
	}
	revision, err := s.FleetRepository.GetLoopRevision(ctx, c.Loop.ID, c.Loop.Revision)
	if err != nil {
		return ContributionResult{}, err
	}
	if revision.Digest != c.Loop.Digest || revision.SchemaVersion != loop.ContributionRevisionSchemaVersion || revision.Contribution == nil {
		return ContributionResult{}, ErrDenied
	}
	if revision.Contribution.FinalVerification == "" {
		return ContributionResult{}, fleet.ErrNotFound
	}
	if revision.Contribution.FinalVerification != evidence.ContributionFinalPolicy {
		return ContributionResult{}, ErrDenied
	}
	return s.GetContributionResultAs(ctx, sub, key)
}
