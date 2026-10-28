package app

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
)

// ResolveContributionNavigationAs includes canonical terminal work for retained
// result navigation. It grants no execution/resume authority from a projection.
func (s *Service) ResolveContributionNavigationAs(ctx context.Context, sub core.Subject, ref string) (contribution.Contract, error) {
	if err := s.RequirePrincipalIdentity(sub); err != nil {
		return contribution.Contract{}, err
	}
	repo, ok := s.FleetRepository.(fleet.ContributionRepository)
	if !ok {
		return contribution.Contract{}, ErrDenied
	}
	issue := ""
	if ref != "" && !contribution.ValidKey(ref) {
		parsed, err := contribution.ParseIssueURL(ref)
		if err != nil {
			return contribution.Contract{}, ErrDenied
		}
		issue = parsed.URL
	}
	items, err := s.FleetRepository.ListQueueItems(ctx)
	if err != nil {
		return contribution.Contract{}, err
	}
	var selected contribution.Contract
	for _, item := range items {
		c, err := repo.GetContributionForQueue(ctx, item.ItemID)
		if errors.Is(err, fleet.ErrNotFound) {
			continue
		}
		if err != nil {
			return c, err
		}
		if c.OwnerID != sub.PrincipalID || (ref != "" && issue == "" && c.Key != ref) || (issue != "" && c.Source.URL.URL != issue) {
			continue
		}
		if c.QueueItemID != item.ItemID || c.RunID != item.GraphRunID || c.SubmissionID != item.Submission.ID {
			return c, fleet.ErrCorrupt
		}
		submission, err := s.FleetRepository.GetSubmission(ctx, c.SubmissionID)
		if err != nil {
			return c, err
		}
		if submission.OwnerID != sub.PrincipalID {
			return c, ErrDenied
		}
		projection, err := s.FleetRepository.GetQueueProjection(ctx, c.QueueItemID)
		if err != nil {
			return c, err
		}
		revision, err := s.FleetRepository.GetLoopRevision(ctx, c.Loop.ID, c.Loop.Revision)
		if err != nil {
			return c, err
		}
		if revision.Digest != c.Loop.Digest {
			return c, ErrDenied
		}
		// Only final-policy definitions extend navigation to terminal work.
		// Legacy stop/exhaustion retains its existing non-rediscovery contract;
		// it must not become a new execution or a synthetic final result.
		if (revision.Contribution == nil || revision.Contribution.FinalVerification == "") && projection.State != queue.StateQueued && projection.State != queue.StateClaimed && projection.State != queue.StatePreparationPending {
			continue
		}
		if selected.Key != "" {
			return c, fleet.ErrConflict
		}
		selected = c
	}
	if selected.Key == "" {
		return selected, fleet.ErrNotFound
	}
	return selected, nil
}
