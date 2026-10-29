package app

import (
	"context"
	"errors"
	"os"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/persistence/fleet"
)

// reconcileLoopReexecution is read-only: an exact authenticated intent may read
// its immutable successor even after its successful effects changed the source.
// It never reserves, prepares, resumes or runs that successor.
func (s *Service) reconcileLoopReexecution(ctx context.Context, sub core.Subject, input QueueLoopInput) (QueueLoopResult, bool, error) {
	var result QueueLoopResult
	if err := s.requireFleetPrincipal(sub); err != nil {
		return result, false, err
	}
	if input.Reexecution == nil {
		return result, false, nil
	}
	normalized, identity, err := normalizeLoopRunIdentity(input)
	if err != nil {
		return result, false, err
	}
	key := queueLoopRequestID(sub, normalized)
	var fact LoopReexecutionFact
	if err = s.Store.Load("queue-loop-reexecution", key, &fact); errors.Is(err, os.ErrNotExist) {
		return result, false, nil
	}
	if err != nil {
		return result, false, err
	}
	if fact.Digest != reexecutionDigest(fact) || fact.RequestID != key || fact.SubjectID != sub.ID || fact.PrincipalID != sub.PrincipalID || fact.Agent != normalized.Agent || fact.Loop != normalized.Loop || fact.Previous != *normalized.Reexecution || fact.RequestDigest != identity {
		return result, false, fleet.ErrConflict
	}
	var slot LoopReexecutionFact
	if err = s.Store.Load("queue-loop-reexecution-successor", fact.Previous.PreviousQueue.ID, &slot); err != nil || slot != fact {
		return result, false, ErrDenied
	}
	var reserved string
	if err = s.Store.Load("queue-loop-intent", key, &reserved); errors.Is(err, os.ErrNotExist) {
		return result, false, nil
	}
	if err != nil || reserved != identity {
		return result, false, fleet.ErrConflict
	}
	prior, err := s.GetQueueItemAs(ctx, sub, key+"-queue")
	if errors.Is(err, fleet.ErrNotFound) {
		return result, false, nil
	}
	if err != nil {
		return result, false, err
	}
	if prior.Submission.OwnerID != sub.PrincipalID || prior.Submission.OwnerAgentID != fact.Agent.ID || prior.Submission.IdempotencyKey != key {
		return result, false, ErrDenied
	}
	result = QueueLoopResult{RequestID: key, QueueItemID: prior.Item.ItemID, Reason: "existing_execution", Execution: &prior, Reexecution: &fact}
	return result, true, nil
}
