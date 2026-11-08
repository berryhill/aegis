package app

import (
	"errors"
	"fmt"
	"os"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
)

// Each actual phase is create-only. The status journal commits the exact phase
// and predecessor, so recovery cannot replace a failed operation with a retry.
type engineeringDispatchPhase struct {
	IntentID       string                   `json:"intent_id"`
	Sequence       uint64                   `json:"sequence"`
	PreviousDigest string                   `json:"previous_digest"`
	Operation      execution.LaunchContract `json:"operation"`
}

func (s *Service) retainEngineeringDispatch(id string, operation execution.LaunchContract) error {
	s.contributionEngineeringMu.Lock()
	defer s.contributionEngineeringMu.Unlock()
	var intent contributionEngineeringIntent
	if err := s.Store.Load("contribution-engineering-intents", id, &intent); err != nil {
		return err
	}
	head, err := s.engineeringStatus(id, intent)
	if err != nil {
		return err
	}
	phase := engineeringDispatchPhase{id, head.DispatchSequence + 1, head.DispatchDigest, operation}
	key := fmt.Sprintf("%s-%06d", id, phase.Sequence)
	if err = s.Store.Create("contribution-engineering-dispatch-phases", key, phase); err != nil {
		return err
	}
	next := head
	next.DispatchSequence, next.DispatchDigest = phase.Sequence, core.Digest(phase)
	if _, err = s.appendEngineeringStatus(head, next); err != nil {
		return err
	}
	// Historical owning target retains its first terminal observation forever. It
	// is compatibility readback only; the complete validated journal is canonical.
	if operation.ParentDispatch.FinishedAt != nil {
		var old execution.LaunchContract
		err = s.Store.Load("contribution-engineering-dispatches", id, &old)
		if errors.Is(err, os.ErrNotExist) {
			return s.Store.Create("contribution-engineering-dispatches", id, operation)
		}
	}
	return err
}
func (s *Service) verifyEngineeringDispatch(head, next ContributionEngineeringStatus) error {
	if next.DispatchSequence == head.DispatchSequence {
		if next.DispatchDigest != head.DispatchDigest {
			return ErrDenied
		}
		return nil
	}
	if next.DispatchSequence != head.DispatchSequence+1 {
		return ErrDenied
	}
	var phase engineeringDispatchPhase
	if s.Store.Load("contribution-engineering-dispatch-phases", fmt.Sprintf("%s-%06d", next.IntentID, next.DispatchSequence), &phase) != nil || core.Digest(phase) != next.DispatchDigest || phase.IntentID != next.IntentID || phase.Sequence != next.DispatchSequence || phase.PreviousDigest != head.DispatchDigest {
		return ErrDenied
	}
	current := phase.Operation.ParentDispatch
	if current.ID == "" || phase.Operation.OwnerID == "" {
		return ErrDenied
	}
	if current.State == execution.StateRequested {
		if current.StartedAt != nil || current.FinishedAt != nil {
			return ErrDenied
		}
		if head.DispatchSequence > 0 {
			var prior engineeringDispatchPhase
			if s.Store.Load("contribution-engineering-dispatch-phases", fmt.Sprintf("%s-%06d", next.IntentID, head.DispatchSequence), &prior) != nil || prior.Operation.ParentDispatch.FinishedAt == nil || prior.Operation.ParentDispatch.ID == current.ID {
				return ErrDenied
			}
		}
		return nil
	}
	if head.DispatchSequence == 0 {
		return ErrDenied
	}
	var prior engineeringDispatchPhase
	if s.Store.Load("contribution-engineering-dispatch-phases", fmt.Sprintf("%s-%06d", next.IntentID, head.DispatchSequence), &prior) != nil {
		return ErrDenied
	}
	old := prior.Operation.ParentDispatch
	if old.ID != current.ID || old.FinishedAt != nil || !old.RequestedAt.Equal(current.RequestedAt) || core.Digest(prior.Operation.Mandate) != core.Digest(phase.Operation.Mandate) || prior.Operation.OwnerID != phase.Operation.OwnerID {
		return ErrDenied
	}
	switch current.State {
	case execution.StateStarted:
		if old.State != execution.StateRequested || current.StartedAt == nil || current.FinishedAt != nil {
			return ErrDenied
		}
	case execution.StateSucceeded, execution.StateFailed, execution.StateCancelled:
		if current.FinishedAt == nil || (current.State == execution.StateSucceeded && current.StartedAt == nil) {
			return ErrDenied
		}
	default:
		return ErrDenied
	}
	return nil
}
