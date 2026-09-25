package orchestration

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/berryhill/aegis/internal/implementation"
)

// DoerStepView is an authenticated, content-free projection of a verified
// append-only cursor fact. Raw task text, model reports and file bytes remain
// in controller custody rather than being copied into Queue responses.
type DoerStepView struct {
	Sequence       uint32 `json:"sequence"`
	Digest         string `json:"digest"`
	PreviousDigest string `json:"previous_digest,omitempty"`
	StepID         string `json:"step_id"`
	Visit          uint32 `json:"visit"`
	Attempt        uint16 `json:"attempt"`
	Terminal       bool   `json:"terminal"`
}

// DoerTrace is a read-only port; the application must authenticate the caller
// and resolve the exact attempt's pinned Loop revision before invoking it.
func (worker *QueueWorker) DoerTrace(ctx context.Context, attemptID, revisionDigest string) ([]DoerStepView, error) {
	if worker == nil || worker.repository == nil {
		return nil, errors.New("Doer worker unavailable")
	}
	custody, ok := worker.repository.(interface{ ImplementationStore() implementation.Store })
	if !ok {
		return nil, errors.New("Doer custody unavailable")
	}
	facts := implementation.StepCheckpointStore{DB: custody.ImplementationStore(), RunID: attemptID, RevisionDigest: revisionDigest}
	history, err := facts.History(ctx)
	if errors.Is(err, implementation.ErrNotFound) {
		return []DoerStepView{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]DoerStepView, 0, len(history))
	for _, fact := range history {
		var cursor struct {
			StepID  string            `json:"step_id"`
			Visits  map[string]uint32 `json:"visits"`
			Pending *struct {
				Attempt uint16 `json:"attempt"`
			} `json:"pending"`
			Done json.RawMessage `json:"done"`
		}
		if err := json.Unmarshal(fact.Payload, &cursor); err != nil || cursor.StepID == "" {
			return nil, errors.New("Doer cursor payload invalid")
		}
		view := DoerStepView{Sequence: fact.Sequence, Digest: fact.Digest, PreviousDigest: fact.PreviousDigest, StepID: cursor.StepID, Visit: cursor.Visits[cursor.StepID], Terminal: len(cursor.Done) > 0 && string(cursor.Done) != "null"}
		if cursor.Pending != nil {
			view.Attempt = cursor.Pending.Attempt
		}
		result = append(result, view)
	}
	return result, nil
}
