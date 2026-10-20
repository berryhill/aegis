package orchestration

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/looprun"
)

// DoerObservation is the content-free runtime observation exported to application reads.
type DoerObservation = looprun.Observation

// DoerStepView is an authenticated, content-free projection of a verified
// append-only cursor fact. Raw task text, model reports and file bytes remain
// in controller custody rather than being copied into Queue responses.
type DoerStepView struct {
	Observations   []looprun.Observation `json:"observations,omitempty"`
	Sequence       uint32                `json:"sequence"`
	Digest         string                `json:"digest"`
	PreviousDigest string                `json:"previous_digest,omitempty"`
	StepID         string                `json:"step_id"`
	Visit          uint32                `json:"visit"`
	Attempt        uint16                `json:"attempt"`
	Terminal       bool                  `json:"terminal"`
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
	previousFailures := 0
	for _, fact := range history {
		var cursor struct {
			Observations []looprun.Observation `json:"observations"`
			StepID       string                `json:"step_id"`
			Visits       map[string]uint32     `json:"visits"`
			Pending      *struct {
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
		// Failures are cumulative cursor history. Only this checkpoint's new
		// failures may explain its failed step boundaries; never smear an old
		// implementation failure onto a subsequent successful pass or step.
		var codes []string
		for i := previousFailures; i < len(cursor.Failures); i++ {
			codes = append(codes, debuggerFailureCode(cursor.Failures[i].Code))
		}
		previousFailures = len(cursor.Failures)
		var done struct {
			Failure *looprun.Failure `json:"failure"`
		}
		if len(cursor.Done) > 0 {
			_ = json.Unmarshal(cursor.Done, &done)
		}
		if len(codes) == 0 && done.Failure != nil {
			codes = append(codes, debuggerFailureCode(done.Failure.Code))
		}
		if len(codes) > 0 {
			view.FailureCode = codes[0]
		}
		if cursor.Pending != nil {
			view.Attempt = cursor.Pending.Attempt
		}
		for _, observation := range cursor.Observations {
			if !validDoerObservation(observation) {
				return nil, errors.New("invalid step observation")
			}
			if observation.Kind == "step_finish" && observation.Outcome == "failed" && len(codes) > 0 {
				observation.FailureCode = codes[0]
				view.FailureCode = codes[0]
				codes = codes[1:]
			}
			view.Observations = append(view.Observations, observation)
		}
		result = append(result, view)
	}
	return result, nil
}

func debuggerFailureCode(code string) string {
	if isDoerFailureCode(code) {
		return code
	}
	switch code {
	case "step_limit", "retry_exhausted", "output_invalid", "branch_invalid", "traversal_exhausted", "mapping_invalid":
		return code
	default:
		return "step_error"
	}
}
