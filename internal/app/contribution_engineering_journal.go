package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
)

// Preparation is an owning-service journey before Queue admission, not a Queue
// result. Every reservation and outcome is create-only and hash-linked. A lost
// return is UNKNOWN, never permission to repeat an inference.
type ContributionEngineeringStatus struct {
	DispatchSequence   uint64                 `json:"dispatch_sequence,omitempty"`
	DispatchDigest     string                 `json:"dispatch_digest,omitempty"`
	Decision           *contribution.Decision `json:"decision,omitempty"`
	IntentID           string                 `json:"intent_id"`
	IntentDigest       string                 `json:"intent_digest"`
	RunID              string                 `json:"run_id"`
	Deadline           time.Time              `json:"deadline"`
	Sequence           uint64                 `json:"sequence"`
	PreviousDigest     string                 `json:"previous_digest"`
	Digest             string                 `json:"digest"`
	Phase              string                 `json:"phase"`
	Outcome            string                 `json:"outcome"`
	ReservedTurns      uint32                 `json:"reserved_turns"`
	ReturnedTurns      uint32                 `json:"returned_turns"`
	TurnLimit          uint32                 `json:"turn_limit"`
	DecisionDeliveries uint32                 `json:"decision_deliveries"`
	SessionID          string                 `json:"session_id,omitempty"`
	Quiescent          bool                   `json:"quiescent"`
	Turn               *execution.Turn        `json:"turn,omitempty"`
	Control            contribution.Control   `json:"control"`
	At                 time.Time              `json:"at"`
}

func (s *Service) engineeringStatus(id string, intent contributionEngineeringIntent) (ContributionEngineeringStatus, error) {
	head := ContributionEngineeringStatus{IntentID: id, IntentDigest: core.Digest(intent), RunID: intent.Contract.RunID, Deadline: intent.Contract.Deadline, TurnLimit: intent.TurnLimit, Phase: "prerequisites", Outcome: "ready", Quiescent: true}
	head.Control.Delivery = intent.Contract.Effect
	for n := uint64(1); n <= 2048; n++ {
		var next ContributionEngineeringStatus
		e := s.Store.Load("contribution-engineering-history", fmt.Sprintf("%s-%06d", id, n), &next)
		if errors.Is(e, os.ErrNotExist) {
			return head, nil
		}
		if e != nil {
			return head, e
		}
		digest := next.Digest
		next.Digest = ""
		if digest != core.Digest(next) || next.IntentID != id || next.IntentDigest != head.IntentDigest || next.RunID != head.RunID || !next.Deadline.Equal(head.Deadline) || next.Sequence != n || next.PreviousDigest != head.Digest || next.TurnLimit != head.TurnLimit || next.ReservedTurns < head.ReservedTurns || next.ReturnedTurns < head.ReturnedTurns || next.ReturnedTurns > next.ReservedTurns || next.ReservedTurns > next.TurnLimit || next.DecisionDeliveries < head.DecisionDeliveries || next.DecisionDeliveries > 8 {
			return head, ErrDenied
		}
		if s.verifyEngineeringDispatch(head, next) != nil {
			return head, ErrDenied
		}
		next.Digest = digest
		head = next
	}
	return head, ErrDenied
}
func (s *Service) appendEngineeringStatus(head, next ContributionEngineeringStatus) (ContributionEngineeringStatus, error) {
	next.Sequence = head.Sequence + 1
	next.PreviousDigest = head.Digest
	next.Digest = ""
	next.At = s.Now()
	next.Digest = core.Digest(next)
	e := s.Store.Create("contribution-engineering-history", fmt.Sprintf("%s-%06d", head.IntentID, next.Sequence), next)
	return next, e
}
func (s *Service) ReadContributionEngineeringAs(ctx context.Context, sub core.Subject, key string) (ContributionEngineeringStatus, error) {
	if ctx.Err() != nil || s.requireFleetPrincipal(sub) != nil || !contribution.ValidKey(key) {
		return ContributionEngineeringStatus{}, ErrDenied
	}
	id := engineeringIntentID(sub.PrincipalID, key)
	var intent contributionEngineeringIntent
	if e := s.Store.Load("contribution-engineering-intents", id, &intent); e != nil {
		return ContributionEngineeringStatus{}, e
	}
	if intent.Contract.OwnerID != sub.PrincipalID {
		return ContributionEngineeringStatus{}, ErrDenied
	}
	s.contributionEngineeringMu.Lock()
	defer s.contributionEngineeringMu.Unlock()
	return s.engineeringStatus(id, intent)
}

