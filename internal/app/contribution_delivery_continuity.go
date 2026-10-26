package app

import (
	"context"
	"encoding/json"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/delivery"
)

// ContributionDeliveryContinuityAuthority admits one exact local-commit intent.
// It is not a publication adapter or a grant. The caller must obtain the intent
// from Source's frozen projection and retain its private synchronous context.
// Request.Candidate is the manifest digest; CandidateEvidence.ID is pass lineage.
// Manager transport session identifiers are never controller session identifiers.
type ContributionDeliveryContinuityAuthority struct {
	service               *Service
	subject               core.Subject
	key, session, control string
	request               contribution.EffectAdmissionRequest
	intent                delivery.Intent
}

var _ delivery.Authority = (*ContributionDeliveryContinuityAuthority)(nil)

// NewContributionDeliveryContinuityAuthority binds an exact reviewed intent and
// canonical role reference. This deliberately supports only local Commit; remote
// delivery requires its own operation-specific scope and payload qualification.
// Construction performs historical validation, never grants write permission.
func NewContributionDeliveryContinuityAuthority(ctx context.Context, s *Service, subject core.Subject, key, controllerSession string, request contribution.EffectAdmissionRequest, intent delivery.Intent) (*ContributionDeliveryContinuityAuthority, error) {
	if s == nil || controllerSession == "" || request.Role != "controller" || request.RoleAuthority.ID != controllerSession || intent.Operation != delivery.Commit || intent.Policy == "" || intent.ID == "" || intent.Payload == "" {
		return nil, delivery.ErrDenied
	}
	c, state, _, err := s.ObserveContributionContinuityAs(ctx, subject, key)
	if err != nil || c.OwnerID != subject.PrincipalID || intent.Run != c.RunID || intent.Contract != c.Digest || intent.Candidate != state.CandidateEvidence.Digest || request.CandidateEvidence != state.CandidateEvidence || request.CandidateEvidence.ID != state.Candidate || request.Candidate != intent.Candidate || intent.FenceEpoch != request.Epoch || intent.RoleBinding != request.RoleAuthority.Digest || intent.Repository != c.Repository || intent.RepositoryID != c.Source.RepositoryID || intent.BaseHead != c.Base {
		return nil, delivery.ErrDenied
	}
	role, err := s.CheckContributionEffectAs(ctx, subject, controllerSession, key, request, false)
	if err != nil || role.SessionID != controllerSession || role.Ref() != request.RoleAuthority {
		return nil, delivery.ErrDenied
	}
	control, err := s.ReadContributionControlAs(ctx, subject, key)
	if err != nil || control.Stop {
		return nil, delivery.ErrDenied
	}
	wire, _ := json.Marshal(control)
	return &ContributionDeliveryContinuityAuthority{s, subject, key, controllerSession, contribution.Digest(wire), request, intent}, nil
}

func (a *ContributionDeliveryContinuityAuthority) Admit(ctx context.Context, intent delivery.Intent, writing bool) error {
	if a == nil || ctx == nil || ctx.Err() != nil || intent != a.intent {
		return delivery.ErrDenied
	}
	control, err := a.service.ReadContributionControlAs(ctx, a.subject, a.key)
	wire, _ := json.Marshal(control)
	if err != nil || (writing && (control.Stop || contribution.Digest(wire) != a.control)) {
		return delivery.ErrDenied
	}
	role, err := a.service.CheckContributionEffectAs(ctx, a.subject, a.session, a.key, a.request, writing)
	if err != nil || role.SessionID != a.session || role.Ref() != a.request.RoleAuthority {
		return delivery.ErrDenied
	}
	return nil
}
