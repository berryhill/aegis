package app

import (
	"context"
	"errors"
	"math"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
)

// SetQueueArchiveInput deliberately contains no caller-selected actor identity.
// ExpectedRevision zero means previously unarchived with no visibility record.
type SetQueueArchiveInput struct {
	RequestID        string              `json:"request_id"`
	QueueItem        reference.DigestRef `json:"queue_item"`
	Archived         bool                `json:"archived"`
	ExpectedRevision uint64              `json:"expected_revision"`
}
type SetQueuePreferencesInput struct {
	RequestID        string                   `json:"request_id"`
	Selection        queue.LifecycleSelection `json:"selection"`
	Visibility       queue.ArchiveVisibility  `json:"visibility"`
	ExpectedRevision uint64                   `json:"expected_revision"`
}

func (s *Service) queuePresentationRepository(subject core.Subject, mutation bool) (fleet.QueuePresentationRepository, error) {
	var err error
	if mutation {
		err = s.requireFleetPrincipal(subject)
	} else {
		err = s.requireFleetPrincipalIdentity(subject)
	}
	if err != nil {
		return nil, err
	}
	if subject.ID == "" || subject.AuthenticatedAt.IsZero() || subject.AuthenticatedAt.After(s.Now()) {
		return nil, ErrDenied
	}
	repository, ok := s.FleetRepository.(fleet.QueuePresentationRepository)
	if !ok {
		return nil, ErrFleetUnavailable
	}
	return repository, nil
}

// SetQueueArchiveAs archives or unarchives only exact terminal owned work. The
// repository repeats ownership and canonical terminal checks in its transaction.
func (s *Service) SetQueueArchiveAs(ctx context.Context, subject core.Subject, input SetQueueArchiveInput) (queue.Archive, bool, error) {
	repository, err := s.queuePresentationRepository(subject, true)
	if err != nil {
		return queue.Archive{}, false, err
	}
	if input.ExpectedRevision == math.MaxUint64 {
		return queue.Archive{}, false, fleet.ErrConflict
	}
	v, err := queue.NewArchive(queue.Archive{PrincipalID: subject.PrincipalID, SubjectID: subject.ID, RequestID: input.RequestID, QueueItem: input.QueueItem, Archived: input.Archived, Revision: input.ExpectedRevision + 1, UpdatedAt: s.Now().UTC()})
	if err != nil {
		return queue.Archive{}, false, err
	}
	return repository.SetQueueArchive(ctx, v, input.ExpectedRevision)
}
func (s *Service) SetQueueArchive(ctx context.Context, input SetQueueArchiveInput) (queue.Archive, bool, error) {
	subject, err := s.Authenticate(ctx)
	if err != nil {
		return queue.Archive{}, false, err
	}
	return s.SetQueueArchiveAs(ctx, subject, input)
}
func (s *Service) GetQueueArchiveAs(ctx context.Context, subject core.Subject, ref reference.DigestRef) (queue.Archive, error) {
	repository, err := s.queuePresentationRepository(subject, false)
	if err != nil {
		return queue.Archive{}, err
	}
	if err = ref.Validate(); err != nil {
		return queue.Archive{}, err
	}
	v, err := repository.GetQueueArchive(ctx, subject.PrincipalID, ref)
	if errors.Is(err, fleet.ErrNotFound) {
		// GetQueueArchive verifies ownership before returning absence.
		return queue.Archive{PrincipalID: subject.PrincipalID, QueueItem: ref}, nil
	}
	return v, err
}
func (s *Service) SetQueuePreferencesAs(ctx context.Context, subject core.Subject, input SetQueuePreferencesInput) (queue.Preferences, bool, error) {
	repository, err := s.queuePresentationRepository(subject, true)
	if err != nil {
		return queue.Preferences{}, false, err
	}
	if input.ExpectedRevision == math.MaxUint64 {
		return queue.Preferences{}, false, fleet.ErrConflict
	}
	v, err := queue.NewPreferences(queue.Preferences{PrincipalID: subject.PrincipalID, SubjectID: subject.ID, RequestID: input.RequestID, Selection: input.Selection, Visibility: input.Visibility, Revision: input.ExpectedRevision + 1, UpdatedAt: s.Now().UTC()})
	if err != nil {
		return queue.Preferences{}, false, err
	}
	return repository.SetQueuePreferences(ctx, v, input.ExpectedRevision)
}
func (s *Service) SetQueuePreferences(ctx context.Context, input SetQueuePreferencesInput) (queue.Preferences, bool, error) {
	subject, err := s.Authenticate(ctx)
	if err != nil {
		return queue.Preferences{}, false, err
	}
	return s.SetQueuePreferencesAs(ctx, subject, input)
}
func (s *Service) GetQueuePreferencesAs(ctx context.Context, subject core.Subject) (queue.Preferences, error) {
	repository, err := s.queuePresentationRepository(subject, false)
	if err != nil {
		return queue.Preferences{}, err
	}
	v, err := repository.GetQueuePreferences(ctx, subject.PrincipalID)
	if errors.Is(err, fleet.ErrNotFound) {
		return queue.Preferences{PrincipalID: subject.PrincipalID, Selection: queue.SelectionSucceeded, Visibility: queue.VisibilityUnarchived}, nil
	}
	return v, err
}
