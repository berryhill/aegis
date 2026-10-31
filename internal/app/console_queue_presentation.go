package app

import (
	"context"
	"errors"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
)

// ConsoleQueuePresentation is a transport-neutral projection of canonical owned
// work. It carries no authority; mutations repeat application admission.
type ConsoleQueuePresentation struct {
	Digest                     string
	Revision                   uint64
	Archived, Terminal, Failed bool
}

func QueuePresentationTerminal(state string) bool {
	switch queue.State(state) {
	case queue.StateSucceeded, queue.StateFailed, queue.StateCancelled, queue.StateDenied, queue.StateExpired:
		return true
	}
	return false
}

func (s *Service) ConsoleQueuePresentationAs(ctx context.Context, subject core.Subject, id, digest string) (ConsoleQueuePresentation, error) {
	view, err := s.GetQueueItemAs(ctx, subject, id)
	if err != nil {
		return ConsoleQueuePresentation{}, err
	}
	return s.consoleQueuePresentation(ctx, subject, view, digest)
}

// ConsoleQueuePresentationsAs reconstructs canonical Queue history once for this
// read. Selectors carry only exact IDs and optional expected digests, never views
// or authority. Omitted entries have no controls; canonical read failures fail
// the entire batch closed. No snapshot survives this call.
func (s *Service) ConsoleQueuePresentationsAs(ctx context.Context, subject core.Subject, selectors map[string]string) (map[string]ConsoleQueuePresentation, error) {
	views, err := s.ListQueueAs(ctx, subject)
	if err != nil {
		return nil, err
	}
	result := make(map[string]ConsoleQueuePresentation, len(selectors))
	for _, view := range views {
		digest, requested := selectors[view.Item.ItemID]
		if !requested {
			continue
		}
		presentation, err := s.consoleQueuePresentation(ctx, subject, view, digest)
		if err == nil {
			result[view.Item.ItemID] = presentation
		}
	}
	return result, nil
}

func (s *Service) consoleQueuePresentation(ctx context.Context, subject core.Subject, view QueueExecutionView, digest string) (ConsoleQueuePresentation, error) {
	if view.Submission.OwnerID != subject.PrincipalID || (digest != "" && view.Item.Digest != digest) {
		return ConsoleQueuePresentation{}, ErrDenied
	}
	archive, err := s.GetQueueArchiveAs(ctx, subject, queuePresentationRef(view.Item.ItemID, view.Item.Digest))
	if err != nil {
		return ConsoleQueuePresentation{}, err
	}
	return ConsoleQueuePresentation{Digest: view.Item.Digest, Revision: archive.Revision, Archived: archive.Archived, Terminal: QueuePresentationTerminal(string(view.Projection.State)), Failed: view.Projection.State == queue.StateFailed}, nil
}

// ReconcileConsoleQueueRequeueFailureAs reads only the exact admitted failed
// successor. It never retries an effect or turns another failure into success.
func (s *Service) ReconcileConsoleQueueRequeueFailureAs(ctx context.Context, subject core.Subject, input RequeueQueueItemInput, result QueueLoopResult, original error) (QueueLoopResult, error) {
	if original == nil || result.QueueItemID == "" || result.Reexecution == nil {
		return result, original
	}
	retained, err := s.GetQueueItemAs(ctx, subject, result.QueueItemID)
	if err == nil && retained.Projection.State == queue.StateFailed && retained.Disposition != nil && retained.Disposition.ReasonCode == "doer_loop_failed" && result.Reexecution.Previous.PreviousQueue == input.QueueItem {
		result.Execution = &retained
		return result, nil
	}
	return result, original
}

func queuePresentationRef(id, digest string) reference.DigestRef {
	return reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: id, Digest: digest}
}

func ConsoleQueueRequeueInput(id, digest, identity string) RequeueQueueItemInput {
	return RequeueQueueItemInput{QueueItem: queuePresentationRef(id, digest), IdempotencyKey: identity}
}

func ConsoleQueueArchiveInput(id, digest, identity string, archived bool, revision uint64) SetQueueArchiveInput {
	return SetQueueArchiveInput{RequestID: identity, QueueItem: queuePresentationRef(id, digest), Archived: archived, ExpectedRevision: revision}
}

func ConsoleQueuePreferencesInput(selection, visibility string) (SetQueuePreferencesInput, error) {
	input := SetQueuePreferencesInput{Selection: queue.LifecycleSelection(selection), Visibility: queue.ArchiveVisibility(visibility)}
	if err := input.Selection.Validate(); err != nil {
		return input, err
	}
	return input, input.Visibility.Validate()
}

func ValidateConsoleQueueVisibility(visibility string) error {
	return queue.ArchiveVisibility(visibility).Validate()
}

// Missing visibility metadata for another owner's record is not disclosure of
// that metadata. Preserve collection visibility while refusing its controls.
func (s *Service) ConsoleQueueArchivedAs(ctx context.Context, subject core.Subject, id, digest string) (bool, error) {
	archive, err := s.GetQueueArchiveAs(ctx, subject, queuePresentationRef(id, digest))
	if errors.Is(err, fleet.ErrPresentationDenied) {
		return false, nil
	}
	return archive.Archived, err
}
