package app

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
)

type productiveContributionRepair struct {
	controller   *ContributionDeliveryController
	subject      core.Subject
	key, session string
}

// RequestRepair is eligibility inside accepted custody, not an async executor.
// RunAs repeats this exact admission AFTER that frame closes, then resumes the
// existing managed attempt synchronously under the original absolute deadline.
func (r *productiveContributionRepair) RequestRepair(ctx context.Context, f delivery.RepairFailure) error {
	_, _, err := r.admit(ctx, f)
	return err
}

func (r *productiveContributionRepair) admit(ctx context.Context, f delivery.RepairFailure) (orchestration.ContributionDeliveryRepair, contribution.Continuity, error) {
	bad := delivery.ErrDenied
	var out orchestration.ContributionDeliveryRepair
	var state contribution.Continuity
	if r == nil || r.controller == nil || ctx == nil || ctx.Err() != nil {
		return out, state, bad
	}
	s := r.controller.service
	c, state, _, err := s.ObserveContributionContinuityAs(ctx, r.subject, r.key)
	if err != nil || s.validateContributionRequestSubject(ctx, r.subject) != nil || state.CancelRequested || !s.Now().Before(c.Deadline) {
		return out, state, bad
	}
	control, err := s.ReadContributionControlAs(ctx, r.subject, r.key)
	if err != nil || control.Stop {
		return out, state, bad
	}
	if _, err = s.CheckContributionRoleAs(ctx, r.subject, r.session, r.key, "controller", ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: state.Sequence, ExpectedDigest: state.Digest}); err != nil {
		return out, state, bad
	}
	record, err := s.ReadContributionExecutionAs(ctx, r.subject, r.key)
	if err != nil || record.State != "candidate_accepted" || record.Contract != c.Digest || record.RunID != c.RunID || len(record.Passes) == 0 || record.Consumed >= c.CandidateBudget || int(record.Consumed) != len(record.Passes) {
		return out, state, bad
	}
	candidate := record.Passes[len(record.Passes)-1].Candidate
	if f.Reference != (delivery.Reference{Run: c.RunID, Contract: c.Digest, Candidate: candidate.Digest}) {
		return out, state, bad
	}
	projection, err := s.FleetRepository.GetQueueProjection(ctx, c.QueueItemID)
	if err != nil || projection.State != queue.StateClaimed || projection.Attempts != 1 {
		return out, state, bad
	}
	claim, err := s.FleetRepository.GetClaim(ctx, projection.ActiveClaimID)
	if err != nil || claim.AttemptID != record.QueueAttempt {
		return out, state, bad
	}
	for _, role := range []string{"builder", "tester"} {
		if !contribution.NativeRoleQuiescent(state, role) {
			return out, state, contribution.ErrNativeQuiescenceUnavailable
		}
	}
	h, err := s.ReadContributionDeliveryHistoryAs(ctx, r.subject, r.key)
	if err != nil || delivery.ValidateHistorySnapshot(h) != nil {
		return out, state, bad
	}
	var failed delivery.Record
	for _, chain := range h.Histories {
		last := chain[len(chain)-1].Record
		// Every effect must be known before another candidate may write. A merge or
		// issue intent is irreversible/possibly irreversible even if later denied.
		_, advanceErr := delivery.VerifiedBaseAdvance(last)
		if (last.Outcome == delivery.Unknown && advanceErr != nil) || last.Merge != "" || last.Intent.Operation == delivery.ReconcileIssue {
			return out, state, bad
		}
		if last.Intent.ID == f.Intent {
			failed = last
		}
	}
	wire, _ := json.Marshal(failed)
	currentBase, advanceErr := delivery.VerifiedBaseAdvance(failed)
	knownAdvance := advanceErr == nil
	if failed.Intent.Reference != f.Reference || failed.Revision < 2 || (!knownAdvance && (failed.Outcome != delivery.Failed || failed.Observation.Outcome != delivery.Failed)) || failed.Merge != "" ||
		(failed.Intent.Operation != delivery.CheckDelivery && failed.Intent.Operation != delivery.Merge) || (!knownAdvance && failed.Code != "ci_failed" && failed.Code != "merge_conflict") ||
		failed.Code != f.Class || f.Criterion != f.Class || contribution.Digest(wire) != f.Evidence || failed.Intent.Head != f.Head || failed.Intent.BaseHead != f.Base || failed.Intent.PR == 0 {
		return out, state, bad
	}
	feedback, feedbackErr := delivery.ConcreteRepairFeedback(failed, r.controller.policy.RequiredChecks)
	if feedbackErr != nil && !knownAdvance {
		return out, state, bad
	}
	out = orchestration.ContributionDeliveryRepair{CurrentBase: currentBase, Feedback: feedback, Failure: f.Intent, Evidence: f.Evidence, Candidate: f.Candidate, Class: f.Class, Head: f.Head, Base: f.Base, PR: failed.Intent.PR, AfterPass: record.Consumed}
	return out, state, nil
}

