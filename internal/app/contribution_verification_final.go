package app

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/reference"
)

// ContributionPreClosureAuthorization has no caller-settable fields. It is not
// a CompletionOwner receipt, Queue disposition or permission to reuse authority.
// Consumption repeats the real source/role/native/delivery join, never a bool.
type ContributionPreClosureAuthorization struct {
	service                                                                              *Service
	key, owner, contract, candidate, native, history, continuity, policy, claim, attempt string
	scope, outcome, cause                                                                string
}

// ContributionFinalVerifier is installed only by trusted controller composition.
// Its public operation takes a retained key, not evidence, policy prose or PASS.
type ContributionFinalVerifier struct {
	service  *Service
	policy   reference.DigestRef
	policies ContributionDeliveryPolicyResolver
}

func NewContributionFinalVerifier(s *Service, ref reference.DigestRef, policies ContributionDeliveryPolicyResolver) (*ContributionFinalVerifier, error) {
	if s == nil || policies == nil || ref.Validate() != nil {
		return nil, ErrDenied
	}
	return &ContributionFinalVerifier{s, ref, policies}, nil
}

// VerifyPreClosureAs mints only full merge-scope pre-closure authorization. Issue
// reconciliation is deliberately not a prerequisite: final post-closure readback
// must subsequently join the exact authorization and canonical disposition.
func (v *ContributionFinalVerifier) VerifyPreClosureAs(ctx context.Context, sub core.Subject, key string) (*ContributionPreClosureAuthorization, RetainedDeliveryAssessment, error) {
	return v.verifyContributionAuthorization(ctx, sub, key, "merge")
}

