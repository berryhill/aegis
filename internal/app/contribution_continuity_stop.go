package app

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/runtime/hermes"
	"time"
)

// StopContributionRoleAs durably fences a role BEFORE asking the owning adapter
// to stop it. Timeout/restart/crash leaves requested, never acknowledged. This
// does not end/reactivate sessions, release source custody, or admit replacement.
func (s *Service) StopContributionRoleAs(ctx context.Context, sub core.Subject, key, role string, in ContributionContinuityInput) (contribution.Continuity, error) {
	if err := s.requirePrincipal(sub); err != nil {
		return contribution.Continuity{}, err
	}
	c, state, _, err := s.ObserveContributionContinuityAs(ctx, sub, key)
	if err != nil {
		return state, err
	}
	return s.stopContributionRole(ctx, c, state, role, in)
}

// Service-owned cleanup uses the retained admitted contract and exact process
// identity, not an expired caller grant. It can only fence/stop, never resume.
func (s *Service) stopContributionRole(ctx context.Context, c contribution.Contract, state contribution.Continuity, role string, in ContributionContinuityInput) (contribution.Continuity, error) {
	var err error
	if in.ContractDigest != c.Digest || in.ExpectedSequence != state.Sequence || in.ExpectedDigest != state.Digest {
		return state, contribution.ErrContinuityConflict
	}
	r, ok := state.RoleAuthorities[role]
	if !ok || s.Hermes == nil {
		return state, ErrDenied
	}
	sess, err := s.GetSession(r.SessionID)
	if err != nil || sess.RuntimeSessionID != r.RuntimeSessionID || sess.Mandate.ID != r.MandateID {
		return state, ErrDenied
	}
	stop := contribution.RoleStop{Authority: r, Epoch: state.RoleEpochs[role], PID: sess.RuntimePID, ProcessStart: sess.ProcessStart}
	appendStop := func(kind string) error {
		e, eerr := contribution.SealContinuityEvent(contribution.ContinuityEvent{SchemaVersion: contribution.ContinuitySchema, QueueItemID: c.QueueItemID, ContractDigest: c.Digest, OwnerID: c.OwnerID, SessionID: r.SessionID, Sequence: state.Sequence + 1, PreviousDigest: state.Digest, Kind: kind, RoleStop: &stop, At: s.Now()})
		if eerr != nil {
			return eerr
		}
		next, eerr := s.FleetRepository.(fleet.ContributionContinuityRepository).AppendContributionContinuity(ctx, e)
		if eerr == nil {
			state = next
		}
		return eerr
	}
	if pending, exists := state.RoleStops[role]; exists {
		exact := pending
		exact.Acknowledged = false
		exact.BootID = ""
		if exact != stop {
			return state, contribution.ErrContinuityConflict
		}
		// Durable acknowledgement is authoritative readback, never another signal.
		if pending.Acknowledged {
			return state, nil
		}
	} else if err = appendStop("role-stop-requested"); err != nil {
		return state, err
	}
	// Only a real retained stop request permits service-owned safety cleanup.
	// This is not a new effect grant. Complete both Wait proof and its durable
	// acknowledgement before returning, even if the requesting transport vanishes.
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	ctx = cleanup
	binding := hermes.ContributionStopBinding{SessionID: r.SessionID, RuntimeSessionID: r.RuntimeSessionID, AuthorityID: r.Authority.ID, AuthorityDigest: r.Authority.Digest, Role: role, Epoch: stop.Epoch, PID: stop.PID, ProcessStart: stop.ProcessStart}
	proof, err := s.Hermes.StopContributionRole(ctx, binding)
	if err != nil {
		return state, err
	}
	if !proof.RoleProcessAcknowledged() || proof.Binding() != binding || proof.BootID() == "" {
		return state, ErrDenied
	}
	stop.Acknowledged = true
	stop.BootID = proof.BootID()
	// Keep the genuine adapter proof in this invocation while retrying only
	// the canonical acknowledgement CAS. Never re-signal for a head race.
	for attempts := 0; attempts < 16; attempts++ {
		err = appendStop("role-stop-acknowledged")
		if err == nil || !errors.Is(err, contribution.ErrContinuityConflict) {
			return state, err
		}
		fresh, _, readErr := s.FleetRepository.(fleet.ContributionContinuityRepository).GetContributionContinuity(ctx, c.QueueItemID)
		if readErr != nil {
			return state, readErr
		}
		pending, exists := fresh.RoleStops[role]
		if !exists {
			return fresh, contribution.ErrContinuityConflict
		}
		if pending == stop {
			return fresh, nil
		}
		expected := stop
		expected.Acknowledged = false
		expected.BootID = ""
		if pending != expected {
			return fresh, contribution.ErrContinuityConflict
		}
		state = fresh
	}
	return state, contribution.ErrContinuityConflict
}
