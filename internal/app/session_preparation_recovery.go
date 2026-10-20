package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"slices"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/registry"
)

// A closed diagnostic, never runtime output, provider stderr, or custody errors.
type sessionStartFailure struct {
	Stage, MandateID, AuthorityID string
	RetrySafe                     bool
	cause                         error
}

func (e *sessionStartFailure) Error() string { return "session_start_" + e.Stage + "_failed" }
func (e *sessionStartFailure) Unwrap() error { return e.cause }

// Immutable completion proof, separate from (and never a rewrite of) the fence.
// RetrySafe is emitted only after no launch was attempted or the launcher has
// synchronously reaped its exact process. Unknown outcomes have no such proof.
type sessionPreparationFact struct {
	ReservationID    string    `json:"reservation_id"`
	QueueItemID      string    `json:"queue_item_id"`
	RequestID        string    `json:"request_id"`
	AgentDigest      string    `json:"agent_digest"`
	LoopDigest       string    `json:"loop_digest"`
	SubjectID        string    `json:"subject_id"`
	MandateID        string    `json:"mandate_id"`
	AuthorityID      string    `json:"authority_id,omitempty"`
	Stage            string    `json:"stage"`
	RetrySafe        bool      `json:"retry_safe"`
	TransitionID     string    `json:"transition_id"`
	TransitionDigest string    `json:"transition_digest"`
	CompletedAt      time.Time `json:"completed_at"`
	Digest           string    `json:"digest"`
}

func preparationFactDigest(f sessionPreparationFact) string { f.Digest = ""; return core.Digest(f) }
func preparationNextID(parent string) string {
	sum := sha256.Sum256([]byte(parent + "\x00session-preparation-retry-v1"))
	return hex.EncodeToString(sum[:])
}

// Follow at most two append-only retry generations. A newly reserved generation
// with no completion proof remains closed; neither timeouts nor age release it.
func (s *Service) preparationReservation(root string) (string, map[string]string, error) {
	id := root
	for generation := 0; generation <= 256; generation++ {
		var f map[string]string
		if err := s.Store.Load("queue-session-preparation", id, &f); err != nil {
			return id, nil, err
		}
		var next map[string]string
		nextID := preparationNextID(id)
		err := s.Store.Load("queue-session-preparation", nextID, &next)
		if errors.Is(err, os.ErrNotExist) {
			return id, f, nil
		}
		if err != nil || generation == 256 || next["parent_reservation"] != id || (next["queue_item_id"] != f["queue_item_id"] && (next["previous_queue_item_id"] != f["queue_item_id"] || next["previous_disposition_digest"] == "")) || next["agent_digest"] != f["agent_digest"] {
			return "", nil, ErrDenied
		}
		id = nextID
	}
	return "", nil, ErrDenied
}

func (s *Service) recordSessionPreparationFailure(ctx context.Context, subject core.Subject, input QueueLoopInput, result QueueLoopResult, reservation string, startErr error) (QueueLoopResult, error) {
	failed, err := s.queueLoopBlockedReadback(ctx, subject, result, "session_start_preparation_failed")
	if err != nil {
		return failed, err
	}
	var se *sessionStartFailure
	if !errors.As(startErr, &se) || failed.Execution == nil {
		return failed, nil
	}
	v := failed.Execution
	if len(v.Transitions) == 0 {
		return failed, ErrDenied
	}
	tr := v.Transitions[len(v.Transitions)-1]
	f := sessionPreparationFact{ReservationID: reservation, QueueItemID: result.QueueItemID, RequestID: result.RequestID, AgentDigest: input.Agent.Digest, LoopDigest: input.Loop.Digest, SubjectID: subject.ID, MandateID: se.MandateID, AuthorityID: se.AuthorityID, Stage: se.Stage, RetrySafe: se.RetrySafe, TransitionID: tr.TransitionID, TransitionDigest: tr.Digest, CompletedAt: s.Now()}
	f.Digest = preparationFactDigest(f)
	if err = s.Store.Create("queue-session-preparation-failed", reservation, f); err != nil {
		return failed, err
	}
	failed.Preparation = &f
	return failed, nil
}

