package orchestration

import (
	"errors"
	"github.com/berryhill/aegis/internal/implementation"
)

// ContributionRecord is the controller-owned intermediate readback DTO.
type ContributionRecord = implementation.ContributionRecord

// ContributionCandidate is the immutable controller-owned downstream handoff.
type ContributionCandidate = implementation.ContributionCandidate

func IsContributionNotFound(err error) bool { return errors.Is(err, implementation.ErrNotFound) }

// ContributionCheckpoint and ContributionRoleIdentity preserve the implementation owner.
type ContributionCheckpoint = implementation.ContributionCheckpoint
type ContributionRoleIdentity = implementation.ContributionRoleIdentity
