package orchestration

import (
	"context"
	"errors"
	"fmt"

	"github.com/berryhill/aegis/internal/disposition"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
)

// preclaimDoerGate assesses task eligibility before any claim or Attempt is
// created. It is a model proposal, not authentication or effect admission.
// The controller repeats fresh authority admission around the local call.
func (worker *QueueWorker) preclaimDoerGate(ctx context.Context, request WorkRequest, item queue.Item, revision loop.LoopRevision, agentRef, loopRef, graphRef reference.RevisionRef) (LayaGate, error) {
	if revision.Doer == nil || worker.implementation == nil || worker.implementation.decision == nil {
		return LayaGate{}, fmt.Errorf("%w: Doer decision backend unavailable", ErrWorkerDenied)
	}
	if readiness := worker.service.Readiness(ctx, ReadinessRequest{Action: FleetActionRuntimeEffect, Subject: request.Subject, Authority: request.Authority, Agent: agentRef, Loop: loopRef, Graph: graphRef}); readiness.State != ReadinessReady {
		return LayaGate{}, fmt.Errorf("%w: preclaim gate admission %s", ErrWorkerDenied, readiness.ReasonCode)
	}
	contract := revision.Doer
	// A pre-existing artifact already satisfying the pinned assertion cannot
	// prove this invocation produced it. Deny before claim rather than
	// manufacturing a success from old bytes.
	filePolicy := evidence.SelectedFilePolicy{Version: evidence.SelectedFilePolicyV1, RelativePath: contract.VerifyFile, Mode: evidence.SelectedFilePresence}
	if contract.ExpectedText != nil {
		filePolicy.Mode, filePolicy.Text = evidence.SelectedFileText, *contract.ExpectedText
	}
	policyDigest, err := filePolicy.Digest()
	if err != nil {
		return LayaGate{}, err
	}
	observation, err := evidence.VerifySelectedFile(ctx, contract.Workspace, filePolicy, policyDigest, evidence.SelectedFileBinding{AttemptID: request.AttemptID, ActionID: "verify", RunID: item.GraphRunID, OwnerID: agentRef.ID, AuthorityContextID: request.Authority.ID, AuthorityContextDigest: request.Authority.Digest})
	if err != nil {
		return LayaGate{}, err
	}
	if observation.Outcome == evidence.Passed {
		return LayaGate{}, fmt.Errorf("%w: selected file already satisfies assertion", ErrWorkerDenied)
	}
	state := "Task: " + contract.Task + "\nExpected result: " + contract.VerifyFile + " exists in the workspace"
	if contract.ExpectedText != nil {
		state += " and contains exactly " + fmt.Sprintf("%q", *contract.ExpectedText)
	}
	gate, err := worker.implementation.decision.Gate(ctx, state)
	if err != nil {
		return LayaGate{}, fmt.Errorf("%w: Doer gate unavailable", ErrWorkerDenied)
	}
	if readiness := worker.service.Readiness(ctx, ReadinessRequest{Action: FleetActionRuntimeEffect, Subject: request.Subject, Authority: request.Authority, Agent: agentRef, Loop: loopRef, Graph: graphRef}); readiness.State != ReadinessReady {
		return LayaGate{}, fmt.Errorf("%w: preclaim gate authority changed", ErrWorkerDenied)
	}
	if !gate.Specified || !gate.ResultDefined {
		if err := worker.rejectDoerNeedsInput(ctx, request, item); err != nil {
			return LayaGate{}, err
		}
		return gate, fmt.Errorf("%w: doer_needs_input", ErrWorkerDenied)
	}
	return gate, nil
}

// Reject the admitted Queue item without inventing a claim or a failed
// implementation Attempt. A stable disposition reason preserves needs_input.
func (worker *QueueWorker) rejectDoerNeedsInput(ctx context.Context, request WorkRequest, item queue.Item) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	now := worker.now()
	ref := digestRef(item.ItemID, item.Digest)
	cancellation, err := queue.NewCancellation(queue.Cancellation{CancellationID: request.DispositionID, QueueItem: ref, Reason: "doer_needs_input", OccurredAt: now})
	if err != nil {
		return err
	}
	transition, err := queue.NewTransition(queue.QueueTransition{TransitionID: request.TerminalTransitionID, QueueItemID: item.ItemID, From: queue.StateQueued, To: queue.StateDenied, Reason: "doer_needs_input", OccurredAt: now})
	if err != nil {
		return err
	}
	dispositionRecord, err := disposition.New(disposition.Record{DispositionID: request.DispositionID, GraphRunID: item.GraphRunID, QueueItem: ref, Authority: item.Authority, State: execution.StateDenied, ReasonCode: "doer_needs_input", OccurredAt: now})
	if err != nil {
		return err
	}
	if err := worker.repository.CancelQueueItem(context.WithoutCancel(ctx), fleet.CancellationMutation{Cancellation: cancellation, Transition: transition, Disposition: dispositionRecord}, worker.service.auditFact("fleet.queue.denied", request.Subject, "doer_needs_input", "", "", "")); err != nil {
		if errors.Is(err, fleet.ErrConflict) {
			return fmt.Errorf("%w: concurrent Queue change", ErrWorkerDenied)
		}
		return err
	}
	return nil
}
