package app

import (
	"context"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
)

func validateDoerHostRunInput(input DoerDraftInput) error {
	if input.HostRun == nil {
		return ErrDoerDraftInvalid
	}
	run, _, err := normalizeLoopRunIdentity(*input.HostRun)
	if err != nil || run.Agent != input.Agent || run.Loop.Validate() != nil || run.Loop.ID != input.LoopID || run.Loop.Revision != input.Revision || run.IdempotencyKey == "" || len(run.IdempotencyKey) > 256 || run.QueueItemID != "" || run.Reexecution != nil || input.Contract.ExactBytes || input.Contract.Coding != nil {
		return ErrDoerDraftInvalid
	}
	candidate, _, err := loop.NewDoerReusableRevision(input.LoopID, input.Revision, input.PreviousDigest, loop.DoerReusableContract{MaxAttempts: input.Contract.MaxAttempts})
	if err != nil || loopRevisionRef(candidate) != run.Loop {
		return ErrDoerDraftInvalid
	}
	values := make([]loop.DoerInput, 0, len(run.Inputs))
	for _, value := range run.Inputs {
		values = append(values, loop.DoerInput{PortID: value.PortID, Type: loop.ValueType(value.Type), Value: value.Value})
	}
	contract, err := loop.NormalizeDoerBinding(candidate, values)
	want, wantErr := input.Contract.Digest()
	got, gotErr := contract.Digest()
	if err != nil || wantErr != nil || gotErr != nil || want != got {
		return ErrDoerDraftInvalid
	}
	return nil
}

// retainQueueLoopSetupReview is reached only from an authenticated execution
// action, never readiness/validation/publication. It retains the same exact Run
// for independent protected review; it issues no consent, session or Queue work.
func (s *Service) retainQueueLoopSetupReview(ctx context.Context, sub core.Subject, input QueueLoopInput, result *QueueLoopResult) error {
	if result == nil || result.QueueItemID != "" || result.Execution != nil || result.Readiness == nil || result.Readiness.definitionSchema != loop.DoerReusableSchemaVersion {
		return nil
	}
	switch result.Reason {
	case "provisioning_receipt_missing", "doer_host_approval_required", "doer_host_approval_absent", "doer_host_approval_invalid", "doer_host_approval_expired":
	default:
		return nil
	}
	if err := s.requireFleetPrincipal(sub); err != nil {
		return err
	}
	run, _, err := normalizeLoopRunIdentity(input)
	if err != nil || run.Reexecution != nil || run.QueueItemID != "" || result.RequestID != queueLoopRequestID(sub, run) {
		return ErrDenied
	}
	if _, err := s.hostApprovalAgent(ctx, run.Agent); err != nil {
		return err
	}
	candidate, err := s.GetLoopAs(ctx, sub, run.Loop.ID, run.Loop.Revision)
	if err != nil || candidate.SchemaVersion != loop.DoerReusableSchemaVersion || loopRevisionRef(candidate) != run.Loop {
		return ErrDenied
	}
	bound, err := bindDoerRun(ctx, run, candidate)
	if err != nil {
		return err
	}
	contract, err := queuedDoerContract(bound, candidate)
	if err != nil {
		return err
	}
	draft, err := s.SaveDoerDraftAs(ctx, sub, DoerDraftInput{
		CreationKey: "host-review-" + result.RequestID,
		Agent:       run.Agent, LoopID: run.Loop.ID, Revision: run.Loop.Revision,
		PreviousDigest: candidate.PreviousDigest, Contract: contract, HostRun: &run,
	})
	if err != nil {
		return err
	}
	result.SetupReviewTask = &draft
	if result.Reason != "provisioning_receipt_missing" {
		result.RequiredAction = "review_exact_reusable_run_host_write"
	}
	return nil
}

// HostReviewRequestID is a deterministic reference, not an issued execution
// intent or authority. The protected adapter uses it to reject wrong-Run facts.
func (d DoerDraft) HostReviewRequestID() string {
	if d.HostRun == nil {
		return ""
	}
	return queueLoopRequestID(core.Subject{PrincipalID: d.PrincipalID}, *d.HostRun)
}

func (d DoerDraft) HostReviewInputDigest() string {
	if d.HostRun == nil {
		return ""
	}
	_, identity, err := normalizeLoopRunIdentity(*d.HostRun)
	if err != nil {
		return ""
	}
	return "sha256:" + identity
}
