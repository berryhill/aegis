package app

import (
	"context"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

// checkContributionRole reloads normal session authority, never caller approval
// flags. It neither starts nor revives any runtime or modifies Queue binding.
func (s *Service) checkContributionRole(ctx context.Context, sub core.Subject, c contribution.Contract, sessionID, role string) (contribution.RoleAuthority, error) {
	denied := contribution.RoleAuthority{}
	if err := s.requirePrincipal(sub); err != nil {
		return denied, err
	}
	binding, ok := contribution.SealedRole(c, role)
	if !ok || c.OwnerID != sub.PrincipalID || !s.Now().Before(c.Deadline) {
		return denied, ErrDenied
	}
	view, err := s.FleetRepository.GetLoopRevision(ctx, c.Loop.ID, c.Loop.Revision)
	if err != nil || view.Digest != c.Loop.Digest {
		return denied, ErrDenied
	}
	agent, err := s.FleetRepository.LatestAgentRevision(ctx, binding.Agent.ID)
	if err != nil || agent.AgentID != binding.Agent.ID || agent.Revision != binding.Agent.Revision || agent.Digest != binding.Agent.Digest || agent.Charter != binding.Charter || agent.Runtime != binding.Runtime || agent.Ownership.OwnerID != c.OwnerID || agent.Lifecycle != registry.LifecycleEnabled {
		return denied, ErrDenied
	}
	a, err := s.authorityContextForSession(ctx, sessionID)
	if err != nil {
		return denied, ErrDenied
	}
	m, err := s.Authority.GetMandate(ctx, a.MandateID)
	if err != nil || core.ValidateAuthorityContext(a, m) != nil || s.validateMandate(m) != nil || a.SubjectID != sub.ID || m.Subject.PrincipalID != c.OwnerID || a.AgentID != binding.Charter.ID || a.CharterRevision != binding.Charter.Revision || a.CharterDigest != binding.Charter.Digest || m.Target != binding.Runtime.Target || a.Runtime.Runtime != binding.Runtime.Runtime {
		return denied, ErrDenied
	}
	session, err := s.GetSession(sessionID)
	if err != nil || session.ID != a.SessionID || core.Digest(session.Mandate) != core.Digest(m) || session.Status != "running" || session.EndedAt != nil || session.RuntimeSessionID == "" || session.RuntimePID <= 0 || session.ProcessStart == "" {
		return denied, ErrDenied
	}
	admission, err := s.AuthorityCommands.AuthorityAdmission(ctx, a.ID, a.Digest, s.Now())
	if err != nil || !admission.Admitted || admission.AuthorityContext.Digest != a.Digest || admission.AuthorityContext.ID != a.ID {
		return denied, ErrDenied
	}
	projection, err := s.FleetRepository.GetQueueProjection(ctx, c.QueueItemID)
	if err != nil || (projection.State != queue.StateQueued && projection.State != queue.StateClaimed) {
		return denied, ErrDenied
	}
	original, err := s.FleetRepository.GetQueueRuntimeBinding(ctx, c.QueueItemID)
	if err != nil || original.OwnerAgent != c.Controller || original.Submission.ID != c.SubmissionID {
		return denied, ErrDenied
	}
	return contribution.RoleAuthority{SessionID: sessionID, RuntimeSessionID: session.RuntimeSessionID, Role: role, Binding: binding, Authority: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: a.ID, Digest: a.Digest}, MandateID: m.ID, OriginalRuntimeBinding: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: original.BindingID, Digest: original.Digest}}, nil
}

// AttachContributionObservationAs records a normally authenticated same-owner
// session without granting runtime rights. The session is resolved, not a label.
func (s *Service) AttachContributionObservationAs(ctx context.Context, sub core.Subject, session, key string, in ContributionContinuityInput) (contribution.Continuity, error) {
	if err := s.requirePrincipal(sub); err != nil {
		return contribution.Continuity{}, err
	}
	c, state, _, err := s.ObserveContributionContinuityAs(ctx, sub, key)
	if err != nil {
		return state, err
	}
	a, err := s.authorityContextForSession(ctx, session)
	if err != nil || a.SubjectID != sub.ID {
		return state, ErrDenied
	}
	m, err := s.Authority.GetMandate(ctx, a.MandateID)
	if err != nil || m.Subject.PrincipalID != c.OwnerID || core.ValidateAuthorityContext(a, m) != nil {
		return state, ErrDenied
	}
	return s.appendContributionRoleEvent(ctx, c, state, session, in, "observation", nil)
}

