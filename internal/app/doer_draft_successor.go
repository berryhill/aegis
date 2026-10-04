package app

import (
	"context"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	"github.com/berryhill/aegis/internal/store"
)

// ContinueDoerDraftSuccessorAs explicitly rebinds a retained proposal, never a
// published definition or Run intent. The archived snapshot precedes the CAS
// publication, so interrupted writes retain the exact superseded binding.
func (s *Service) ContinueDoerDraftSuccessorAs(ctx context.Context, subject core.Subject, id string, version uint64, expected, charter reference.RevisionRef) (DoerDraft, error) {
	if err := s.requirePrincipal(subject); err != nil {
		return DoerDraft{}, err
	}
	draft, err := s.ReadDoerDraftAs(ctx, subject, id)
	if err != nil {
		return DoerDraft{}, err
	}
	if draft.Version != version || draft.Agent != expected {
		return DoerDraft{}, ErrConflict
	}
	if s.FleetRepository == nil {
		return DoerDraft{}, ErrFleetUnavailable
	}
	previous, err := s.FleetRepository.GetAgentRevision(ctx, expected.ID, expected.Revision)
	if err != nil {
		return DoerDraft{}, err
	}
	latest, err := s.FleetRepository.LatestAgentRevision(ctx, expected.ID)
	if err != nil {
		return DoerDraft{}, err
	}
	if agentRevisionRef(previous) != expected || latest.Charter != charter || latest.Lifecycle != registry.LifecycleEnabled || latest.Ownership.OwnerID != subject.PrincipalID || !s.validAgentCharterSuccessor(previous, latest) {
		return DoerDraft{}, ErrConflict
	}
	var result DoerDraft
	err = s.Store.UpdateWithIntent(doerDraftKind, doerDraftSlot(subject.PrincipalID), "doer-draft-bindings", store.ID("binding"), draft, func(raw []byte) (any, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := s.requirePrincipal(subject); err != nil {
			return nil, err
		}
		set, err := decodeDoerDraftSet(raw, subject.PrincipalID)
		if err != nil {
			return nil, err
		}
		for i, d := range set.Drafts {
			if d.ID != id {
				continue
			}
			now := s.Now().UTC()
			if !now.Before(d.ExpiresAt) {
				return nil, ErrExpired
			}
			if d.Version != version || d.Agent != expected || d.Version == ^uint64(0) || now.Before(d.UpdatedAt) {
				return nil, ErrConflict
			}
			fresh, err := s.FleetRepository.LatestAgentRevision(ctx, expected.ID)
			if err != nil || agentRevisionRef(fresh) != agentRevisionRef(latest) {
				return nil, ErrConflict
			}
			d.Agent = agentRevisionRef(latest)
			d.LoopID, d.PublicationKey = store.ID("doer"), store.ID("doerpub")
			d.Revision, d.PreviousDigest = 1, ""
			d.Version++
			d.UpdatedAt = now
			set.Drafts[i], result = d, d
			return set, nil
		}
		return nil, ErrDenied
	})
	if err != nil {
		return DoerDraft{}, err
	}
	return s.ReadDoerDraftAs(ctx, subject, result.ID)
}

// ApproveDoerDraftSuccessorAs recovers only an identical already-applied
// immediate successor. It never retries approval with a changed expected ref.
func (s *Service) ApproveDoerDraftSuccessorAs(ctx context.Context, subject core.Subject, id string, version uint64, input ApproveAgentCharterInput) (DoerDraft, error) {
	if err := s.requirePrincipal(subject); err != nil {
		return DoerDraft{}, err
	}
	d, err := s.ReadDoerDraftAs(ctx, subject, id)
	if err != nil {
		return DoerDraft{}, err
	}
	if d.Version != version || d.Agent != input.Expected {
		return DoerDraft{}, ErrConflict
	}
	_, err = s.ApproveAgentCharterAs(ctx, subject, input.Expected.ID, input)
	if err != nil {
		if s.FleetRepository == nil {
			return DoerDraft{}, err
		}
		old, e := s.FleetRepository.GetAgentRevision(ctx, input.Expected.ID, input.Expected.Revision)
		if e != nil {
			return DoerDraft{}, err
		}
		latest, e := s.FleetRepository.LatestAgentRevision(ctx, input.Expected.ID)
		if e != nil || agentRevisionRef(old) != input.Expected || latest.Charter != input.Charter || !s.validAgentCharterSuccessor(old, latest) {
			return DoerDraft{}, err
		}
	}
	return s.ContinueDoerDraftSuccessorAs(ctx, subject, id, version, input.Expected, input.Charter)
}
