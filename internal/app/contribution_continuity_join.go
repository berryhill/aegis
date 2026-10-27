package app

import (
	"context"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/orchestration"
	"sync/atomic"
)

type contributionCustodyKey struct{}
type contributionCustodyProof struct {
	terminal                     *contributionTerminalSlot
	service                      *Service
	contract, candidate, session string
	live                         atomic.Bool
}

// WithContributionContinuityCandidateAs consumes the real #376 lock-held gate,
// retaining its original admission checks. It does not certify legacy native
// producers: without their exact registered boundary the join remains denied.
// The action is synchronous and must not retain the context or launch work.
func (s *Service) WithContributionContinuityCandidateAs(ctx context.Context, sub core.Subject, session, key string, action func(context.Context, orchestration.ContributionCandidate) error) error {
	if action == nil {
		return ErrDenied
	}
	gate := func(ctx context.Context, candidate orchestration.ContributionCandidate) error {
		c, state, _, err := s.ObserveContributionContinuityAs(ctx, sub, key)
		if err != nil {
			return err
		}
		lineage := (orchestration.ContributionCheckpoint{RunID: c.RunID, Pass: candidate.Pass}).Lineage()
		if candidate.Contract != c.Digest || candidate.Pass == 0 || candidate.Pass > c.CandidateBudget || state.Candidate != lineage || candidate.Digest != state.CandidateEvidence.Digest {
			return ErrDenied
		}
		known := map[string]bool{}
		for pass := uint8(0); pass <= candidate.Pass; pass++ {
			known[(orchestration.ContributionCheckpoint{RunID: c.RunID, Pass: pass}).Lineage()] = true
		}
		if _, err = s.CheckContributionRoleAs(ctx, sub, session, key, "controller", ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: state.Sequence, ExpectedDigest: state.Digest}); err != nil {
			return err
		}
		for _, role := range []string{"builder", "tester"} {
			if !contribution.NativeRoleQuiescent(state, role) {
				return contribution.ErrNativeQuiescenceUnavailable
			}
			current := false
			claim := ""
			for _, binding := range state.NativeBoundaries[role].Bindings {
				if !known[binding.Candidate] || binding.RunID != c.RunID || binding.QueueItemID != c.QueueItemID || binding.PolicyDigest != c.TemplateDigest || binding.AttemptID != candidate.QueueAttempt || binding.ClaimID == "" {
					return ErrDenied
				}
				if claim != "" && claim != binding.ClaimID {
					return ErrDenied
				}
				claim = binding.ClaimID
				if binding.Candidate == lineage {
					current = true
				}
			}
			if !current {
				return contribution.ErrNativeQuiescenceUnavailable
			}
		}
		return nil
	}
	sealed, _, err := s.ReadContributionAs(ctx, sub, key)
	if err != nil {
		return err
	}
	definition, err := s.FleetRepository.GetLoopRevision(ctx, sealed.Loop.ID, sealed.Loop.Revision)
	if err != nil || definition.Digest != sealed.Loop.Digest {
		return ErrDenied
	}
	var terminal *contributionTerminalSlot
	if definition.Contribution != nil && definition.Contribution.FinalVerification == evidence.ContributionFinalPolicy {
		terminal = &contributionTerminalSlot{open: true, valid: true}
		defer terminal.invalidate()
	}
	runAction := func(ctx context.Context, candidate orchestration.ContributionCandidate) error {
		if err := gate(ctx, candidate); err != nil {
			return err
		}
		proof := &contributionCustodyProof{service: s, contract: candidate.Contract, candidate: candidate.Digest, session: session, terminal: terminal}
		proof.live.Store(true)
		defer proof.live.Store(false)
		if terminal != nil {
			defer terminal.closeAction()
		}
		return action(context.WithValue(ctx, contributionCustodyKey{}, proof), candidate)
	}
	if terminal != nil {
		if s.QueueWorker == nil {
			return ErrDenied
		}
		return s.QueueWorker.WithFinalizableContributionCandidate(ctx, sub, sealed.QueueItemID, definition, gate, runAction, terminal.consume)
	}
	return s.WithAcceptedContributionCandidateAs(ctx, sub, key, gate, runAction)
}