// Legacy fences did not retain mandate identity. Infer it ONLY from one exact
// authoritative mandate in the accepted Queue's completed failure interval, and
// ONLY for the deterministically reproducible pre-launch nil/empty defect. No
// context may have been issued: activation always precedes launch. Do not adopt
// an arbitrary unbound context, infer process absence, or trust a generic error.
func (s *Service) legacyPreparationFailure(ctx context.Context, sub core.Subject, input QueueLoopInput, result QueueLoopResult, reservation string, agent registry.AgentRevision, v QueueExecutionView, tr queue.QueueTransition) (sessionPreparationFact, error) {
	all, err := s.Authority.ListMandates(ctx)
	if err != nil {
		return sessionPreparationFact{}, err
	}
	var m core.Mandate
	for _, candidate := range all {
		if candidate.Subject.ID == sub.ID && candidate.AgentID == input.Agent.ID && candidate.CharterRevision == agent.Charter.Revision && candidate.CharterDigest == agent.Charter.Digest && !candidate.IssuedAt.Before(v.Item.EnqueuedAt) && !candidate.IssuedAt.After(tr.OccurredAt) {
			if m.ID != "" {
				return sessionPreparationFact{}, ErrDenied
			}
			m = candidate
		}
	}
	if m.ID == "" || s.validateMandateAt(m, m.IssuedAt) == nil {
		return sessionPreparationFact{}, ErrDenied
	}
	c, err := s.GetCharter(m.AgentID, m.CharterRevision)
	if err != nil {
		return sessionPreparationFact{}, err
	}
	repaired := m
	for _, st := range c.Charter.Stanzas {
		if st.ID == m.StanzaID {
			if len(st.Grant.Capabilities) != 0 || len(st.Grant.Tools) != 0 || len(m.Capabilities) != 0 || len(m.Tools) != 0 {
				return sessionPreparationFact{}, ErrDenied
			}
			repaired.Capabilities = slices.Clone(st.Grant.Capabilities)
			repaired.Tools = slices.Clone(st.Grant.Tools)
		}
	}
	if core.Digest(repaired) == core.Digest(m) || s.validateMandateAt(repaired, m.IssuedAt) != nil {
		return sessionPreparationFact{}, ErrDenied
	}
	f := sessionPreparationFact{ReservationID: reservation, QueueItemID: result.QueueItemID, RequestID: result.RequestID, AgentDigest: input.Agent.Digest, LoopDigest: input.Loop.Digest, SubjectID: sub.ID, MandateID: m.ID, Stage: "legacy_mandate_empty_slice_mismatch", RetrySafe: true, TransitionID: tr.TransitionID, TransitionDigest: tr.Digest, CompletedAt: tr.OccurredAt}
	f.Digest = preparationFactDigest(f)
	return f, nil
}

