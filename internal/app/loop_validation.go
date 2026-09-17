package app

import (
	"context"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
)

// ValidatedLoop is a non-persisted definition check, not publication approval,
// execution verification, or runtime authority. Publication repeats admission.
type ValidatedLoop struct {
	Revision   loop.LoopRevision         `json:"revision"`
	Validation loop.LoopValidationResult `json:"validation"`
	Published  bool                      `json:"published"`
}

func (s *Service) ValidateLoopAs(ctx context.Context, subject core.Subject, input PublishLoopInput) (ValidatedLoop, error) {
	if input.AgentID == "" || input.Authority != (reference.DigestRef{}) || input.Publisher != (reference.RevisionRef{}) {
		return ValidatedLoop{}, ErrDenied
	}
	if _, err := s.RegisteredAgentWorkspaceAs(ctx, subject, input.AgentID); err != nil {
		return ValidatedLoop{}, err
	}
	revision, validation, err := loop.NewRevision(input.Revision)
	return ValidatedLoop{Revision: revision, Validation: validation, Published: false}, err
}

func (s *Service) ValidateLoop(ctx context.Context, input PublishLoopInput) (ValidatedLoop, error) {
	subject, err := s.Authenticate(ctx)
	if err != nil {
		return ValidatedLoop{}, err
	}
	return s.ValidateLoopAs(ctx, subject, input)
}
