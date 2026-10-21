package app

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/reference"
)

// LoopAvailability exposes the domain wire type through the application boundary.
type LoopAvailability = loop.Availability

var ErrLoopAvailabilityInput = errors.New("invalid Loop availability input")

type SetLoopAvailabilityInput struct {
	AgentID        string                `json:"agent_id,omitempty"`
	Authority      reference.DigestRef   `json:"authority"`
	Publisher      reference.RevisionRef `json:"publisher"`
	Head           reference.RevisionRef `json:"head"`
	EventID        string                `json:"event_id"`
	Sequence       uint64                `json:"sequence"`
	ExpectedState  loop.Availability     `json:"expected_state"`
	State          loop.Availability     `json:"state"`
	PreviousDigest string                `json:"previous_digest,omitempty"`
}

type LoopAvailabilityResult struct {
	Event      loop.AvailabilityEvent `json:"event"`
	Idempotent bool                   `json:"idempotent"`
}

func (s *Service) GetLoopAvailabilityAs(ctx context.Context, subject core.Subject, id string) (loop.AvailabilityStatus, error) {
	if err := s.requireFleetPrincipalIdentity(subject); err != nil {
		return loop.AvailabilityStatus{}, err
	}
	repository, ok := s.FleetRepository.(fleet.LoopAvailabilityRepository)
	if !ok {
		return loop.AvailabilityStatus{}, ErrFleetUnavailable
	}
	status, err := repository.GetLoopAvailability(ctx, id)
	if err != nil {
		return loop.AvailabilityStatus{}, err
	}
	if status.LoopID != id || !status.State.Valid() || (status.Sequence == 0 && (status.Digest != "" || status.State != loop.AvailabilityPublished)) || (status.Sequence > 0 && status.Digest == "") {
		return loop.AvailabilityStatus{}, fleet.ErrCorrupt
	}
	return status, nil
}

func (s *Service) GetLoopAvailability(ctx context.Context, id string) (loop.AvailabilityStatus, error) {
	subject, err := s.Authenticate(ctx)
	if err != nil {
		return loop.AvailabilityStatus{}, err
	}
	return s.GetLoopAvailabilityAs(ctx, subject, id)
}

func (s *Service) SetLoopAvailabilityAs(ctx context.Context, subject core.Subject, id string, input SetLoopAvailabilityInput) (LoopAvailabilityResult, error) {
	if err := s.requireFleetPrincipal(subject); err != nil {
		return LoopAvailabilityResult{}, err
	}
	if input.Head.ID != id || input.Head.Validate() != nil {
		return LoopAvailabilityResult{}, fleet.ErrConflict
	}
	request := orchestration.SetLoopAvailabilityRequest{Subject: subject, Authority: input.Authority, Publisher: input.Publisher, Event: loop.AvailabilityEvent{EventID: input.EventID, LoopID: id, Head: loop.NewProvenanceRevision(input.Head.ID, input.Head.Revision, input.Head.Digest), Sequence: input.Sequence, ExpectedState: input.ExpectedState, State: input.State, PreviousDigest: input.PreviousDigest}}
	if err := loop.ValidateAvailabilityInput(request.Event); err != nil {
		return LoopAvailabilityResult{}, ErrLoopAvailabilityInput
	}
	if input.AgentID != "" {
		workspace, err := s.RegisteredAgentWorkspaceAs(ctx, subject, input.AgentID)
		if err != nil {
			return LoopAvailabilityResult{}, err
		}
		request.Workspace, request.Authority, request.Publisher = &workspace, workspace.Ref(), workspace.Agent
	}
	event, replay, err := s.Fleet.SetLoopAvailability(ctx, request)
	return LoopAvailabilityResult{Event: event, Idempotent: replay}, err
}

func (s *Service) SetLoopAvailability(ctx context.Context, id string, input SetLoopAvailabilityInput) (LoopAvailabilityResult, error) {
	subject, err := s.Authenticate(ctx)
	if err != nil {
		return LoopAvailabilityResult{}, err
	}
	return s.SetLoopAvailabilityAs(ctx, subject, id, input)
}

func (s *Service) ListLoopInventory(ctx context.Context) ([]LoopView, error) {
	subject, err := s.Authenticate(ctx)
	if err != nil {
		return nil, err
	}
	return s.ListLoopInventoryAs(ctx, subject)
}