// AdmitContributionRoleAs explicitly associates a separately admitted existing
// clean session. Replacement always denies pending real process-Wait integration.
func (s *Service) AdmitContributionRoleAs(ctx context.Context, sub core.Subject, session, key, role string, in ContributionContinuityInput) (contribution.Continuity, error) {
	if err := s.requirePrincipal(sub); err != nil {
		return contribution.Continuity{}, err
	}
	c, state, _, err := s.ObserveContributionContinuityAs(ctx, sub, key)
	if err != nil {
		return state, err
	}
	r, err := s.checkContributionRole(ctx, sub, c, session, role)
	if err != nil {
		return state, err
	}
	if old, exists := state.RoleAuthorities[role]; exists {
		if !contribution.NativeRoleQuiescent(state, role) {
			return state, contribution.ErrNativeQuiescenceUnavailable
		}
		previous, e := s.Authority.GetMandate(ctx, old.MandateID)
		oldContext, contextErr := s.Authority.GetAuthorityContext(ctx, old.Authority.ID)
		if contextErr != nil || oldContext.Digest != old.Authority.Digest || oldContext.SessionID != old.SessionID || oldContext.MandateID != old.MandateID || core.ValidateAuthorityContext(oldContext, previous) != nil {
			return state, ErrDenied
		}
		if e != nil {
			return state, ErrDenied
		}
		current, e := s.Authority.GetMandate(ctx, r.MandateID)
		if e != nil || !compatibleContributionMandates(previous, current) {
			return state, ErrDenied
		}
	}
	r.Epoch = state.RoleEpochs[role] + 1
	return s.appendContributionRoleEvent(ctx, c, state, session, in, "role-admitted", &r)
}

func (s *Service) appendContributionRoleEvent(ctx context.Context, c contribution.Contract, state contribution.Continuity, session string, in ContributionContinuityInput, kind string, r *contribution.RoleAuthority) (contribution.Continuity, error) {
	if in.ContractDigest != c.Digest || in.ExpectedSequence != state.Sequence || in.ExpectedDigest != state.Digest {
		return state, contribution.ErrContinuityConflict
	}
	e, err := contribution.SealContinuityEvent(contribution.ContinuityEvent{SchemaVersion: contribution.ContinuitySchema, QueueItemID: c.QueueItemID, ContractDigest: c.Digest, OwnerID: c.OwnerID, SessionID: session, Sequence: state.Sequence + 1, PreviousDigest: state.Digest, Kind: kind, RoleAuthority: r, At: s.Now()})
	if err != nil {
		return state, err
	}
	return s.FleetRepository.(fleet.ContributionContinuityRepository).AppendContributionContinuity(ctx, e)
}

// CheckContributionRoleAs is the fresh gate for an already-associated session.
// A historical association does not bypass revocation, expiry, stop or head CAS.
func (s *Service) CheckContributionRoleAs(ctx context.Context, sub core.Subject, session, key, role string, in ContributionContinuityInput) (contribution.RoleAuthority, error) {
	c, state, _, err := s.ObserveContributionContinuityAs(ctx, sub, key)
	if err != nil {
		return contribution.RoleAuthority{}, err
	}
	if _, stopped := state.RoleStops[role]; stopped {
		return contribution.RoleAuthority{}, ErrDenied
	}
	if state.CancelRequested || state.Sequence != in.ExpectedSequence || state.Digest != in.ExpectedDigest || c.Digest != in.ContractDigest {
		return contribution.RoleAuthority{}, ErrDenied
	}
	r, err := s.checkContributionRole(ctx, sub, c, session, role)
	if err != nil {
		return r, err
	}
	r.Epoch = state.RoleEpochs[role]
	if old, ok := state.RoleAuthorities[role]; !ok || old != r {
		return contribution.RoleAuthority{}, ErrDenied
	}
	control, err := s.FleetRepository.(fleet.ContributionRepository).GetContributionControl(ctx, c.QueueItemID)
	if err != nil || control.Stop {
		return contribution.RoleAuthority{}, ErrDenied
	}
	return r, nil
}
