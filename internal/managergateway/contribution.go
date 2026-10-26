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
				result.Data = map[string]any{"loop": selected, "execution_started": false}
			}
		}
	case "run":
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
			if ok && current.token == entry.token {
				current.contributionKey = c.Key
				s.sessions[entry.id] = current
			}
			s.mu.Unlock()
			journey, executeErr := s.app.ExecuteContributionJourneyAs(ctx, entry.subject, c.Key)
			execution := journey.Execution
			err = executeErr
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
		contract, resolveErr := s.app.ResolveContributionAs(ctx, entry.subject, ref)
		if resolveErr != nil {
			return result, resolveErr
		}
		key := contract.Key
		continuity, readErr := s.app.ReadContributionContinuityViewAs(ctx, entry.subject, key)
		if readErr != nil {
			return result, readErr
		}
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
			break
		}
		result.Data = map[string]any{"continuity": continuity, "held_reason": continuity.HeldReason, "completed": false}
		execution, e := s.app.ResumeContributionAs(ctx, entry.subject, key)
		err = e
		if err == nil {
			contract, _, readErr := s.app.ReadContributionAs(ctx, entry.subject, key)
			if readErr != nil {
				return result, readErr
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
			result.Data = map[string]any{"execution": execution, "continuity": continuity, "held_reason": continuity.HeldReason, "completed": false}
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
