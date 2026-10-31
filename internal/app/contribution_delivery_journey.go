package app

import (
	"context"
	"errors"
	"reflect"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/credentials"
	"github.com/berryhill/aegis/internal/delivery"
)

var ErrContributionDeliveryControllerUnavailable = errors.New("delivery_controller_unavailable")
var ErrContributionDeliveryControllerConflict = errors.New("delivery_controller_setup_conflict")

// InstallContributionDeliveryController is trusted startup composition only. It
// snapshots exact policy on the existing same-service publication owner; requests
// cannot install, replace or extend authority. An empty credential container is
// usable for local-only targets, never a remote credential fallback.
func InstallContributionDeliveryController(s *Service, policy *ContributionDeliveryServerPolicy, authority *credentials.Authority, privacy delivery.Privacy) (*ContributionDeliveryController, error) {
	if s == nil {
		return nil, ErrContributionDeliveryControllerUnavailable
	}
	owner, ok := s.ContributionPublicationQuiescer.(*ContributionDeliveryPublicationOwner)
	if !ok || owner == nil || owner.service != s {
		return nil, ErrContributionDeliveryControllerUnavailable
	}
	controller, err := NewContributionDeliveryController(s, policy, authority, privacy)
	if err != nil {
		return nil, err
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if old := owner.controller; old != nil {
		if reflect.DeepEqual(old.policy, controller.policy) && old.credentials == authority && sameContributionPrivacy(old.privacy, privacy) {
			return old, nil
		}
		return nil, ErrContributionDeliveryControllerConflict
	}
	owner.controller = controller
	return controller, nil
}

func sameContributionPrivacy(a, b delivery.Privacy) bool {
	// Do not invoke user implementations or panic for non-comparable values.
	return reflect.TypeOf(a) == reflect.TypeOf(b) && reflect.TypeOf(a) != nil && reflect.TypeOf(a).Comparable() && a == b
}

// ContributionJourneyResult is transient readback, never #379 disposition.
type ContributionJourneyResult struct {
	Execution         ContributionExecutionResult `json:"execution"`
	Delivery          *delivery.DeliveryResult    `json:"delivery,omitempty"`
	HeldStage         string                      `json:"held_stage,omitempty"`
	HeldReason        string                      `json:"held_reason,omitempty"`
	Completed         bool                        `json:"completed"`
	BoundaryCompleted bool                        `json:"boundary_completed"`
	Result            *ContributionResult         `json:"result,omitempty"`
}

// ExecuteContributionJourneyAs retains one registered service-owned invocation
// through native execution, publication and disposition. A transport disconnect
// neither cancels that invocation nor renews its original authority. Accepted
// replays reuse canonical candidate/effect history, not another builder or task.
func (s *Service) ExecuteContributionJourneyAs(ctx context.Context, subject core.Subject, key string) (ContributionJourneyResult, error) {
	return s.runContributionJourneyAs(ctx, subject, key, false)
}

// ResumeContributionJourneyAs keeps the same registered owner across retained
// native continuation and delivery. It neither renews authority nor task consent.
func (s *Service) ResumeContributionJourneyAs(ctx context.Context, subject core.Subject, key string) (ContributionJourneyResult, error) {
	return s.runContributionJourneyAs(ctx, subject, key, true)
}

func (s *Service) runContributionJourneyAs(ctx context.Context, subject core.Subject, key string, resume bool) (out ContributionJourneyResult, err error) {
	// A retained terminal result is read-only navigation, never a new native or
	// remote effect. Only canonical not-found may fall through to execution.
	if found, readErr := s.readContributionJourneyResult(ctx, subject, key, &out); found || readErr != nil {
		return out, readErr
	}
	c, view, err := s.ReadContributionAs(ctx, subject, key)
	if err != nil {
		return out, err
	}
	runCtx, finish, err := s.beginContributionRun(ctx, c)
	if err != nil {
		return out, err
	}
	defer finish()
	ctx = runCtx
	if resume {
		out.Execution, err = s.resumeContributionInRun(ctx, subject, key)
	} else {
		out.Execution, err = s.executeContributionInRun(ctx, subject, key, c, view)
	}
	if err != nil {
		return out, err
	}
	out.HeldStage, out.HeldReason = "execution", out.Execution.Reason
	if out.Execution.Candidate == nil || out.Execution.Candidate.State != "candidate_accepted" {
		return out, nil
	}
	out.HeldStage, out.HeldReason = "delivery", ErrContributionDeliveryControllerUnavailable.Error()
	owner, ok := s.ContributionPublicationQuiescer.(*ContributionDeliveryPublicationOwner)
	if !ok || owner == nil || owner.service != s {
		return out, nil
	}
	owner.mu.Lock()
	controller := owner.controller
	owner.mu.Unlock()
	if controller == nil {
		return out, nil
	}
	c, state, _, e := s.ObserveContributionContinuityAs(ctx, subject, key)
	if e != nil {
		out.HeldReason = "delivery_continuity_unavailable"
		return out, nil
	}
	role, ok := state.RoleAuthorities["controller"]
	if !ok || role.SessionID == "" || role.Epoch == 0 || role.Epoch != state.RoleEpochs["controller"] {
		out.HeldReason = "delivery_controller_role_unavailable"
		return out, nil
	}
	if _, e = s.CheckContributionRoleAs(ctx, subject, role.SessionID, key, "controller", ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: state.Sequence, ExpectedDigest: state.Digest}); e != nil {
		out.HeldReason = "delivery_controller_authority_denied"
		return out, nil
	}
	result, e := controller.RunAs(ctx, subject, key, role.SessionID)
	out.Delivery = &result
	if found, readErr := s.readContributionJourneyResult(ctx, subject, key, &out); found || readErr != nil {
		return out, readErr
	}
	out.HeldReason = result.Reason
	if result.Held != "" {
		out.HeldStage = string(result.Held)
	}
	if e != nil {
		switch {
		case errors.Is(e, ErrContributionDeliveryApprovalUnavailable):
			out.HeldReason = "delivery_approval_unavailable"
		case errors.Is(e, ErrContributionDeliveryUnavailable):
			out.HeldReason = "delivery_controller_unavailable"
		default:
			out.HeldReason = "delivery_authority_or_source_denied"
		}
	} else if result.Held == "" {
		out.HeldStage, out.HeldReason = "disposition", "completion_owner_unavailable_issue_open"
	}
	// Legacy non-final profiles remain held. Delivery alone is not disposition.
	return out, nil
}

func (s *Service) readContributionJourneyResult(ctx context.Context, subject core.Subject, key string, out *ContributionJourneyResult) (bool, error) {
	terminal, err := s.GetFinalContributionResultAs(ctx, subject, key)
	if IsContributionResultNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	out.Result = &terminal
	out.Completed = terminal.Report.Outcome == "succeeded"
	out.BoundaryCompleted = out.Completed || terminal.Report.Outcome == "narrower-target-completed"
	out.HeldReason = terminal.Report.Reason
	out.HeldStage = "disposition"
	if out.BoundaryCompleted {
		out.HeldStage, out.HeldReason = "", ""
	}
	return true, nil
}