// Run itself is the authenticated retry boundary. It archives evidence and
// atomically creates a child denial fence; it never clears an old fence/lease,
// rewrites signed facts, substitutes Queue identity, or adds authority scope.
func (s *Service) recoverDoerSessionPreparation(ctx context.Context, sub core.Subject, input QueueLoopInput, result QueueLoopResult, lv LoopView, agent registry.AgentRevision, reservation string, fence map[string]string) (string, *sessionPreparationFact, error) {
	if lv.Revision.SchemaVersion != loop.DoerRevisionSchemaVersion || !s.doerEphemeralPreparationAllowed(ctx, sub, lv.Revision, agent) || fence["queue_item_id"] != result.QueueItemID || fence["agent_digest"] != agent.Digest {
		return "", nil, ErrDenied
	}
	rootSum := sha256.Sum256([]byte(sub.ID + "\x00" + agent.Digest))
	rootID := hex.EncodeToString(rootSum[:])
	if reservation != rootID && reservation != preparationNextID(rootID) {
		return "", nil, ErrDenied
	}
	v, err := s.GetQueueItemAs(ctx, sub, result.QueueItemID)
	if err != nil {
		return "", nil, err
	}
	if !v.Projection.State.IsPreparation() || v.Projection.Attempts != 0 || len(v.Claims) != 0 || len(v.Attempts) != 0 || len(v.LoopExecutions) != 0 || v.RuntimeAuthorityBinding != nil || v.Artifact != nil || len(v.Receipts) != 0 || v.Disposition != nil || len(v.Transitions) == 0 {
		return "", nil, ErrDenied
	}
	tr := v.Transitions[len(v.Transitions)-1]
	if tr.TransitionID != v.Projection.LastTransitionID || tr.Reason != "session_start_preparation_failed" {
		return "", nil, ErrDenied
	}
	var f sessionPreparationFact
	err = s.Store.Load("queue-session-preparation-failed", reservation, &f)
	legacy := errors.Is(err, os.ErrNotExist)
	if legacy {
		f, err = s.legacyPreparationFailure(ctx, sub, input, result, reservation, agent, v, tr)
	}
	if err != nil || !f.RetrySafe || f.ReservationID != reservation || f.QueueItemID != result.QueueItemID || f.RequestID != result.RequestID || f.AgentDigest != agent.Digest || f.LoopDigest != input.Loop.Digest || f.SubjectID != sub.ID || f.TransitionID != tr.TransitionID || f.TransitionDigest != tr.Digest || f.Digest != preparationFactDigest(f) {
		return "", nil, ErrDenied
	}
	switch f.Stage {
	case "mandate_validation", "provider_resolution", "legacy_mandate_empty_slice_mismatch":
		if f.AuthorityID != "" {
			return "", nil, ErrDenied
		}
	case "runtime_launch":
		if f.AuthorityID == "" {
			return "", nil, ErrDenied
		}
	default:
		return "", nil, ErrDenied
	}
	if f.CompletedAt.Before(tr.OccurredAt) || f.CompletedAt.After(s.Now()) {
		return "", nil, ErrDenied
	}
	m, err := s.Authority.GetMandate(ctx, f.MandateID)
	if err != nil || m.Subject.ID != sub.ID || m.AgentID != agent.AgentID || m.CharterDigest != agent.Charter.Digest || m.CharterRevision != agent.Charter.Revision {
		return "", nil, ErrDenied
	}
	contexts, err := s.Authority.ListAuthorityContexts(ctx)
	if err != nil {
		return "", nil, err
	}
	sessions, err := s.ListSessions()
	if err != nil {
		return "", nil, err
	}
	for _, session := range sessions {
		if session.Mandate.ID == m.ID {
			return "", nil, ErrDenied
		}
	}
	var old *core.AuthorityContext
	for i := range contexts {
		a := contexts[i]
		if a.MandateID == m.ID && (legacy || a.ID != f.AuthorityID || a.SubjectID != sub.ID || a.AgentID != agent.AgentID || a.CharterDigest != agent.Charter.Digest) {
			return "", nil, ErrDenied
		}
		if a.SubjectID != sub.ID || a.AgentID != agent.AgentID || a.CharterDigest != agent.Charter.Digest {
			continue
		}
		// Prior generations must already have an immutable recovery record proving
		// their retirement; never exempt unrelated historical authority.
		if a.ID != f.AuthorityID || a.MandateID != m.ID {
			var retired sessionPreparationFact
			if s.Store.Load("queue-session-preparation-retired", a.ID, &retired) != nil || retired.AuthorityID != a.ID || retired.QueueItemID != result.QueueItemID || retired.RequestID != result.RequestID || retired.AgentDigest != agent.Digest || retired.LoopDigest != input.Loop.Digest || retired.Digest != preparationFactDigest(retired) {
				return "", nil, ErrDenied
			}
			admission, e := s.AuthorityCommands.AuthorityAdmission(ctx, a.ID, a.Digest, s.Now())
			if e != nil || admission.Admitted {
				return "", nil, ErrDenied
			}
			continue
		}
		if legacy || old != nil {
			return "", nil, ErrDenied
		}
		copy := a
		old = &copy
	}
	if f.AuthorityID != "" && old == nil {
		return "", nil, ErrDenied
	}
	items, err := s.FleetRepository.ListQueueItems(ctx)
	if err != nil {
		return "", nil, err
	}
	if old != nil {
		for _, item := range items {
			binding, e := s.FleetRepository.GetQueueRuntimeBinding(ctx, item.ItemID)
			if e == nil && binding.Authority.ID == old.ID {
				return "", nil, ErrDenied
			}
			if e != nil && !errors.Is(e, fleet.ErrNotFound) {
				return "", nil, e
			}
		}
	}
	if old != nil {
		admission, e := s.AuthorityCommands.AuthorityAdmission(ctx, old.ID, old.Digest, s.Now())
		if e != nil || admission.Projection.State != core.AuthorityStateActive || (!admission.Admitted && (admission.ReasonCode != "outside_lifetime" || s.Now().Before(old.ExpiresAt))) {
			return "", nil, ErrDenied
		}
	}
	if legacy {
		if err = s.Store.Create("queue-session-preparation-failed", reservation, f); err != nil {
			return "", nil, err
		}
	}
	next := preparationNextID(reservation)
	// The child reservation is create-only and the single winner precedes any
	// revocation, fresh mandate or launch. A crash here remains closed.
	child := map[string]string{"queue_item_id": result.QueueItemID, "agent_digest": agent.Digest, "subject_id": sub.ID, "request_id": result.RequestID, "loop_digest": input.Loop.Digest, "parent_reservation": reservation, "failed_transition_id": tr.TransitionID, "failed_transition_digest": tr.Digest, "failure_digest": f.Digest}
	if err = s.Store.Create("queue-session-preparation", next, child); err != nil {
		return "", nil, err
	}
	if old != nil {
		if s.Now().Before(old.ExpiresAt) {
			if err = s.processAuthorityCommand(ctx, core.AuthorityCommandRevoke, *old, "failed_session_preparation_recovered"); err != nil {
				return "", nil, err
			}
		}
		admission, e := s.AuthorityCommands.AuthorityAdmission(ctx, old.ID, old.Digest, s.Now())
		if e != nil || admission.Admitted {
			return "", nil, ErrDenied
		}
		if err = s.Store.Create("queue-session-preparation-retired", old.ID, f); err != nil {
			return "", nil, err
		}
	}
	if err = s.audit(ctx, core.AuditEvent{Type: "doer.session.preparation.recovered", SubjectID: sub.ID, PrincipalID: sub.PrincipalID, AgentID: agent.AgentID, MandateID: m.ID, CharterDigest: agent.Charter.Digest, Outcome: "authorized", Reason: f.Stage, Metadata: map[string]string{"request_id": result.RequestID, "queue_item_id": result.QueueItemID, "failed_transition_id": tr.TransitionID, "failure_digest": f.Digest, "reservation_id": next}}); err != nil {
		return "", nil, err
	}
	return next, &f, nil
}
