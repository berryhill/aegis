package app

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
)

// ResumeContributionAs is explicit same-owner admission, never a login side
// effect. Preparation holds and independently issued mandates retain normal gates.
func (s *Service) ResumeContributionAs(ctx context.Context, sub core.Subject, key string) (ContributionExecutionResult, error) {
	result := ContributionExecutionResult{Available: true}
	if err := s.requireFleetPrincipal(sub); err != nil {
		return result, err
	}
	c, view, err := s.ReadContributionAs(ctx, sub, key)
	if err != nil {
		return result, err
	}
	if len(view.Attempts) == 0 && (view.Projection.State.IsPreparation() || view.Projection.State == queue.StateQueued) {
		return s.ExecuteContributionAs(ctx, sub, key)
	}
	c, state, _, err := s.ObserveContributionContinuityAs(ctx, sub, key)
	if err != nil {
		return result, err
	}
	if expected := ContributionExpectedService(ctx); expected != nil && *expected != CurrentContributionServiceIdentity() {
		return result, ErrConflict
	}
	if err = s.retainContributionRunProvenance(c); err != nil {
		return result, err
	}
	runCtx, finish, err := s.beginContributionRun(ctx, c)
	if err != nil {
		return result, err
	}
	defer finish()
	ctx = runCtx
	control, err := s.ReadContributionControlAs(ctx, sub, key)
	if err != nil || control.Stop || state.CancelRequested || !s.Now().Before(c.Deadline) {
		return result, ErrDenied
	}
	record, err := s.ReadContributionExecutionAs(ctx, sub, key)
	if err != nil || (record.State != "phase_boundary" && record.State != "candidate_accepted") {
		return result, ErrDenied
	}
	projection, err := s.FleetRepository.GetQueueProjection(ctx, c.QueueItemID)
	if err != nil || projection.State != queue.StateClaimed || projection.Attempts != 1 {
		return result, ErrDenied
	}
	claim, err := s.FleetRepository.GetClaim(ctx, projection.ActiveClaimID)
	if err != nil || claim.AttemptID != record.QueueAttempt {
		return result, ErrDenied
	}
	if s.acceptedContributionControllerLive(ctx, sub, c, state, record) {
		return s.ContributionExecutionStatusAs(ctx, sub, key)
	}
	needsRecovery := false
	for role := range state.RoleAuthorities {
		if !contribution.NativeRoleQuiescent(state, role) {
			needsRecovery = true
		}
	}
	if needsRecovery {
		err = s.QueueWorker.WithContributionRecovery(ctx, sub, c.QueueItemID, func(locked context.Context) error {
			var e error
			state, e = s.recoverContributionRoles(locked, c, state)
			return e
		})
		if err != nil {
			return result, err
		}
	}
	for role := range state.RoleAuthorities {
		if !contribution.NativeRoleQuiescent(state, role) {
			return result, contribution.ErrNativeQuiescenceUnavailable
		}
	}
	session, err := s.ResumeContributionRoleAs(runCtx, sub, key, "controller", ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: state.Sequence, ExpectedDigest: state.Digest})
	if err != nil {
		return result, err
	}
	_, state, _, err = s.ObserveContributionContinuityAs(runCtx, sub, key)
	if err != nil {
		return result, err
	}
	role := state.RoleAuthorities["controller"]
	expires := session.Mandate.ExpiresAt
	if c.Deadline.Before(expires) {
		expires = c.Deadline
	}
	fact := contribution.Continuation{ClaimID: claim.ClaimID, AttemptID: claim.AttemptID, Controller: role.Ref(), Epoch: role.Epoch, ExpiresAt: expires}
	event, err := contribution.SealContinuityEvent(contribution.ContinuityEvent{SchemaVersion: contribution.ContinuitySchema, QueueItemID: c.QueueItemID, ContractDigest: c.Digest, OwnerID: c.OwnerID, SessionID: c.SessionID, Sequence: state.Sequence + 1, PreviousDigest: state.Digest, Kind: "continuation-admitted", Continuation: &fact, At: s.Now()})
	if err != nil {
		return result, err
	}
	if _, err = s.FleetRepository.(fleet.ContributionContinuityRepository).AppendContributionContinuity(runCtx, event); err != nil {
		return result, err
	}
	if record.State == "candidate_accepted" {
		return s.ContributionExecutionStatusAs(runCtx, sub, key)
	}
	_, err = s.QueueWorker.ResumeContribution(runCtx, sub, c.QueueItemID)
	if errors.Is(err, orchestration.ErrContributionCandidatePending) {
		err = nil
	}
	observed, readErr := s.ContributionExecutionStatusAs(runCtx, sub, key)
	return observed, errors.Join(err, readErr)
}

// This port is contribution-specific: the original immutable Queue authority is
// not refreshed and generic workers never call it. Every use reloads the exact
// appended epoch and current controller authority, stop and cancellation facts.
func (s *Service) currentContributionAdmission(ctx context.Context, sub core.Subject, c contribution.Contract, claim queue.Claim) (execution.LaunchContract, execution.AdmissionChecker, error) {
	deny := func() (execution.LaunchContract, execution.AdmissionChecker, error) {
		return execution.LaunchContract{}, nil, ErrDenied
	}
	current, state, _, err := s.ObserveContributionContinuityAs(ctx, sub, c.Key)
	if err != nil || current.Digest != c.Digest {
		return deny()
	}
	if state.Continuation == nil {
		return execution.LaunchContract{}, nil, orchestration.ErrNoContributionContinuation
	}
	lease := state.Continuation
	role := state.RoleAuthorities["controller"]
	if lease.ClaimID != claim.ClaimID || lease.AttemptID != claim.AttemptID || lease.Controller != role.Ref() || lease.Epoch != role.Epoch || !s.Now().Before(lease.ExpiresAt) {
		return deny()
	}
	checked, err := s.CheckContributionRoleAs(ctx, sub, role.SessionID, c.Key, "controller", ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: state.Sequence, ExpectedDigest: state.Digest})
	if err != nil || checked != role {
		return deny()
	}
	authority, err := s.authorityContextForSession(ctx, role.SessionID)
	if err != nil {
		return deny()
	}
	mandate, err := s.Authority.GetMandate(ctx, role.MandateID)
	if err != nil {
		return deny()
	}
	previous, err := s.Authority.GetAuthorityContext(ctx, claim.Authority.ID)
	if err != nil || previous.Digest != claim.Authority.Digest {
		return deny()
	}
	old, err := s.Authority.GetMandate(ctx, previous.MandateID)
	if err != nil || !compatibleContributionMandates(old, mandate) {
		return deny()
	}
	now := s.Now()
	launch := execution.LaunchContract{OwnerID: c.OwnerID, Mandate: mandate, AuthorityContext: authority, ParentDispatch: execution.Dispatch{ID: claim.AttemptID, AuthorityContextID: authority.ID, State: execution.StateSucceeded, RequestedAt: now, StartedAt: &now, FinishedAt: &now}}
	return launch, contributionRoleAdmission{s, c, 0}, nil
}
