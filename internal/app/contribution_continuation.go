package app

import (
	"context"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"reflect"
)

// Compatibility excludes only identity and freshness; authority, stanza, runtime,
// environment and scopes must match exactly. Authentication never renews old facts.
func compatibleContributionMandates(old, fresh core.Mandate) bool {
	if old.Subject.PrincipalID != fresh.Subject.PrincipalID {
		return false
	}
	old.ID = ""
	fresh.ID = ""
	old.Subject = core.Subject{}
	fresh.Subject = core.Subject{}
	old.IssuedAt = fresh.IssuedAt
	old.ExpiresAt = fresh.ExpiresAt
	return reflect.DeepEqual(old, fresh)
}

// ResumeContributionRoleAs creates a clean session through ordinary provisioning
// receipt, selection, mandate and runtime admission. A durable create-only hold
// precedes issuance: an unknown/crashed preparation cannot issue a duplicate.
// It does not renew the immutable Queue binding, claim, attempt or budgets.
func (s *Service) ResumeContributionRoleAs(ctx context.Context, sub core.Subject, key, role string, in ContributionContinuityInput) (core.Session, error) {
	c, state, _, err := s.ObserveContributionContinuityAs(ctx, sub, key)
	if err != nil {
		return core.Session{}, err
	}
	old, ok := state.RoleAuthorities[role]
	if !ok || !contribution.NativeRoleQuiescent(state, role) || in.ContractDigest != c.Digest || in.ExpectedSequence != state.Sequence || in.ExpectedDigest != state.Digest {
		return core.Session{}, ErrDenied
	}
	previous, err := s.Authority.GetMandate(ctx, old.MandateID)
	if err != nil {
		return core.Session{}, err
	}
	hold := old.SessionID
	if err = s.Store.Create("contribution-role-preparation", hold, struct {
		Contract, Role string
		Epoch          uint64
	}{c.Digest, role, old.Epoch + 1}); err != nil {
		return core.Session{}, err
	}
	m, _, err := s.PreviewSessionAs(ctx, sub, old.Binding.Charter.ID, old.Binding.Charter.Revision, previous.StanzaID, previous.Environment)
	if err != nil {
		return core.Session{}, err
	}
	if !compatibleContributionMandates(previous, m) {
		return core.Session{}, ErrDenied
	}
	session, err := s.StartSessionAs(ctx, sub, m.ID)
	if err != nil {
		return core.Session{}, err
	}
	fail := func(e error) (core.Session, error) {
		_ = s.TerminateSession(context.WithoutCancel(ctx), session.ID, "continuation_admission_failed")
		return core.Session{}, e
	}
	state, err = s.AttachContributionObservationAs(ctx, sub, session.ID, key, in)
	if err != nil {
		return fail(err)
	}
	in.ExpectedSequence = state.Sequence
	in.ExpectedDigest = state.Digest
	_, err = s.AdmitContributionRoleAs(ctx, sub, session.ID, key, role, in)
	if err != nil {
		return fail(err)
	}
	return session, nil
}
