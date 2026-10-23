package app

import (
	"context"
	"errors"
	"os"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	"github.com/berryhill/aegis/internal/store"
)

// This is a returned, durably recorded pre-launch refusal, not an unknown
// interrupted start. Reconcile the entire authority footprint before creating
// a child reservation; never clear a fence or infer safety from process absence.
func (s *Service) recoverCompletedPrelaunchBlock(ctx context.Context, sub core.Subject, input QueueLoopInput, result QueueLoopResult, agent registry.AgentRevision, reservation string, fence map[string]string, view QueueExecutionView, tr queue.QueueTransition) (string, *sessionPreparationFact, error) {
	if fence["request_id"] != result.RequestID || fence["subject_id"] != sub.ID || fence["loop_digest"] != input.Loop.Digest || tr.From != queue.StatePreparationPending || tr.To != queue.StatePreparationPending || tr.Reason != "prior_runtime_authority_requires_explicit_recovery" || tr.TransitionID != view.Projection.LastTransitionID || tr.OccurredAt.After(s.Now()) {
		return "", nil, ErrDenied
	}
	var prepared reference.DigestRef
	if err := s.Store.Load("queue-session-prepared", reservation, &prepared); !errors.Is(err, os.ErrNotExist) {
		return "", nil, ErrDenied
	}
	contexts, err := s.Authority.ListAuthorityContexts(ctx)
	if err != nil {
		return "", nil, err
	}
	knownMandates := map[string]bool{}
	for _, authority := range contexts {
		if authority.SubjectID != sub.ID || authority.AgentID != agent.AgentID || authority.CharterDigest != agent.Charter.Digest {
			continue
		}
		if s.completedProviderProbeAuthority(ctx, sub, agent, authority) {
			knownMandates[authority.MandateID] = true
			continue
		}
		var retired sessionPreparationFact
		if s.Store.Load("queue-session-preparation-retired", authority.ID, &retired) != nil || retired.AuthorityID != authority.ID || retired.AgentDigest != agent.Digest || retired.SubjectID != sub.ID || retired.Digest != preparationFactDigest(retired) {
			return "", nil, ErrDenied
		}
		admission, err := s.AuthorityCommands.AuthorityAdmission(ctx, authority.ID, authority.Digest, s.Now())
		if err != nil || admission.Admitted {
			return "", nil, ErrDenied
		}
		session, err := s.GetSession(authority.SessionID)
		if err != nil || session.EndedAt == nil || core.ValidateAuthorityContext(authority, session.Mandate) != nil {
			return "", nil, ErrDenied
		}
		switch session.Status {
		case "failed", "terminated", "expired", "revoked":
		default:
			return "", nil, ErrDenied
		}
		if s.predecessorAuthorityIdle(ctx, reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: authority.ID, Digest: authority.Digest}) != nil {
			return "", nil, ErrDenied
		}
		knownMandates[authority.MandateID] = true
	}
	mandates, err := s.Authority.ListMandates(ctx)
	if err != nil {
		return "", nil, err
	}
	for _, mandate := range mandates {
		if mandate.Subject.ID == sub.ID && mandate.AgentID == agent.AgentID && mandate.CharterDigest == agent.Charter.Digest && !mandate.IssuedAt.Before(view.Item.EnqueuedAt) && !knownMandates[mandate.ID] {
			return "", nil, ErrDenied
		}
	}
	fact := sessionPreparationFact{ReservationID: reservation, QueueItemID: result.QueueItemID, RequestID: result.RequestID, AgentDigest: agent.Digest, LoopDigest: input.Loop.Digest, SubjectID: sub.ID, Stage: "prelaunch_authority_readiness", RetrySafe: true, TransitionID: tr.TransitionID, TransitionDigest: tr.Digest, CompletedAt: tr.OccurredAt}
	fact.Digest = preparationFactDigest(fact)
	if err = s.Store.Create("queue-session-preparation-failed", reservation, fact); err != nil {
		var prior sessionPreparationFact
		if !errors.Is(err, store.ErrAlreadyExists) || s.Store.Load("queue-session-preparation-failed", reservation, &prior) != nil || prior != fact {
			return "", nil, ErrDenied
		}
	}
	next := preparationNextID(reservation)
	child := map[string]string{"queue_item_id": result.QueueItemID, "agent_digest": agent.Digest, "subject_id": sub.ID, "request_id": result.RequestID, "loop_digest": input.Loop.Digest, "parent_reservation": reservation}
	if err = s.Store.Create("queue-session-preparation", next, child); err != nil {
		return "", nil, err
	}
	if err = s.audit(ctx, core.AuditEvent{Type: "doer.session.prelaunch-recovery", SubjectID: sub.ID, PrincipalID: sub.PrincipalID, AgentID: agent.AgentID, Outcome: "authorized", Reason: "completed_prelaunch_refusal_no_runtime_effect", Metadata: map[string]string{"request_id": result.RequestID, "preparation_digest": fact.Digest}}); err != nil {
		return "", nil, err
	}
	return next, &fact, nil
}
