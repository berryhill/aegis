package app

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/reference"
)

var ErrContributionFinalizationNotEnabled = errors.New("immutable contribution profile has no final verification")

type contributionFramePolicyResolver struct {
	history *ContributionDeliveryPolicyHistory
	ref     reference.DigestRef
}

func (r contributionFramePolicyResolver) ResolveContributionDeliveryPolicy(ctx context.Context, ref reference.DigestRef) (ContributionDeliveryPolicy, error) {
	if ref != r.ref {
		return ContributionDeliveryPolicy{}, ErrDenied
	}
	return r.history.ResolveContributionDeliveryPolicyDigest(ctx, ref.Digest)
}

type contributionCompletionFrame struct {
	proof            *contributionCustodyProof
	role             reference.DigestRef
	control, subject string
}

// NewContributionCompletionInFrame is the concrete #379 insertion port for the
// actual owning controller AFTER it retains the projected immutable policy.
// A request cannot create the required private source frame or supply records.
// No source lock is reacquired, and no fallback policy or authority is installed.
func (s *Service) NewContributionCompletionInFrame(ctx context.Context, subject core.Subject, key string, ref reference.DigestRef) (*ContributionCompletionAdapter, error) {
	if s == nil || ctx == nil || ref.Validate() != nil || s.requirePrincipal(subject) != nil {
		return nil, ErrDenied
	}
	proof, ok := ctx.Value(contributionCustodyKey{}).(*contributionCustodyProof)
	if !ok || proof == nil || !proof.live.Load() || proof.service != s {
		return nil, ErrDenied
	}
	c, state, _, err := s.ObserveContributionContinuityAs(ctx, subject, key)
	if err != nil {
		return nil, err
	}
	definition, err := s.FleetRepository.GetLoopRevision(ctx, c.Loop.ID, c.Loop.Revision)
	if err != nil || definition.Digest != c.Loop.Digest || definition.Contribution == nil {
		return nil, ErrDenied
	}
	if definition.Contribution.FinalVerification == "" {
		return nil, ErrContributionFinalizationNotEnabled
	}
	if definition.Contribution.FinalVerification != evidence.ContributionFinalPolicy {
		return nil, ErrDenied
	}
	history, err := NewContributionDeliveryPolicyHistory(s)
	if err != nil {
		return nil, err
	}
	p, err := history.ResolveContributionDeliveryPolicyDigest(ctx, ref.Digest)
	if err != nil {
		return nil, err
	}
	role := state.RoleAuthorities["controller"]
	if proof.contract != c.Digest || proof.candidate != state.CandidateEvidence.Digest || proof.session != role.SessionID || p.Binding.Key != key || p.Binding.OwnerID != subject.PrincipalID || p.Binding.QueueItemID != c.QueueItemID || p.Binding.Run != c.RunID || p.Binding.Contract != c.Digest || p.Binding.Candidate != proof.candidate || p.ControllerSession != proof.session || p.Binding.FenceEpoch != role.Epoch || p.Binding.RoleBinding != role.Ref().Digest || p.Loop != c.Loop {
		return nil, ErrDenied
	}
	candidate, err := s.AcceptedContributionCandidateAs(ctx, subject, key)
	if err != nil || candidate.Digest != proof.candidate || candidate.Source.Digest != p.Source || candidate.CandidateTree != p.Tree || candidate.Toolchain != p.Toolchain || candidate.QueueAttempt != p.QueueAttempt || candidate.Policy != p.CandidatePolicy {
		return nil, ErrDenied
	}
	request := contribution.EffectAdmissionRequest{Contract: referenceForContribution(c), RunID: c.RunID, QueueItemID: c.QueueItemID, Candidate: proof.candidate, CandidateEvidence: state.CandidateEvidence, Role: "controller", Epoch: role.Epoch, RoleAuthority: role.Ref(), Scope: c.Effect}
	if _, err = s.CheckContributionEffectAs(ctx, subject, role.SessionID, key, request, true); err != nil {
		return nil, err
	}
	control, err := s.ReadContributionControlAs(ctx, subject, key)
	if err != nil || control.Stop {
		return nil, ErrDenied
	}
	wire, _ := json.Marshal(control)
	verifier, err := NewContributionFinalVerifier(s, ref, contributionFramePolicyResolver{history, ref})
	if err != nil {
		return nil, err
	}
	completion, err := NewContributionCompletionAdapter(verifier, subject, key)
	if err != nil {
		return nil, err
	}
	completion.frame = &contributionCompletionFrame{proof: proof, role: role.Ref(), control: contribution.Digest(wire), subject: core.Digest(subject)}
	return completion, nil
}
func (a *ContributionCompletionAdapter) validateCompletionFrame(ctx context.Context, subject core.Subject) error {
	if a.frame == nil {
		return nil
	} // Standalone path still performs real custody entry.
	if ctx == nil {
		return ErrDenied
	}
	f := a.frame
	s := a.verifier.service
	actual, ok := ctx.Value(contributionCustodyKey{}).(*contributionCustodyProof)
	if !ok || actual == nil || actual != f.proof || !actual.live.Load() || actual.service != s || core.Digest(subject) != f.subject {
		return ErrDenied
	}
	c, state, _, err := s.ObserveContributionContinuityAs(ctx, subject, a.key)
	if err != nil || c.Digest != actual.contract || state.CandidateEvidence.Digest != actual.candidate {
		return ErrDenied
	}
	role := state.RoleAuthorities["controller"]
	if role.Ref() != f.role || role.SessionID != actual.session {
		return ErrDenied
	}
	control, err := s.ReadContributionControlAs(ctx, subject, a.key)
	wire, _ := json.Marshal(control)
	if err != nil || control.Stop || contribution.Digest(wire) != f.control {
		return ErrDenied
	}
	p, err := a.verifier.policies.ResolveContributionDeliveryPolicy(ctx, a.verifier.policy)
	if err != nil || p.Digest() != a.verifier.policy.Digest {
		return ErrDenied
	}
	_, err = s.CheckContributionEffectAs(ctx, subject, role.SessionID, a.key, contribution.EffectAdmissionRequest{Contract: referenceForContribution(c), RunID: c.RunID, QueueItemID: c.QueueItemID, Candidate: actual.candidate, CandidateEvidence: state.CandidateEvidence, Role: "controller", Epoch: role.Epoch, RoleAuthority: role.Ref(), Scope: c.Effect}, true)
	return err
}
