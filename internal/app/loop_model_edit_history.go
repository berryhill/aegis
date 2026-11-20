package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/registry"
	"github.com/berryhill/aegis/internal/store"
)

const loopModelEditHistoryKind = "loop-model-edit-history"

// Reconcile the original immutable publication, not a new publication under
// today's Agent or session. Only authoritative absence permits a new effect.
func (s *Service) ownedLoopEditPublication(ctx context.Context, sub core.Subject, in PublishLoopInput) (*PublishedLoop, error) {
	exact, err := s.FleetRepository.GetLoopRevision(ctx, in.Revision.LoopID, in.Revision.Revision)
	if errors.Is(err, fleet.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(exact, in.Revision) {
		return nil, ErrConflict
	}
	if err = s.ownedLoopEditPredecessor(ctx, sub, in); err != nil {
		return nil, err
	}
	p, err := s.FleetRepository.GetLoopPublicationProvenance(ctx, exact.LoopID, exact.Revision)
	if err != nil {
		return nil, err
	}
	if _, err = loop.MarshalPublicationProvenance(p); err != nil {
		return nil, err
	}
	v, err := s.FleetRepository.GetLoopValidation(ctx, exact.LoopID, exact.Revision, p.ValidationDigest)
	if err != nil {
		return nil, err
	}
	if _, err = loop.MarshalLoopValidationResult(v); err != nil {
		return nil, err
	}
	if p.Loop != loop.NewProvenanceRevision(exact.LoopID, exact.Revision, exact.Digest) || v.Digest != p.ValidationDigest || v.LoopID != exact.LoopID || v.Revision != exact.Revision || v.RevisionDigest != exact.Digest || v.Outcome != loop.ValidationValid || p.PublisherAgent.ID != in.AgentID || p.OwnerID != sub.PrincipalID || p.PrincipalID != sub.PrincipalID {
		return nil, ErrDenied
	}
	publisher, err := s.FleetRepository.GetAgentRevision(ctx, p.PublisherAgent.ID, p.PublisherAgent.Revision)
	if err != nil {
		return nil, err
	}
	if _, err = registry.MarshalAgentRevision(publisher); err != nil {
		return nil, err
	}
	workspace, err := orchestration.NewRegisteredAgentWorkspaceAuthority(sub.PrincipalID, agentRevisionRef(publisher), publisher.Ownership.OwnerID)
	if err != nil || publisher.Lifecycle != registry.LifecycleEnabled || p.PublisherAgent != loop.NewProvenanceRevision(publisher.AgentID, publisher.Revision, publisher.Digest) || p.Charter != loop.NewProvenanceRevision(publisher.Charter.ID, publisher.Charter.Revision, publisher.Charter.Digest) || p.AuthorityKind != "registered-agent-workspace" || p.Authority != loop.NewProvenanceDigest(workspace.Ref().ID, workspace.Ref().Digest) || p.MandateID != workspace.ID || p.StanzaID != "workspace-control" || p.Runtime.Runtime != publisher.Runtime.Runtime {
		return nil, ErrDenied
	}
	if err = s.FleetRepository.VerifyLoopPublication(ctx, loop.PublishRequest{Revision: exact, Validation: v, Provenance: p, ExpectedPreviousDigest: in.ExpectedPreviousDigest, IdempotencyKey: in.IdempotencyKey}); err != nil {
		return nil, err
	}
	return &PublishedLoop{Revision: exact, Validation: v, Provenance: &p, Decision: loop.PublicationDecision{Idempotent: true}}, nil
}

func (s *Service) projectLoopModelEdit(ctx context.Context, sub core.Subject, edit LoopModelEdit) (LoopModelEdit, error) {
	published, err := s.ownedLoopEditPublication(ctx, sub, edit.Input)
	if err != nil {
		if errors.Is(err, ErrConflict) || errors.Is(err, fleet.ErrConflict) {
			edit.Status = "stale"
			return edit, nil
		}
		return LoopModelEdit{}, err
	}
	if published != nil {
		edit.Status, edit.Publication = "published", published
		return edit, nil
	}
	if !s.Now().Before(edit.ExpiresAt) {
		edit.Status = "expired"
		return edit, nil
	}
	if err = s.ownedLoopEditPredecessor(ctx, sub, edit.Input); err != nil {
		return LoopModelEdit{}, err
	}
	old, err := s.FleetRepository.GetAgentRevision(ctx, edit.Expected.ID, edit.Expected.Revision)
	if err != nil {
		return LoopModelEdit{}, err
	}
	if agentRevisionRef(old) != edit.Expected || old.Charter.Digest != edit.Original.Digest || old.Ownership.OwnerID != sub.PrincipalID {
		return LoopModelEdit{}, ErrConflict
	}
	current, err := s.FleetRepository.LatestAgentRevision(ctx, edit.Expected.ID)
	if err != nil {
		return LoopModelEdit{}, err
	}
	edit.Status = "stale"
	if agentRevisionRef(current) == edit.Expected && current.Lifecycle == registry.LifecycleEnabled {
		edit.Status = "pending"
	} else if current.Charter == RevisionReference(edit.Expected.ID, edit.Proposed.Charter.Revision, edit.Proposed.Digest) && s.validAgentCharterSuccessor(old, current) {
		edit.Status = "approved"
	}
	return edit, nil
}

func loopModelEditResult(edit LoopModelEdit) (PublishedLoop, error) {
	switch edit.Status {
	case "published":
		return *edit.Publication, nil
	case "expired":
		return PublishedLoop{}, ErrExpired
	case "stale":
		return PublishedLoop{}, ErrConflict
	case "pending", "approved":
		_, validation, err := loop.NewRevision(edit.Input.Revision)
		return PublishedLoop{Revision: edit.Input.Revision, Validation: validation, PendingEdit: &edit}, err
	default:
		return PublishedLoop{}, ErrConflict
	}
}

func (s *Service) loadLoopModelEditHistory(principal, id string) (LoopModelEdit, error) {
	var raw json.RawMessage
	if err := s.Store.Load(loopModelEditHistoryKind, id, &raw); err != nil {
		return LoopModelEdit{}, err
	}
	if len(raw) > DoerDraftMaxBytes*4 {
		return LoopModelEdit{}, ErrConflict
	}
	set, err := decodeLoopModelEdits(raw, principal)
	if err != nil || len(set.Edits) != 1 || set.Edits[0].ID != id {
		return LoopModelEdit{}, ErrConflict
	}
	return set.Edits[0], nil
}

// Archive terminal reviews create-only before removing them from the bounded
// pending set. A crash may leave duplicate identical records, never lost history.
// No directory/history scan or growing principal blob is required for readback.
func (s *Service) archiveLoopModelEdits(ctx context.Context, sub core.Subject) error {
	var raw json.RawMessage
	if err := s.Store.Load(loopModelEditKind, doerDraftSlot(sub.PrincipalID), &raw); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	set, err := decodeLoopModelEdits(raw, sub.PrincipalID)
	if err != nil {
		return err
	}
	for _, edit := range set.Edits {
		view, err := s.projectLoopModelEdit(ctx, sub, edit)
		if err != nil {
			return err
		}
		if view.Status == "pending" {
			continue
		}
		if err = s.Store.Create(loopModelEditHistoryKind, edit.ID, loopModelEditSet{Edits: []LoopModelEdit{edit}}); err != nil && !errors.Is(err, store.ErrAlreadyExists) {
			return err
		}
		saved, err := s.loadLoopModelEditHistory(sub.PrincipalID, edit.ID)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(saved, edit) {
			return ErrConflict
		}
	}
	return nil
}

// Called under the Store update lock: reads only; never reenter a Store writer.
func (s *Service) pendingLoopModelEdits(ctx context.Context, sub core.Subject, set loopModelEditSet) (loopModelEditSet, error) {
	pending := loopModelEditSet{Edits: []LoopModelEdit{}}
	for _, edit := range set.Edits {
		archived, err := s.loadLoopModelEditHistory(sub.PrincipalID, edit.ID)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return pending, err
		}
		if err == nil {
			if !reflect.DeepEqual(archived, edit) {
				return pending, ErrConflict
			}
			view, err := s.projectLoopModelEdit(ctx, sub, edit)
			if err != nil {
				return pending, err
			}
			if view.Status != "pending" {
				continue
			}
		}
		pending.Edits = append(pending.Edits, edit)
	}
	return pending, nil
}
