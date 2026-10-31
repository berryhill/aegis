package managergateway

import (
	"context"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/slash"
	"time"
)

func (s *Service) contribute(ctx context.Context, entry session, request slash.Request) (slash.Result, error) {
	now := s.now()
	result := slash.Result{Schema: slash.SchemaVersion, ResultSchema: "contribution.v1", Operation: "manager.contribution", State: "denied", Reason: "contribution_denied", ActorID: entry.subject.ID, ContextID: entry.id, StartedAt: now, ObservedAt: now}
	args := request.Arguments
	var err error
	switch args[0] {
	case "readiness":
		scope := ""
		if len(args) == 2 {
			scope = args[1]
		}
		readiness, e := s.app.ReadContributionReadinessAs(ctx, entry.subject, app.ContributionReadinessInput{Loop: entry.contributionLoop, Scope: scope, ExpectedService: app.ContributionExpectedService(ctx)})
		err = e
		if err == nil {
			result.State, result.Reason = "observed", readiness.Reason
			result.Data = map[string]any{"readiness": readiness, "execution_started": false, "completed": false}
		}

	case "select":
		selected, e := s.app.SelectContributionLoopAs(ctx, entry.subject, args[1])
		err = e
		if err == nil {
			s.mu.Lock()
			current, ok := s.sessions[entry.id]
			if !ok || current.token != entry.token || !s.now().Before(current.expires) {
				err = app.ErrExpired
			} else {
				current.contributionLoop = selected
				s.sessions[entry.id] = current
			}
			s.mu.Unlock()
			if err == nil {
				result.State, result.Reason = "selected", "exact_published_contribution_loop"
				readiness, e := s.app.ReadContributionReadinessAs(ctx, entry.subject, app.ContributionReadinessInput{Loop: selected, ExpectedService: app.ContributionExpectedService(ctx)})
				if e != nil {
					return result, e
				}
				result.Data = map[string]any{"loop": selected, "readiness": readiness, "execution_started": false}
			}
		}
	case "run":
		if expected := app.ContributionExpectedService(ctx); expected != nil && *expected != app.CurrentContributionServiceIdentity() {
			result.State, result.Reason = "blocked", "candidate_service_mismatch"
			result.Data = map[string]any{"observing_service": app.CurrentContributionServiceIdentity(), "execution_started": false, "completed": false}
			break
		}
		scope := ""
		if len(args) == 3 {
			scope = args[2]
		}
		key := app.ContributionIntentKey(entry.contributionLoop, args[1], scope)
		c, e := s.app.AdmitContributionAs(ctx, entry.subject, entry.id, entry.contributionLoop, args[1], key, scope)
		err = e
		if err == nil {
			s.mu.Lock()
			current, ok := s.sessions[entry.id]
			if ok && current.token == entry.token && s.now().Before(current.expires) {
				current.contributionKey = c.Key
				s.sessions[entry.id] = current
			} else {
				err = app.ErrExpired
			}
			s.mu.Unlock()
			if err != nil {
				return result, err
			}
			if found, e := s.contributionTerminal(ctx, entry, c, &result); e != nil {
				return result, e
			} else if found {
				break
			}
			journey, executeErr := s.app.ExecuteContributionJourneyAs(ctx, entry.subject, c.Key)
			execution := journey.Execution
			if found, e := s.contributionTerminalAfterJourney(ctx, entry, c, execution, executeErr, &result); e != nil {
				return result, e
			} else if found {
				err = nil
				break
			}
			result.State, result.Reason = "accepted", execution.Reason
			if journey.HeldReason != "" {
				result.State, result.Reason = "unavailable", journey.HeldReason
			}
			if execution.Available && execution.Candidate == nil && (execution.Execution == nil || len(execution.Execution.Attempts) == 0) {
				result.State = "blocked"
			}
			result.Data = map[string]any{"contract_digest": c.Digest, "queue_item_id": c.QueueItemID, "run_id": c.RunID, "loop": c.Loop, "effect": c.Effect, "deadline": c.Deadline, "execution": execution, "delivery": journey.Delivery, "held_stage": journey.HeldStage, "held_reason": journey.HeldReason, "execution_started": execution.Execution != nil && len(execution.Execution.Attempts) > 0, "completed": false}
		}
	case "status", "resume":
		ref := ""
		if len(args) == 2 {
			ref = args[1]
		}
		if ref == "" && entry.contributionKey != "" {
			ref = entry.contributionKey
		}
		contract, resolveErr := s.app.ResolveContributionNavigationAs(ctx, entry.subject, ref)
		if resolveErr != nil {
			return result, resolveErr
		}
		key := contract.Key
		s.mu.Lock()
		current, ok := s.sessions[entry.id]
		if ok && current.token == entry.token && s.now().Before(current.expires) {
			current.contributionKey = key
			current.contributionLoop = contract.Loop
			s.sessions[entry.id] = current
		} else {
			err = app.ErrExpired
		}
		s.mu.Unlock()
		if err != nil {
			return result, err
		}
		if found, e := s.contributionTerminal(ctx, entry, contract, &result); e != nil {
			return result, e
		} else if found {
			if args[0] == "status" {
				if e = s.contributionContractStatus(ctx, entry, contract, &result); e != nil {
					return result, e
				}
			}
			break
		}
		continuity, readErr := s.app.ReadContributionContinuityViewAs(ctx, entry.subject, key)
		if readErr != nil {
			return result, readErr
		}
		if args[0] == "status" {
			continuity, continuityErr := s.app.ReadContributionContinuityViewAs(ctx, entry.subject, key)
			if continuityErr != nil {
				return result, continuityErr
			}
			result.State, result.Reason = "observed", "durable_contribution_readback"
			result.Data = map[string]any{"contract_digest": contract.Digest, "queue_item_id": contract.QueueItemID, "run_id": contract.RunID, "loop": contract.Loop, "continuity": continuity, "held_reason": continuity.HeldReason, "completed": false}
			// Identity-only navigation may observe the safe retained projection;
			// legacy execution readback retains its generic freshness admission.
			if s.app.RequirePrincipal(entry.subject) != nil {
				break
			}
			execution, executionErr := s.app.ContributionExecutionStatusAs(ctx, entry.subject, key)
			if executionErr != nil {
				return result, executionErr
			}
			if execution.Reason != "" {
				continuity.HeldReason = execution.Reason
			}
			result.Data = map[string]any{"contract_digest": contract.Digest, "queue_item_id": contract.QueueItemID, "run_id": contract.RunID, "loop": contract.Loop, "execution_available": execution.Available, "execution": execution, "continuity": continuity, "held_reason": continuity.HeldReason, "completed": false}
			if e := s.contributionContractStatus(ctx, entry, contract, &result); e != nil {
				return result, e
			}
			break
		}
		result.Data = map[string]any{"continuity": continuity, "held_reason": continuity.HeldReason, "completed": false}
		{
			journey, journeyErr := s.app.ResumeContributionJourneyAs(ctx, entry.subject, key)
			execution := journey.Execution
			// contract is the immutable exact key resolved before Resume. The
			// terminal getter independently reloads it; no caller-context read or
			// journey-error return may bypass bounded metadata reconciliation.
			if found, e := s.contributionTerminalAfterJourney(ctx, entry, contract, execution, journeyErr, &result); e != nil {
				return result, e
			} else if found {
				err = nil
				break
			}
			s.mu.Lock()
			current, ok := s.sessions[entry.id]
			if ok && current.token == entry.token {
				current.contributionKey = key
				current.contributionLoop = contract.Loop
				s.sessions[entry.id] = current
			}
			s.mu.Unlock()
			result.State, result.Reason = "accepted", execution.Reason
			continuity, readErr = s.app.ReadContributionContinuityViewAs(ctx, entry.subject, key)
			if readErr != nil {
				return result, readErr
			}
			result.Data = map[string]any{"execution": execution, "continuity": continuity, "delivery": journey.Delivery, "held_stage": journey.HeldStage, "held_reason": journey.HeldReason, "completed": false}
		}
	case "stop", "narrow":
		key := entry.contributionKey
		if len(args) == 2 && args[0] != "narrow" {
			key = args[1]
		}
		c, view, e := s.app.ReadContributionAs(ctx, entry.subject, key)
		err = e
		if err == nil && args[0] == "stop" {
			err = s.app.StopContributionInSessionAs(ctx, entry.subject, entry.id, key)
			if err == nil {
				c, view, err = s.app.ReadContributionAs(ctx, entry.subject, key)
			}
		}
		if err == nil && args[0] == "narrow" {
			err = s.app.ControlContributionAs(ctx, entry.subject, entry.id, key, false, args[1])
		}
		if err == nil {
			control, controlErr := s.app.ReadContributionControlAs(ctx, entry.subject, key)
			if controlErr != nil {
				return result, controlErr
			}
			target := c.Effect
			if control.Sequence > 0 {
				target = control.Delivery
			}
			execution, executionErr := s.app.ContributionExecutionStatusAs(ctx, entry.subject, key)
			if executionErr != nil {
				return result, executionErr
			}
			continuity, continuityErr := s.app.ReadContributionContinuityViewAs(ctx, entry.subject, key)
			if continuityErr != nil {
				return result, continuityErr
			}
			result.State, result.Reason = "observed", "durable_contribution_readback"
			result.Data = map[string]any{"contract_digest": c.Digest, "contract": c, "roles": c.Roles, "queue_item_id": c.QueueItemID, "run_id": c.RunID, "queue_state": view.Projection.State, "effect": target, "control_sequence": control.Sequence, "stopped": continuity.CancelAcknowledged, "cancel_requested": continuity.CancelRequested, "cancel_acknowledged": continuity.CancelAcknowledged, "execution_available": execution.Available, "execution": execution, "completed": false}
		}
	default:
		err = app.ErrDenied
	}
	result.FinishedAt = s.now()
	auditOutcome := result.State
	if auditOutcome == "blocked" {
		auditOutcome = "denied"
	}
	if auditOutcome == "selected" || auditOutcome == "observed" {
		auditOutcome = "completed"
	}
	auditCtx, cancelAudit := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancelAudit()
	if auditErr := s.app.AuditManagerCommand(auditCtx, entry.subject, "manager.contribution", auditOutcome, result.Reason, "", ""); auditErr != nil {
		return result, auditErr
	}
	return result, err
}
