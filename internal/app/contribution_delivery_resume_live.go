package app

import (
	"context"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/orchestration"
)

// A transport reattachment after native acceptance is not crash recovery when
// this same service still owns the exact admitted delivery controller. This is
// only status readback: later delivery independently reacquires source custody
// and repeats effect admission. No session, mandate, epoch or counter is minted.
func (s *Service) acceptedContributionControllerLive(ctx context.Context, sub core.Subject, c contribution.Contract, state contribution.Continuity, record orchestration.ContributionRecord) bool {
	if ctx == nil || ctx.Err() != nil || record.State != "candidate_accepted" || record.RunID != c.RunID || record.Contract != c.Digest || len(record.Passes) == 0 || state.ContractDigest != c.Digest || state.QueueItemID != c.QueueItemID {
		return false
	}
	last := record.Passes[len(record.Passes)-1]
	if last.Check.Outcome != "passed" || state.CandidateEvidence.Validate() != nil || last.Candidate.Digest != state.CandidateEvidence.Digest {
		return false
	}
	for _, effect := range state.NativeEffects {
		if effect.State != "completed" {
			return false
		}
	}
	for _, role := range []string{"builder", "tester"} {
		if !contribution.NativeRoleQuiescent(state, role) {
			return false
		}
	}
	controller, ok := state.RoleAuthorities["controller"]
	if !ok || controller.Epoch == 0 || state.RoleEpochs["controller"] != controller.Epoch {
		return false
	}
	admitted, err := s.CheckContributionRoleAs(ctx, sub, controller.SessionID, c.Key, "controller", ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: state.Sequence, ExpectedDigest: state.Digest})
	return err == nil && admitted == controller
}
