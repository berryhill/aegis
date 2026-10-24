package app

import (
	"context"
	"errors"
	"os"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	"github.com/berryhill/aegis/internal/store"
)

// A different Run is not same-Queue interrupted-start recovery. Its predecessor
// must have a canonical terminal disposition and no admitted authority or
// unfinished session/binding. Process absence alone is never such proof.
func (s *Service) reserveNextLoopRun(ctx context.Context, sub core.Subject, in QueueLoopInput, result QueueLoopResult, agent registry.AgentRevision, parent string, fence map[string]string) (string, error) {
	if fence["queue_item_id"] == "" || fence["queue_item_id"] == result.QueueItemID || fence["agent_digest"] != agent.Digest {
		return "", ErrDenied
	}
	old, err := s.GetQueueItemAs(ctx, sub, fence["queue_item_id"])
	if err != nil {
		return "", err
	}
	if old.Projection.ActiveClaimID != "" || old.Disposition == nil || old.RuntimeAuthorityBinding == nil || old.Submission.OwnerID != sub.PrincipalID || old.Submission.OwnerAgentID != agent.AgentID || old.RuntimeAuthorityBinding.OwnerAgent != in.Agent || old.Disposition.GraphRunID != old.Item.GraphRunID || old.Disposition.QueueItem.ID != old.Item.ItemID || old.Disposition.QueueItem.Digest != old.Item.Digest {
		return "", ErrDenied
	}
	if (old.Projection.State != queue.StateFailed || old.Disposition.State != execution.StateFailed) && (old.Projection.State != queue.StateSucceeded || old.Disposition.State != execution.StateSucceeded) {
		return "", ErrDenied
	}
	// Retirement is Agent-scoped, but task budgets are not. A failed reusable
	// task requires exact budget-linked continuation only for that same task.
	reusable := false
	if old.SourceKind == graph.SourceDirectLoop {
		rev, e := s.FleetRepository.GetLoopRevision(ctx, old.Source.ID, old.Source.Revision)
		if e != nil || rev.Digest != old.Source.Digest {
			return "", ErrDenied
		}
		reusable = rev.SchemaVersion == loop.DoerReusableSchemaVersion || nativeCodingRevision(rev)
		if reusable && old.Projection.State == queue.StateFailed {
			if err := s.validateReusableNextTask(ctx, sub, in, result.RequestID, old, rev); err != nil {
				return "", err
			}
		}
	}
	// Failed Doer work may only continue through the explicitly budget-linked
	// intent; a new arbitrary key must not reset the same task's pass allowance.
	for _, child := range old.LoopExecutions {
		rev, e := s.FleetRepository.GetLoopRevision(ctx, child.Loop.ID, child.Loop.Revision)
		if e != nil {
			return "", e
		}

		if rev.Digest != child.Loop.Digest {
			return "", ErrDenied
		}
		if rev.SchemaVersion == loop.DoerRevisionSchemaVersion && !nativeCodingRevision(rev) && old.Projection.State == queue.StateFailed {
			if in.Reexecution == nil || in.Reexecution.PreviousQueue.ID != old.Item.ItemID || in.Reexecution.PreviousQueue.Digest != old.Item.Digest || in.Reexecution.PreviousDisposition.ID != old.Disposition.DispositionID || in.Reexecution.PreviousDisposition.Digest != old.Disposition.Digest {
				return "", ErrDenied
			}
		}
	}
	binding := old.RuntimeAuthorityBinding
	authority, err := s.Authority.GetAuthorityContext(ctx, binding.Authority.ID)
	if err != nil || authority.Digest != binding.Authority.Digest || authority.SubjectID != sub.ID || authority.AgentID != agent.AgentID || authority.CharterDigest != agent.Charter.Digest {
		return "", ErrDenied
	}
	session, err := s.GetSession(authority.SessionID)
	if err != nil || session.Mandate.ID != authority.MandateID || core.ValidateAuthorityContext(authority, session.Mandate) != nil {
		return "", ErrDenied
	}
	admission, err := s.AuthorityCommands.AuthorityAdmission(ctx, authority.ID, authority.Digest, s.Now())
	if err != nil {
		return "", ErrDenied
	}
	if admission.Admitted && session.Status != "running" {
		return "", ErrDenied
	}
	if reusable || admission.Admitted || session.Status == "running" {
		// Validate the entire predecessor before the first lifecycle mutation.
		if !s.completedReusablePredecessor(ctx, sub, in, agent, parent, fence, old, authority, session) {
			return "", ErrDenied
		}
	}
	if err = s.predecessorAuthorityIdle(ctx, binding.Authority); err != nil {
		return "", err
	}
	if admission.Admitted || session.Status == "running" {
		if err = s.TerminateSessionAs(ctx, sub, session.ID, "terminal_reusable_run"); err != nil {
			return "", err
		}
	}
	// Supported lifecycle success is necessary but not sufficient: independently
	// reload both authority admission and the durable terminal session.
	admission, err = s.AuthorityCommands.AuthorityAdmission(ctx, authority.ID, authority.Digest, s.Now())
	if err != nil || admission.Admitted {
		return "", ErrDenied
	}
	session, err = s.GetSession(authority.SessionID)
	if err != nil {
		return "", err
	}
	switch session.Status {
	case "failed", "terminated", "expired", "revoked":
	default:
		return "", ErrDenied
	}
	next := preparationNextID(parent)
	child := map[string]string{"queue_item_id": result.QueueItemID, "agent_digest": agent.Digest, "subject_id": sub.ID, "request_id": result.RequestID, "loop_digest": in.Loop.Digest, "parent_reservation": parent, "previous_queue_item_id": old.Item.ItemID, "previous_disposition_digest": old.Disposition.Digest}
	if err = s.Store.Create("queue-session-preparation", next, child); err != nil {
		return "", err
	}
	// Preserve the old authority and fence forever; this exact retirement proof
	// exempts only already inadmissible terminal authority from new preparation.
	f := sessionPreparationFact{ReservationID: parent, QueueItemID: old.Item.ItemID, RequestID: old.Submission.IdempotencyKey, AgentDigest: agent.Digest, LoopDigest: old.Source.Digest, SubjectID: sub.ID, MandateID: authority.MandateID, AuthorityID: authority.ID, Stage: "terminal_run", RetrySafe: true, TransitionID: old.Projection.LastTransitionID, TransitionDigest: old.Disposition.Digest, CompletedAt: s.Now()}
	f.Digest = preparationFactDigest(f)
	err = s.Store.Create("queue-session-preparation-retired", authority.ID, f)
	if errors.Is(err, store.ErrAlreadyExists) {
		var prior sessionPreparationFact
		if e := s.Store.Load("queue-session-preparation-retired", authority.ID, &prior); e != nil || prior.AuthorityID != authority.ID || prior.Digest != preparationFactDigest(prior) {
			return "", ErrDenied
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err = s.audit(ctx, core.AuditEvent{Type: "doer.session.next-run", SubjectID: sub.ID, PrincipalID: sub.PrincipalID, AgentID: agent.AgentID, Outcome: "authorized", Reason: "known_terminal_inadmissible_authority", Metadata: map[string]string{"request_id": result.RequestID, "previous_queue_id": old.Item.ItemID, "reservation_id": next}}); err != nil {
		return "", err
	}
	return next, nil
}

// Called with loopRunMu held, also shared by public submission and binding.
// Fresh authority admission still gates every subsequent worker effect.
func (s *Service) predecessorAuthorityIdle(ctx context.Context, authority reference.DigestRef) error {
	items, err := s.FleetRepository.ListQueueItems(ctx)
	if err != nil {
		return err
	}
	for _, item := range items {
		b, e := s.FleetRepository.GetQueueRuntimeBinding(ctx, item.ItemID)
		shared := e == nil && b.Authority == authority
		if errors.Is(e, fleet.ErrNotFound) {
			submission, loadErr := s.FleetRepository.GetSubmission(ctx, item.Submission.ID)
			if loadErr != nil || submission.Digest != item.Submission.Digest {
				return ErrDenied
			}
			shared = submission.Authority == authority
		} else if e != nil {
			return e
		}
		if !shared {
			continue
		}
		p, e := s.FleetRepository.GetQueueProjection(ctx, item.ItemID)
		if e != nil {
			return e
		}
		switch p.State {
		case queue.StateSucceeded, queue.StateFailed, queue.StateDenied, queue.StateCancelled, queue.StateExpired, queue.StateRevoked:
		default:
			return ErrDenied
		}
		if p.ActiveClaimID != "" {
			return ErrDenied
		}
	}
	return nil
}

// Only an exactly owned terminal direct v5 or native CODING v4 predecessor
// is eligible for cleanup. Legacy selected-file v4 is deliberately unchanged.
// Failure-budget admission is separate and precedes this lifecycle predicate.
func (s *Service) completedReusablePredecessor(ctx context.Context, sub core.Subject, in QueueLoopInput, agent registry.AgentRevision, parent string, fence map[string]string, old QueueExecutionView, authority core.AuthorityContext, session core.Session) bool {
	if old.SourceKind != graph.SourceDirectLoop || old.Projection.ActiveClaimID != "" || old.Disposition == nil || old.RuntimeAuthorityBinding == nil || len(old.LoopExecutions) != 1 || len(old.Attempts) != 1 || len(old.Claims) != 1 || s.loopRunWorkers[old.Item.ItemID] {
		return false
	}
	if (old.Projection.State != queue.StateSucceeded || old.Disposition.State != execution.StateSucceeded) && (old.Projection.State != queue.StateFailed || old.Disposition.State != execution.StateFailed) {
		return false
	}
	rev, err := s.FleetRepository.GetLoopRevision(ctx, old.Source.ID, old.Source.Revision)
	if err != nil || rev.Digest != old.Source.Digest || (rev.SchemaVersion != loop.DoerReusableSchemaVersion && !nativeCodingRevision(rev)) {
		return false
	}
	if nativeCodingRevision(rev) {
		a := authority.Authority
		h := a.Hermes
		if session.Mandate.Target != "aegis-owned-ephemeral" || len(a.Capabilities) != 0 || len(a.Tools) != 0 || len(a.Memory) != 0 || len(a.Credentials) != 0 || h.PersistentHome || h.Profile != "" || len(h.Toolsets) != 0 || len(h.MCPServers) != 0 || len(h.Plugins) != 0 {
			return false
		}
	}
	if fence["queue_item_id"] != old.Item.ItemID || fence["agent_digest"] != agent.Digest || (fence["subject_id"] != "" && fence["subject_id"] != sub.ID) || (fence["loop_digest"] != "" && fence["loop_digest"] != old.Source.Digest) {
		return false
	}
	var prepared reference.DigestRef
	b := old.RuntimeAuthorityBinding
	if s.Store.Load("queue-session-prepared", parent, &prepared) != nil || prepared != b.Authority || b.OwnerAgent != in.Agent || b.QueueItem.ID != old.Item.ItemID || b.QueueItem.Digest != old.Item.Digest || b.Submission != old.Item.Submission || b.MandateID != authority.MandateID || b.Runtime != session.Mandate.Runtime.Runtime {
		return false
	}
	if old.Submission.OwnerID != sub.PrincipalID || old.Submission.OwnerAgentID != agent.AgentID || authority.SubjectID != sub.ID || authority.AgentID != agent.AgentID || authority.CharterDigest != agent.Charter.Digest || session.ID != authority.SessionID || session.Mandate.Subject.ID != sub.ID || session.Mandate.Subject.PrincipalID != sub.PrincipalID || session.Mandate.AgentID != agent.AgentID || session.Mandate.CharterDigest != agent.Charter.Digest || session.Mandate.CharterRevision != agent.Charter.Revision {
		return false
	}
	child, attempt, claim, d := old.LoopExecutions[0], old.Attempts[0], old.Claims[0], old.Disposition
	if child.Loop != old.Source || child.Participant != in.Agent || child.GraphRunID != old.Item.GraphRunID || d.GraphRunID != child.GraphRunID || d.LoopExecutionID != child.LoopExecutionID || d.AttemptID != attempt.AttemptID || attempt.LoopExecutionID != child.LoopExecutionID || attempt.GraphRunID != child.GraphRunID || attempt.QueueItem != b.QueueItem || attempt.ClaimID != claim.ClaimID || claim.AttemptID != attempt.AttemptID || claim.QueueItem != b.QueueItem || claim.Authority != b.Authority || d.Authority != b.Authority || d.QueueItem != b.QueueItem {
		return false
	}
	// Execution/attempt records are immutable start facts. Their terminal
	// evidence is the exact disposition and canonical terminal transition.
	for _, tr := range old.Transitions {
		if tr.TransitionID == old.Projection.LastTransitionID && tr.From == queue.StateClaimed && tr.To == old.Projection.State && tr.ClaimID == claim.ClaimID && tr.OccurredAt.Equal(d.OccurredAt) {
			return true
		}
	}
	return false
}
