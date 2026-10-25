package orchestration

import (
	"context"
	"errors"

	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

func (w *QueueWorker) SetCodingContinuationResolver(resolve func(context.Context, loop.DoerContract, registry.AgentRevision) (*CodingContinuation, error)) {
	w.codingContinuation = resolve
}

func (w *QueueWorker) PrepareCodingContinuation(ctx context.Context, ref reference.DigestRef, attempt string, revision loop.LoopRevision, head, successor string, remaining uint16) (*CodingContinuation, error) {
	h, err := w.CodingHistory(ctx, ref, attempt, revision)
	if err != nil || h.Digest != head {
		return nil, errors.New("native coding history mismatch")
	}
	c, err := revision.Doer.CodingImplementation()
	if err != nil {
		return nil, err
	}
	custody, ok := w.repository.(interface{ ImplementationStore() implementation.Store })
	if !ok {
		return nil, errors.New("native custody unavailable")
	}
	kernel := implementation.Executor{DB: custody.ImplementationStore()}
	kernel.RequireInputEvidence = true
	native, err := kernel.PrepareContinuation(ref.ID, head, successor, remaining, c)
	if err != nil {
		return nil, err
	}
	return &CodingContinuation{native: native}, nil
}

// CodingHistory binds native custody to the exact immutable Queue/Attempt/Loop.
// Callers authenticate before using this read-only port. No host code executes.
func (w *QueueWorker) CodingHistory(ctx context.Context, queueRef reference.DigestRef, attemptID string, revision loop.LoopRevision) (CodingHistoryView, error) {
	var empty CodingHistoryView
	if w == nil || w.repository == nil || revision.Doer == nil || revision.Doer.Coding == nil {
		return empty, errors.New("native coding history unavailable")
	}
	item, err := w.repository.GetQueueItem(ctx, queueRef.ID)
	if err != nil || item.Digest != queueRef.Digest {
		return empty, errors.New("native coding Queue mismatch")
	}
	attempt, err := w.repository.GetAttempt(ctx, attemptID)
	if err != nil || attempt.QueueItem != queueRef || attempt.GraphRunID != item.GraphRunID {
		return empty, errors.New("native coding attempt mismatch")
	}
	child, err := w.repository.GetLoopExecution(ctx, attempt.LoopExecutionID)
	if err != nil || child.GraphRunID != item.GraphRunID || child.Loop.ID != revision.LoopID || child.Loop.Revision != revision.Revision || child.Loop.Digest != revision.Digest {
		return empty, errors.New("native coding Loop mismatch")
	}
	custody, ok := w.repository.(interface{ ImplementationStore() implementation.Store })
	if !ok {
		return empty, errors.New("native coding custody unavailable")
	}
	contract, err := revision.Doer.CodingImplementation()
	if err != nil {
		return empty, err
	}
	kernel := implementation.Executor{DB: custody.ImplementationStore()}
	kernel.RequireInputEvidence = true
	h, err := kernel.History(queueRef.ID, contract)
	if errors.Is(err, implementation.ErrNotFound) {
		return empty, nil
	} // admission can fail before reservation
	if err != nil {
		return empty, err
	}
	return codingHistoryView(h), nil
}
