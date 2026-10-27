package app

import (
	"context"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/orchestration"
)

// ReadContributionCandidateEvidenceAs is an authenticated internal composition
// boundary for the contribution verifier. It accepts only the retained key;
// exact definitions, producer records and policy are resolved server-side.
// This is not a final disposition or a public source-content export.
func (s *Service) ReadContributionCandidateEvidenceAs(ctx context.Context, subject core.Subject, key string) (orchestration.ContributionCandidateEvidence, error) {
	c, _, err := s.ReadContributionAs(ctx, subject, key)
	if err != nil {
		return orchestration.ContributionCandidateEvidence{}, err
	}
	if s.QueueWorker == nil {
		return orchestration.ContributionCandidateEvidence{}, ErrDenied
	}
	revision, err := s.FleetRepository.GetLoopRevision(ctx, c.Loop.ID, c.Loop.Revision)
	if err != nil || revision.Digest != c.Loop.Digest {
		return orchestration.ContributionCandidateEvidence{}, ErrDenied
	}
	return s.QueueWorker.ReloadContributionCandidateEvidence(ctx, subject, c.QueueItemID, revision)
}
