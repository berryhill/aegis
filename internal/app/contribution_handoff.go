package app

import (
	"context"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/orchestration"
)

// AcceptedContributionCandidateAs is the authenticated downstream service
// handoff. Resolve current sealed intake and exact immutable Loop server-side;
// never accept caller-supplied policy, candidate or GitHub effect authority.
func (s *Service) AcceptedContributionCandidateAs(ctx context.Context, subject core.Subject, key string) (orchestration.ContributionCandidate, error) {
	c, _, err := s.ReadContributionAs(ctx, subject, key)
	if err != nil {
		return orchestration.ContributionCandidate{}, err
	}
	if s.QueueWorker == nil {
		return orchestration.ContributionCandidate{}, ErrDenied
	}
	revision, err := s.FleetRepository.GetLoopRevision(ctx, c.Loop.ID, c.Loop.Revision)
	if err != nil || revision.Digest != c.Loop.Digest {
		return orchestration.ContributionCandidate{}, ErrDenied
	}
	return s.QueueWorker.AcceptedContributionCandidate(ctx, subject, c.QueueItemID, revision)
}
