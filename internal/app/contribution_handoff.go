package app

import (
	"context"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/orchestration"
)

// AcceptedContributionCandidateAs is authenticated diagnostic readback, not a
// source-custody lease or downstream-effect authorization. Resolve current sealed intake and exact immutable Loop server-side;
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

// WithAcceptedContributionCandidateAs resolves exact immutable intake server-side
// and holds source custody across action. The explicit gate must establish exact
// controller quiescence/epoch admission; nil is unavailable, never default allow.
// No publication, commit, stop or authority renewal is granted here.
func (s *Service) WithAcceptedContributionCandidateAs(ctx context.Context, subject core.Subject, key string, gate orchestration.ContributionQuiescenceGate, action func(context.Context, orchestration.ContributionCandidate) error) error {
	c, _, err := s.ReadContributionAs(ctx, subject, key)
	if err != nil {
		return err
	}
	if s.QueueWorker == nil {
		return ErrDenied
	}
	revision, err := s.FleetRepository.GetLoopRevision(ctx, c.Loop.ID, c.Loop.Revision)
	if err != nil || revision.Digest != c.Loop.Digest {
		return ErrDenied
	}
	return s.QueueWorker.WithAcceptedContributionCandidate(ctx, subject, c.QueueItemID, revision, gate, action)
}
