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
	// FailureCode is the last closed step failure, not raw runtime output.
	// Legacy untyped failures remain step_error; no historical cause is inferred.
	FailureCode string `json:"failure_code,omitempty"`
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
			Done     json.RawMessage `json:"done"`
			Failures []struct {
				Code string `json:"code"`
			} `json:"failures"`
		}
		if err := json.Unmarshal(fact.Payload, &cursor); err != nil || cursor.StepID == "" {
			return nil, errors.New("Doer cursor payload invalid")
		}
		view := DoerStepView{Sequence: fact.Sequence, Digest: fact.Digest, PreviousDigest: fact.PreviousDigest, StepID: cursor.StepID, Visit: cursor.Visits[cursor.StepID], Terminal: len(cursor.Done) > 0 && string(cursor.Done) != "null"}
		if len(cursor.Failures) > 0 {
			view.FailureCode = "step_error"
			if code := cursor.Failures[len(cursor.Failures)-1].Code; isDoerFailureCode(code) {
				view.FailureCode = code
			}
		}
		if cursor.Pending != nil {
			view.Attempt = cursor.Pending.Attempt
		}
		result = append(result, view)
	}
	return result, nil
}