func (d *ContributionDeliveryController) repairDeliveryAs(ctx context.Context, subject core.Subject, key, session string, failed delivery.Record) error {
	ctx, err := d.service.bindContributionRequestSubject(ctx, subject)
	if err != nil {
		return err
	}
	wire, _ := json.Marshal(failed)
	f := delivery.RepairFailure{Reference: failed.Intent.Reference, Intent: failed.Intent.ID, Class: failed.Code, Criterion: failed.Code, Evidence: contribution.Digest(wire), Head: failed.Intent.Head, Base: failed.Intent.BaseHead}
	owner := &productiveContributionRepair{controller: d, subject: subject, key: key, session: session}
	repair, state, err := owner.admit(ctx, f)
	if err != nil {
		return err
	}
	s := d.service
	c, _, err := s.ReadContributionAs(ctx, subject, key)
	if err != nil {
		return err
	}
	projection, err := s.FleetRepository.GetQueueProjection(ctx, c.QueueItemID)
	if err != nil {
		return err
	}
	claim, err := s.FleetRepository.GetClaim(ctx, projection.ActiveClaimID)
	if err != nil {
		return err
	}
	// An initial still-live controller can continue the SAME immutable claim.
	// This fact does not renew its mandate or original deadline.
	if state.Continuation == nil {
		role := state.RoleAuthorities["controller"]
		mandate, e := s.Authority.GetMandate(ctx, role.MandateID)
		if e != nil {
			return e
		}
		expires := mandate.ExpiresAt
		if c.Deadline.Before(expires) {
			expires = c.Deadline
		}
		fact := contribution.Continuation{ClaimID: claim.ClaimID, AttemptID: claim.AttemptID, Controller: role.Ref(), Epoch: role.Epoch, ExpiresAt: expires}
		event, e := contribution.SealContinuityEvent(contribution.ContinuityEvent{SchemaVersion: contribution.ContinuitySchema, QueueItemID: c.QueueItemID, ContractDigest: c.Digest, OwnerID: c.OwnerID, SessionID: c.SessionID, Sequence: state.Sequence + 1, PreviousDigest: state.Digest, Kind: "continuation-admitted", Continuation: &fact, At: s.Now()})
		if e != nil {
			return e
		}
		if _, e = s.FleetRepository.(fleet.ContributionContinuityRepository).AppendContributionContinuity(ctx, event); e != nil {
			return e
		}
	}
	_, err = s.QueueWorker.ResumeContributionDeliveryRepair(ctx, subject, c.QueueItemID, func(locked context.Context, record orchestration.ContributionRecord) (orchestration.ContributionDeliveryRepair, error) {
		current, _, e := owner.admit(locked, f)
		if e != nil || current != repair || record.QueueAttempt != claim.AttemptID {
			return orchestration.ContributionDeliveryRepair{}, delivery.ErrDenied
		}
		return current, nil
	})
	if errors.Is(err, orchestration.ErrContributionCandidatePending) {
		return nil
	}
	return err
}
