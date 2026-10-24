package managergateway

import (
	"context"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/slash"
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
			result.State, result.Reason = "accepted", "intake_accepted_execution_separate"
			result.Data = map[string]any{"contract_digest": c.Digest, "queue_item_id": c.QueueItemID, "run_id": c.RunID, "loop": c.Loop, "effect": c.Effect, "deadline": c.Deadline, "execution_started": false, "completed": false}
		}
	case "status", "stop", "narrow":
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
			result.State, result.Reason = "observed", "durable_contribution_readback"
			result.Data = map[string]any{"contract_digest": c.Digest, "contract": c, "roles": c.Roles, "queue_item_id": c.QueueItemID, "run_id": c.RunID, "queue_state": view.Projection.State, "effect": target, "control_sequence": control.Sequence, "stopped": control.Stop, "execution_available": false, "completed": false}
		}
	default:
		err = app.ErrDenied
	}
	result.FinishedAt = s.now()
	auditOutcome := result.State
	if auditOutcome == "selected" || auditOutcome == "observed" {
		auditOutcome = "completed"
	}
	if auditErr := s.app.AuditManagerCommand(ctx, entry.subject, "manager.contribution", auditOutcome, result.Reason, "", ""); auditErr != nil {
		return result, auditErr
	}
	return result, err
}
