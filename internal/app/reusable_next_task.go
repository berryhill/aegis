package app

import (
	"context"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/hostapproval"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
)

// Concrete task scope, not an Agent-wide reservation or a new request key,
// determines whether a failed task is being continued. Definitions grant nothing.
func reusableNextTaskAllowed(previous, next loop.DoerContract, linked bool) bool {
	oldDigest, oldErr := previous.Digest()
	nextDigest, nextErr := next.Digest()
	if oldErr != nil || nextErr != nil {
		return false
	}
	return (oldDigest == nextDigest) == linked
}

func (s *Service) reusableHistoricalContract(ctx context.Context, in QueueLoopInput, old QueueExecutionView, revision loop.LoopRevision) (loop.DoerContract, error) {
	if old.SourceKind != "direct_loop" || old.Source.Digest != revision.Digest || old.Submission.OwnerAgentID != in.Agent.ID || old.RuntimeAuthorityBinding == nil || old.RuntimeAuthorityBinding.OwnerAgent != in.Agent {
		return loop.DoerContract{}, ErrDenied
	}
	bound, err := bindDoerRun(ctx, QueueLoopInput{Agent: in.Agent, Loop: old.Source, Inputs: old.Inputs}, revision)
	if err != nil {
		return loop.DoerContract{}, err
	}
	return queuedDoerContract(bound, revision)
}

func (s *Service) validateReusableNextTask(ctx context.Context, sub core.Subject, in QueueLoopInput, requestID string, old QueueExecutionView, previous loop.LoopRevision) error {
	before, err := s.reusableHistoricalContract(ctx, in, old, previous)
	if err != nil {
		return err
	}
	revision, err := s.FleetRepository.GetLoopRevision(ctx, in.Loop.ID, in.Loop.Revision)
	if err != nil || revision.Digest != in.Loop.Digest {
		return ErrDenied
	}
	next, err := queuedDoerContract(ctx, revision)
	if err != nil {
		return err
	}
	linked := false
	if in.Reexecution != nil {
		var fact LoopReexecutionFact
		if s.Store.Load("queue-loop-reexecution", requestID, &fact) != nil || fact.Digest != reexecutionDigest(fact) || fact.RequestID != requestID || fact.SubjectID != sub.ID || fact.PrincipalID != sub.PrincipalID || fact.Agent != in.Agent || fact.Loop != in.Loop || fact.Remaining == 0 || fact.Remaining >= next.MaxAttempts || fact.Previous.PreviousQueue.ID != old.Item.ItemID || fact.Previous.PreviousQueue.Digest != old.Item.Digest || old.Disposition == nil || fact.Previous.PreviousDisposition.ID != old.Disposition.DispositionID || fact.Previous.PreviousDisposition.Digest != old.Disposition.Digest {
			return ErrDenied
		}
		linked = true
	}
	if !reusableNextTaskAllowed(before, next, linked) {
		return ErrDenied
	}
	return nil
}

// A different task between two requests must not hide an older failed budget.
// Explicit continuation may cover only its verified immutable ancestry.
func (s *Service) validateReusableTaskHistory(ctx context.Context, sub core.Subject, in QueueLoopInput, candidate loop.LoopRevision) error {
	if candidate.SchemaVersion != loop.DoerReusableSchemaVersion {
		return nil
	}
	current, err := queuedDoerContract(ctx, candidate)
	if err != nil {
		return err
	}
	currentDigest, err := current.Digest()
	if err != nil {
		return err
	}
	views, err := s.ListQueueAs(ctx, sub)
	if err != nil {
		return err
	}
	for _, old := range views {
		if old.Projection.State != queue.StateFailed || old.Submission.OwnerID != sub.PrincipalID || old.Submission.OwnerAgentID != in.Agent.ID || old.RuntimeAuthorityBinding == nil || old.RuntimeAuthorityBinding.OwnerAgent != in.Agent || old.SourceKind != "direct_loop" {
			continue
		}
		previous, err := s.FleetRepository.GetLoopRevision(ctx, old.Source.ID, old.Source.Revision)
		if err != nil || previous.Digest != old.Source.Digest {
			return ErrDenied
		}
		if previous.SchemaVersion != loop.DoerReusableSchemaVersion {
			continue
		}
		contract, err := s.reusableHistoricalContract(ctx, in, old, previous)
		if err != nil {
			return err
		}
		digest, err := contract.Digest()
		if err != nil {
			return err
		}
		if digest != currentDigest {
			continue
		}
		if in.Reexecution == nil || !s.reexecutionCoversQueue(sub, in, reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: old.Item.ItemID, Digest: old.Item.Digest}) {
			return ErrDenied
		}
	}
	return nil
}

func (s *Service) reexecutionCoversQueue(sub core.Subject, in QueueLoopInput, target reference.DigestRef) bool {
	next := in.Reexecution.PreviousQueue
	for n := 0; n < 3; n++ {
		if next == target {
			return true
		}
		// Only immutable lineage facts link an ancestor; no process or projection guess.
		var fact LoopReexecutionFact
		requestID := next.ID
		if len(requestID) < 6 || requestID[len(requestID)-6:] != "-queue" {
			return false
		}
		requestID = requestID[:len(requestID)-6]
		if s.Store.Load("queue-loop-reexecution", requestID, &fact) != nil || fact.Digest != reexecutionDigest(fact) || fact.RequestID != requestID || fact.SubjectID != sub.ID || fact.PrincipalID != sub.PrincipalID || fact.Agent != in.Agent || fact.Loop != in.Loop || fact.Remaining == 0 {
			return false
		}
		next = fact.Previous.PreviousQueue
	}
	return false
}

// A live exact-task approval can cover its canonically budget-linked v5
// continuation without renewing consent or changing its signed expiry. It can
// never cover a fresh key alone, a different task/candidate, or a budget reset.
func (s *Service) reusableApprovalCoversRun(record hostapproval.Record, requestID string, candidate, agent reference.RevisionRef) bool {
	if record.DoerReusable == nil || record.RunRequestID == "" || record.RunRequestID == requestID || record.Candidate != candidate || record.Agent != agent {
		return false
	}
	cap := record.Contract.MaxAttempts
	if record.ImplementationPassCap > 0 {
		cap = record.ImplementationPassCap
	}
	current := requestID
	subjectID := ""
	for n := 0; n < 3; n++ {
		var fact LoopReexecutionFact
		if s.Store.Load("queue-loop-reexecution", current, &fact) != nil || fact.Digest != reexecutionDigest(fact) || fact.RequestID != current || fact.PrincipalID != record.PrincipalID || fact.Agent != agent || fact.Loop != candidate || fact.SubjectID == "" || fact.Consumed == 0 || fact.Remaining == 0 || fact.Consumed+fact.Remaining > cap || fact.Previous.PreviousQueue.Validate() != nil || fact.Previous.PreviousDisposition.Validate() != nil {
			return false
		}
		if subjectID != "" && subjectID != fact.SubjectID {
			return false
		}
		subjectID = fact.SubjectID
		if fact.Previous.PreviousQueue.ID == record.RunRequestID+"-queue" {
			return true
		}
		id := fact.Previous.PreviousQueue.ID
		if len(id) < 6 || id[len(id)-6:] != "-queue" {
			return false
		}
		current = id[:len(id)-6]
	}
	return false
}
