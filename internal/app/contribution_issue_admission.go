package app

import (
	"context"
	"encoding/json"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/delivery"
)

// VerifyContributionIssueIntent is the verifier-owned half of productive issue
// reconciliation admission. The owning controller MUST independently admit the
// operation-specific credential/resource/approval scope before any remote effect.
// This method grants none of those rights and accepts no caller-authored proof.
// It requires the opaque preclosure minted by this exact adapter, the same live
// custody frame, and the exact canonical issue intent, including its completion
// reference. Reservation of that same intent is allowed; unrelated history is not.
func (a *ContributionCompletionAdapter) VerifyContributionIssueIntent(ctx context.Context, intent delivery.Intent) error {
	if a == nil || a.verifier == nil || a.verifier.service == nil || a.frame == nil || ctx == nil || ctx.Err() != nil || intent.Operation != delivery.ReconcileIssue {
		return ErrDenied
	}
	if err := a.validateCompletionFrame(ctx, a.subject); err != nil {
		return err
	}
	a.mu.Lock()
	auth, completion := a.authorization, a.reference
	a.mu.Unlock()
	s := a.verifier.service
	if auth == nil || auth.service != s || auth.key != a.key || auth.owner != a.subject.PrincipalID || auth.outcome != "succeeded" || auth.scope != "verified-protected-base-merge" || completion == "" || intent.Reference != (delivery.Reference{Run: auth.run, Contract: auth.contract, Candidate: auth.candidate}) {
		return ErrDenied
	}
	c, state, events, err := s.ObserveContributionContinuityAs(ctx, a.subject, a.key)
	if err != nil || c.Digest != auth.contract || c.RunID != auth.run || verifyFinalContinuity(c, state, events) != nil || !compatiblePreclosureContinuity(c, state, events, auth.continuity) {
		return ErrDenied
	}
	control, err := s.ReadContributionControlAs(ctx, a.subject, a.key)
	if err != nil || control.Stop {
		return ErrDenied
	}
	scope, err := effectiveContributionScope(c, control)
	if err != nil || scope != auth.scope {
		return ErrDenied
	}
	native, err := s.ReadContributionCandidateEvidenceAs(ctx, a.subject, a.key)
	if err != nil || native.EvidenceRef != auth.native || native.Candidate != auth.candidate || native.Attempt != auth.attempt {
		return ErrDenied
	}
	record, err := s.ReadContributionExecutionAs(ctx, a.subject, a.key)
	if err != nil || s.verifyFinalNativeRoles(c, state, events, record) != nil {
		return ErrDenied
	}
	history, err := s.ReadContributionDeliveryHistoryAs(ctx, a.subject, a.key)
	if err != nil || delivery.ValidateHistorySnapshot(history) != nil {
		return ErrDenied
	}
	prior := history
	prior.Histories = nil
	issueCount := 0
	for _, chain := range history.Histories {
		last := chain[len(chain)-1]
		if last.Record.Intent.Operation != delivery.ReconcileIssue {
			prior.Histories = append(prior.Histories, chain)
			continue
		}
		issueCount++
		if issueCount > 1 || last.Record.Intent != intent || (last.Record.Outcome != delivery.Unknown && last.Record.Outcome != delivery.Confirmed) {
			return ErrDenied
		}
	}
	wire, err := json.Marshal(prior)
	if err != nil || contribution.Digest(wire) != auth.history {
		return ErrDenied
	}
	policy, err := a.verifier.policies.ResolveContributionDeliveryPolicy(ctx, a.verifier.policy)
	if err != nil || policy.Digest() != auth.policy {
		return ErrDenied
	}
	candidate, err := s.AcceptedContributionCandidateAs(ctx, a.subject, a.key)
	if err != nil || candidate.Digest != auth.candidate {
		return ErrDenied
	}
	source := &ContributionDeliverySource{service: s, subject: a.subject, key: a.key, policyRef: a.verifier.policy, policies: a.verifier.policies}
	plan, err := source.project(ctx, policy.Binding.Reference, policy, candidate)
	if err != nil {
		return ErrDenied
	}
	plan.PR, err = retainedContributionPR(history, policy)
	if err != nil {
		return ErrDenied
	}
	plan.Completion = completion
	if delivery.ValidateIntentForPlan(plan, intent) != nil {
		return ErrDenied
	}
	role := state.RoleAuthorities["controller"]
	_, err = s.CheckContributionEffectAs(ctx, a.subject, role.SessionID, a.key, contribution.EffectAdmissionRequest{Contract: referenceForContribution(c), RunID: c.RunID, QueueItemID: c.QueueItemID, Candidate: auth.candidate, CandidateEvidence: state.CandidateEvidence, Role: "controller", Epoch: role.Epoch, RoleAuthority: role.Ref(), Scope: scope}, true)
	return err
}