func (v *ContributionFinalVerifier) verifyContributionAuthorization(ctx context.Context, sub core.Subject, key, mode string) (*ContributionPreClosureAuthorization, RetainedDeliveryAssessment, error) {
	empty := RetainedDeliveryAssessment{Status: "not-verified", Code: "verification_denied", MergeRefs: []string{}}
	if mode != "merge" && mode != "narrow" && mode != "held" {
		return nil, empty, ErrDenied
	}
	if v == nil || v.service == nil || v.policies == nil {
		return nil, empty, ErrDenied
	}
	s := v.service
	sealed, _, lookupErr := s.ReadContributionAs(ctx, sub, key)
	if lookupErr != nil {
		return nil, empty, ErrDenied
	}
	definition, lookupErr := s.FleetRepository.GetLoopRevision(ctx, sealed.Loop.ID, sealed.Loop.Revision)
	if lookupErr != nil || definition.Digest != sealed.Loop.Digest || definition.Contribution == nil || definition.Contribution.FinalVerification != evidence.ContributionFinalPolicy {
		return nil, empty, ErrDenied
	}
	assessment, err := s.AssessContributionRetainedDeliveryAs(ctx, sub, key, v.policy, v.policies)
	if err != nil {
		return nil, assessment, ErrDenied
	}
	fail := func(code string) (*ContributionPreClosureAuthorization, RetainedDeliveryAssessment, error) {
		assessment.Code = code
		return nil, assessment, ErrDenied
	}
	if mode != "held" && (assessment.Status != "retained-delivery-evidence-assessed" || assessment.Code != "continuity_verification_required") {
		// Preserve the closed owning verifier diagnostic instead of erasing the
		// first failed producer join behind a generic preclosure label.
		return nil, assessment, ErrDenied
	}
	p, err := v.policies.ResolveContributionDeliveryPolicy(ctx, v.policy)
	p = p.snapshot()
	if err != nil || p.Digest() != v.policy.Digest {
		return fail("policy_invalid")
	}
	var authorization *ContributionPreClosureAuthorization
	contract, currentState, _, currentErr := s.ObserveContributionContinuityAs(ctx, sub, key)
	currentRole, hasRole := currentState.RoleAuthorities["controller"]
	if currentErr != nil || !hasRole {
		return fail("current_controller_unavailable")
	}
	stage := "source_custody_denied"
	action := func(cctx context.Context, candidate orchestration.ContributionCandidate) error {
		stage = "continuity_history_invalid"
		c, state, events, e := s.ObserveContributionContinuityAs(cctx, sub, key)
		if e != nil {
			return e
		}
		if e = verifyFinalContinuity(c, state, events); e != nil {
			return e
		}
		stage = "continuity_scope_invalid"
		if state.CancelRequested || !s.Now().Before(c.Deadline) || state.CandidateEvidence.Digest != candidate.Digest || candidate.Digest != p.Binding.Candidate {
			return ErrDenied
		}
		control, e := s.ReadContributionControlAs(cctx, sub, key)
		if e != nil || control.Stop {
			return ErrDenied
		}
		scope, e := effectiveContributionScope(c, control)
		if e != nil || (mode == "merge" && scope != "verified-protected-base-merge") || (mode == "narrow" && scope == "verified-protected-base-merge") {
			return ErrDenied
		}
		role, ok := state.RoleAuthorities["controller"]
		if !ok || role != currentRole {
			return ErrDenied
		}
		request := contribution.EffectAdmissionRequest{Contract: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: c.QueueItemID, Digest: c.Digest}, RunID: c.RunID, QueueItemID: c.QueueItemID, Candidate: candidate.Digest, CandidateEvidence: state.CandidateEvidence, Role: "controller", Epoch: role.Epoch, RoleAuthority: role.Ref(), Scope: c.Effect}
		stage = "continuity_admission_denied"
		if _, e = s.CheckContributionEffectAs(cctx, sub, role.SessionID, key, request, true); e != nil {
			return e
		}
		record, e := s.ReadContributionExecutionAs(cctx, sub, key)
		stage = "continuity_native_roles_invalid"
		if e != nil {
			return e
		}
		if e = s.verifyFinalNativeRoles(c, state, events, record); e != nil {
			return e
		}
		_, view, e := s.ReadContributionAs(cctx, sub, key)
		stage = "continuity_claim_invalid"
		if e != nil {
			return e
		}
		claim, e := s.FleetRepository.GetClaim(cctx, view.Projection.ActiveClaimID)
		if e != nil || claim.AttemptID != candidate.QueueAttempt {
			return ErrDenied
		}
		if state.Continuation != nil {
			lease := state.Continuation
			if lease.ClaimID != claim.ClaimID || lease.AttemptID != claim.AttemptID || lease.Controller != role.Ref() || lease.Epoch != role.Epoch || !s.Now().Before(lease.ExpiresAt) || lease.ExpiresAt.After(c.Deadline) {
				return ErrDenied
			}
		} else if !s.Now().Before(claim.ExpiresAt) {
			return ErrDenied
		}
		// Read current owning native/delivery evidence without reacquiring source
		// custody. These read methods do not call WithAcceptedCandidate.
		again, e := s.ReadContributionCandidateEvidenceAs(cctx, sub, key)
		stage = "continuity_native_evidence_changed"
		if e != nil || again.EvidenceRef != assessment.NativeRef {
			return ErrDenied
		}
		history, e := s.ReadContributionDeliveryHistoryAs(cctx, sub, key)
		stage = "continuity_delivery_history_changed"
		if e != nil {
			return e
		}
		wire, _ := json.Marshal(history)
		if contribution.Digest(wire) != assessment.HistoryRef {
			return ErrDenied
		}
		policyAfter, e := v.policies.ResolveContributionDeliveryPolicy(cctx, v.policy)
		stage = "continuity_policy_changed"
		if e != nil || policyAfter.Digest() != p.Digest() {
			return ErrDenied
		}
		_, after, afterEvents, e := s.ObserveContributionContinuityAs(cctx, sub, key)
		stage = "continuity_head_changed"
		if e != nil || verifyFinalContinuity(c, after, afterEvents) != nil || after.Digest != state.Digest {
			return ErrDenied
		}
		if _, e = s.CheckContributionEffectAs(cctx, sub, role.SessionID, key, request, true); e != nil {
			return e
		}
		stage = "source_postcondition_denied"
		outcome, cause := "succeeded", ""
		if mode == "narrow" {
			outcome = "narrower-target-completed"
		}
		if mode == "held" {
			outcome, cause = classifyContributionHeldHistory(history, candidate.Digest)
			if outcome == "" {
				return ErrDenied
			}
		}
		authorization = &ContributionPreClosureAuthorization{service: s, key: key, owner: sub.PrincipalID, contract: c.Digest, candidate: candidate.Digest, native: assessment.NativeRef, history: assessment.HistoryRef, continuity: state.Digest, policy: v.policy.Digest, claim: claim.ClaimID, attempt: claim.AttemptID, scope: scope, outcome: outcome, cause: cause}
		return nil
	}
	if proof, ok := ctx.Value(contributionCustodyKey{}).(*contributionCustodyProof); ok {
		if proof == nil || !proof.live.Load() || proof.service != s || proof.contract != contract.Digest || proof.candidate != currentState.CandidateEvidence.Digest || proof.session != currentRole.SessionID {
			return fail("source_custody_invalid")
		}
		candidate, e := s.AcceptedContributionCandidateAs(ctx, sub, key)
		if e != nil {
			return fail("native_unavailable")
		}
		err = action(ctx, candidate)
	} else {
		err = s.WithContributionContinuityCandidateAs(ctx, sub, currentRole.SessionID, key, action)
	}
	if err != nil {
		return fail(stage)
	}
	if authorization == nil {
		return fail("verification_denied")
	}
	return authorization, assessment, nil
}

