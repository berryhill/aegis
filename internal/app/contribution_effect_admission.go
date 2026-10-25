package app

import (
	"context"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
)

// CheckContributionEffectAs is the neutral #378 bridge. A successful read check
// grants no effect. Writing remains fenced until the source owner supplies its
// exact trusted native-quiescence join; caller references/booleans cannot mint it.
// Call inside #376's lock-held pre/post gate, never instead of source custody.
func (s *Service) CheckContributionEffectAs(ctx context.Context, sub core.Subject, session, key string, in contribution.EffectAdmissionRequest, writing bool) (contribution.RoleAuthority, error) {
	if err := ctx.Err(); err != nil {
		return contribution.RoleAuthority{}, err
	}
	c, state, _, err := s.ObserveContributionContinuityAs(ctx, sub, key)
	if err != nil {
		return contribution.RoleAuthority{}, err
	}
	if in.Contract.Validate() != nil || in.Contract.ID != c.QueueItemID || in.Contract.Digest != c.Digest || in.RunID != c.RunID || in.QueueItemID != c.QueueItemID || in.Scope != c.Effect || in.Candidate == "" || in.Candidate != state.CandidateEvidence.Digest || in.CandidateEvidence.Validate() != nil || in.CandidateEvidence != state.CandidateEvidence {
		return contribution.RoleAuthority{}, ErrDenied
	}
	r, err := s.CheckContributionRoleAs(ctx, sub, session, key, in.Role, ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: state.Sequence, ExpectedDigest: state.Digest})
	if err != nil {
		return r, err
	}
	if in.Epoch != r.Epoch || in.RoleAuthority != r.Ref() {
		return contribution.RoleAuthority{}, ErrDenied
	}
	if writing {
		proof, ok := ctx.Value(contributionCustodyKey{}).(*contributionCustodyProof)
		if !ok || !proof.live.Load() || proof.service != s || proof.contract != c.Digest || proof.candidate != state.CandidateEvidence.Digest || proof.session != session || in.Role != "controller" {
			return contribution.RoleAuthority{}, contribution.ErrNativeQuiescenceUnavailable
		}
		for _, role := range []string{"builder", "tester"} {
			if !contribution.NativeRoleQuiescent(state, role) {
				return contribution.RoleAuthority{}, contribution.ErrNativeQuiescenceUnavailable
			}
		}
	}
	return r, nil
}
