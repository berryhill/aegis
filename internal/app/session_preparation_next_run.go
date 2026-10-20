package app

import (
	"context"
	"errors"
	"os"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
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
	// Failed Doer work may only continue through the explicitly budget-linked
	// intent; a new arbitrary key must not reset the same task's pass allowance.
	for _, child := range old.LoopExecutions {
		rev, e := s.FleetRepository.GetLoopRevision(ctx, child.Loop.ID, child.Loop.Revision)
		if e != nil {
			return "", e
		}
		if rev.SchemaVersion == loop.DoerRevisionSchemaVersion && old.Projection.State == queue.StateFailed {
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
	admission, err := s.AuthorityCommands.AuthorityAdmission(ctx, authority.ID, authority.Digest, s.Now())
	if err != nil || admission.Admitted {
		return "", ErrDenied
	}
	session, err := s.GetSession(authority.SessionID)
	if err != nil || session.Mandate.ID != authority.MandateID || core.ValidateAuthorityContext(authority, session.Mandate) != nil {
		return "", ErrDenied
	}
	switch session.Status {
	case "failed", "terminated", "expired", "revoked":
	default:
		return "", ErrDenied
	}
	// No other queued/preparing/claimed work may still use this authority.
	items, err := s.FleetRepository.ListQueueItems(ctx)
	if err != nil {
		return "", err
	}
	for _, item := range items {
		b, e := s.FleetRepository.GetQueueRuntimeBinding(ctx, item.ItemID)
		if errors.Is(e, fleet.ErrNotFound) {
			continue
		}
		if e != nil {
			return "", e
		}
		if b.Authority != binding.Authority {
			continue
		}
		p, e := s.FleetRepository.GetQueueProjection(ctx, item.ItemID)
		if e != nil || p.State.IsPreparation() || p.State == queue.StateQueued || p.State == queue.StateClaimed || p.ActiveClaimID != "" {
			return "", ErrDenied
		}
	}
	next := preparationNextID(parent)
	child := map[string]string{"queue_item_id": result.QueueItemID, "agent_digest": agent.Digest, "subject_id": sub.ID, "request_id": result.RequestID, "loop_digest": in.Loop.Digest, "parent_reservation": parent, "previous_queue_item_id": old.Item.ItemID, "previous_disposition_digest": old.Disposition.Digest}
	if err = s.Store.Create("queue-session-preparation", next, child); err != nil {
		return "", err
	}
	// Preserve the old authority and fence forever; this exact retirement proof
	// exempts only already inadmissible terminal authority from new preparation.
	f := sessionPreparationFact{ReservationID: parent, QueueItemID: old.Item.ItemID, RequestID: old.Submission.IdempotencyKey, AgentDigest: agent.Digest, LoopDigest: in.Loop.Digest, SubjectID: sub.ID, MandateID: authority.MandateID, AuthorityID: authority.ID, Stage: "terminal_run", RetrySafe: true, TransitionID: old.Projection.LastTransitionID, TransitionDigest: old.Disposition.Digest, CompletedAt: s.Now()}
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