func (s *Service) controlEngineeringPreparation(ctx context.Context, sub core.Subject, session, key string, stop bool, delivery string) error {
	if ctx.Err() != nil || session == "" || !contribution.ValidKey(key) || s.requireFleetPrincipal(sub) != nil {
		return ErrDenied
	}
	id := engineeringIntentID(sub.PrincipalID, key)
	var intent contributionEngineeringIntent
	if e := s.Store.Load("contribution-engineering-intents", id, &intent); e != nil {
		return e
	}
	if intent.Contract.OwnerID != sub.PrincipalID {
		return ErrDenied
	}
	s.contributionEngineeringMu.Lock()
	defer s.contributionEngineeringMu.Unlock()
	head, e := s.engineeringStatus(id, intent)
	if e != nil {
		return e
	}
	target, e := contribution.RestrictDelivery(head.Control.Delivery, delivery)
	if e != nil {
		return e
	}
	next := head
	next.Control = contribution.Control{QueueItemID: intent.Contract.QueueItemID, ContractDigest: core.Digest(intent), OwnerID: sub.PrincipalID, SessionID: session, Sequence: head.Control.Sequence + 1, PreviousDigest: head.Control.Digest, Stop: head.Control.Stop || stop, Delivery: target, At: s.Now()}
	next.Control.Digest = core.Digest(next.Control)
	// A sticky stop is a request, not a claim that a running child has joined.
	_, e = s.appendEngineeringStatus(head, next)
	return e
}

type retainedEngineeringReader struct{ source contribution.Source }

func (r retainedEngineeringReader) Read(context.Context, contribution.IssueURL) (contribution.Source, error) {
	return r.source, nil
}

func (s *Service) resumeEngineeringPreparation(ctx context.Context, sub core.Subject, key string) (ContributionExecutionResult, error) {
	status, e := s.ReadContributionEngineeringAs(ctx, sub, key)
	if e != nil {
		return ContributionExecutionResult{}, e
	}
	if status.Control.Stop || !status.Quiescent || status.ReservedTurns != status.ReturnedTurns || status.ReservedTurns >= status.TurnLimit || !s.Now().Before(status.Deadline) {
		return s.preparationExecutionStatus(ctx, sub, key)
	}
	var intent contributionEngineeringIntent
	if e = s.Store.Load("contribution-engineering-intents", status.IntentID, &intent); e != nil {
		return ContributionExecutionResult{}, e
	}
	c := intent.Contract
	if _, e = s.admitContributionAs(ctx, sub, c.SessionID, c.Loop, c.Source.URL.URL, key, c.NarrowerScope, retainedEngineeringReader{c.Source}); e != nil {
		r, re := s.preparationExecutionStatus(ctx, sub, key)
		return r, errors.Join(e, re)
	}
	return s.ResumeContributionAs(ctx, sub, key)
}

func (s *Service) preparationExecutionStatus(ctx context.Context, sub core.Subject, key string) (ContributionExecutionResult, error) {
	status, e := s.ReadContributionEngineeringAs(ctx, sub, key)
	if e != nil {
		return ContributionExecutionResult{}, e
	}
	return ContributionExecutionResult{Available: true, Reason: "engineering_" + status.Outcome, Preparation: &status}, nil
}
