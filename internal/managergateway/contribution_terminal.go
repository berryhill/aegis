package managergateway

import (
	"context"
	"errors"
	"time"

	"github.com/berryhill/aegis/internal/app"

	"github.com/berryhill/aegis/internal/slash"
)

// A completed service-owned native run may outlive its transport request.
// Only its metadata notification gets a short read context; admission and all
// effect paths retain their existing contexts and original authority expiry.
func (s *Service) contributionTerminalAfterOwnedRun(ctx context.Context, entry session, c app.ContributionNavigationContract, execution app.ContributionExecutionResult, result *slash.Result) (bool, error) {
	if !errors.Is(ctx.Err(), context.Canceled) || execution.Candidate == nil || execution.Candidate.State != "candidate_accepted" {
		return s.contributionTerminal(ctx, entry, c, result)
	}
	deadline := c.Deadline
	for _, bound := range []time.Time{entry.expires, entry.subject.ExpiresAt} {
		if bound.Before(deadline) {
			deadline = bound
		}
	}
	read, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	read, cancelBound := context.WithDeadline(read, deadline)
	defer cancelBound()
	return s.contributionTerminal(read, entry, c, result)
}

// Terminal navigation is metadata-only and never continues native/delivery.
// Every non-not-found error is returned, including corrupt or foreign evidence.
func (s *Service) contributionTerminal(ctx context.Context, entry session, c app.ContributionNavigationContract, result *slash.Result) (bool, error) {
	s.mu.Lock()
	current, ok := s.sessions[entry.id]
	valid := ok && current.token == entry.token && current.subject.ID == entry.subject.ID && current.subject.PrincipalID == entry.subject.PrincipalID && s.now().Before(current.expires)
	s.mu.Unlock()
	if !valid {
		return false, app.ErrExpired
	}
	terminal, err := s.app.GetFinalContributionResultAs(ctx, entry.subject, c.Key)
	if app.IsContributionResultNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	whole := terminal.Report.Outcome == "succeeded"
	bounded := terminal.Report.Outcome == "narrower-target-completed"
	result.State, result.Reason = string(terminal.Disposition.State), terminal.Report.Reason
	if whole || bounded {
		result.State = "completed"
	}
	result.Data = map[string]any{"contract_digest": c.Digest, "queue_item_id": c.QueueItemID, "run_id": c.RunID, "loop": c.Loop, "effect": terminal.Report.Scope, "terminal": true, "completed": whole || bounded, "whole_contribution_succeeded": whole, "boundary_completed": whole || bounded, "outcome": terminal.Report.Outcome, "result": terminal, "disposition": terminal.Disposition, "report": terminal.Report, "artifact": terminal.Artifact, "receipts": terminal.Receipts, "execution_started": true}
	return true, nil
}
