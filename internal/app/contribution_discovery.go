package app

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
)

// ResolveContributionAs discovers retained work through the existing Queue, not
// a session-local key or another index. Observation never starts a runtime.
func (s *Service) ResolveContributionAs(ctx context.Context, sub core.Subject, ref string) (contribution.Contract, error) {
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
			return contribution.Contract{}, err
		}
		if c.OwnerID != sub.PrincipalID || (ref != "" && issue == "" && c.Key != ref) || (issue != "" && c.Source.URL.URL != issue) {
			continue
		}
		if c.QueueItemID != item.ItemID || c.RunID != item.GraphRunID || c.SubmissionID != item.Submission.ID {
			return contribution.Contract{}, fleet.ErrCorrupt
		}
		submission, err := s.FleetRepository.GetSubmission(ctx, c.SubmissionID)
		if err != nil {
			return contribution.Contract{}, err
		}
		if submission.OwnerID != sub.PrincipalID {
			return contribution.Contract{}, ErrDenied
		}
		projection, err := s.FleetRepository.GetQueueProjection(ctx, c.QueueItemID)
		if err != nil {
			return contribution.Contract{}, err
		}
		if projection.State != queue.StateQueued && projection.State != queue.StateClaimed && projection.State != queue.StatePreparationPending {
			continue
		}
		revision, err := s.FleetRepository.GetLoopRevision(ctx, c.Loop.ID, c.Loop.Revision)
		if err != nil || revision.Digest != c.Loop.Digest {
			return contribution.Contract{}, ErrDenied
		}
		if selected.Key != "" {
			return contribution.Contract{}, fleet.ErrConflict
		}
		selected = c
	}
	if selected.Key == "" {
		return selected, ErrDenied
	}
	return selected, nil
}