// RevalidatePreClosureAs cannot turn a zero value, another service or stale
// authorization into closure permission. It repeats the private live-custody gate.
func (v *ContributionFinalVerifier) RevalidatePreClosureAs(ctx context.Context, sub core.Subject, key string, a *ContributionPreClosureAuthorization) error {
	if v == nil || a == nil || a.service != v.service || a.key != key || a.owner != sub.PrincipalID {
		return ErrDenied
	}
	current, _, err := v.VerifyPreClosureAs(ctx, sub, key)
	if err != nil || current == nil || *current != *a {
		return ErrDenied
	}
	return nil
}

func verifyFinalContinuity(c contribution.Contract, state contribution.Continuity, events []contribution.ContinuityEvent) error {
	if len(events) == 0 {
		return ErrDenied
	}
	var replay contribution.Continuity
	for _, event := range events {
		wire, err := json.Marshal(event)
		if err != nil {
			return ErrDenied
		}
		canonical, err := contribution.UnmarshalContinuityEvent(wire)
		if err != nil {
			return ErrDenied
		}
		replay, err = contribution.ApplyContinuity(c, replay, canonical)
		if err != nil {
			return ErrDenied
		}
	}
	want, _ := json.Marshal(state)
	actual, _ := json.Marshal(replay)
	if !bytes.Equal(want, actual) || state.QueueItemID != c.QueueItemID || state.ContractDigest != c.Digest || state.Used.Candidates > uint32(c.CandidateBudget) || state.Used.QueueAttempts != 1 {
		return ErrDenied
	}
	for _, role := range []string{"builder", "tester"} {
		if !contribution.NativeRoleQuiescent(state, role) {
			return contribution.ErrNativeQuiescenceUnavailable
		}
	}
	for _, effect := range state.NativeEffects {
		if effect.State != "completed" {
			return contribution.ErrNativeQuiescenceUnavailable
		}
	}
	return nil
}

func (s *Service) verifyFinalNativeRoles(c contribution.Contract, state contribution.Continuity, events []contribution.ContinuityEvent, r orchestration.ContributionRecord) error {
	if r.RunID != c.RunID || r.Contract != c.Digest || r.State != "candidate_accepted" || len(r.Passes) == 0 || int(r.Consumed) != len(r.Passes) || uint32(r.Consumed) != state.Used.Candidates {
		return ErrDenied
	}
	matches := func(role string, id orchestration.ContributionRoleIdentity, lineage string) bool {
		for _, event := range events {
			a := event.RoleAuthority
			if event.Kind != "role-admitted" || a == nil || a.Role != role || a.Binding.Agent.Digest != id.AgentDigest || a.Binding.Charter.Digest != id.CharterDigest || a.Authority.ID != id.AuthorityID || a.Authority.Digest != id.AuthorityDigest || a.MandateID != id.MandateID || a.SessionID != id.SessionID {
				continue
			}
			for _, effect := range state.NativeEffects {
				b := effect.Binding
				if effect.State == "completed" && b.Role == role && b.Epoch == a.Epoch && b.Authority == a.Ref() && b.RunID == c.RunID && b.QueueItemID == c.QueueItemID && b.AttemptID == r.QueueAttempt && b.Candidate == lineage && b.PolicyDigest == c.TemplateDigest {
					if s.matchesContributionTurn(c, state, *a, id, lineage, r.QueueAttempt, b.ClaimID) {
						return true
					}
				}
			}
		}
		return false
	}
	for _, pass := range r.Passes {
		lineage := (orchestration.ContributionCheckpoint{RunID: c.RunID, Pass: pass.Candidate.Pass}).Lineage()
		if !matches("builder", pass.Candidate.Builder, lineage) || !matches("tester", pass.Tester, lineage) {
			return ErrDenied
		}
	}
	return nil
}
