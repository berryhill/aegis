package app

import (
 "context"
 "fmt"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/persistence/fleet"
)

// Carry already spent preparation calls/deliveries into the SAME runtime and
// decision budgets before any executable preparation. Stable operation keys make
// an interrupted import idempotent; admission replay finishes the original import.
func (s *Service) carryEngineeringPreparation(ctx context.Context,c contribution.Contract) error {
 if c.Preparation==nil{return nil}
 id:=engineeringIntentID(c.OwnerID,c.Key)
 var intent contributionEngineeringIntent
 if e:=s.Store.Load("contribution-engineering-intents",id,&intent);e!=nil{return e}
 status,e:=s.engineeringStatus(id,intent);if e!=nil{return e}
 if !status.Quiescent || status.ReturnedTurns!=status.ReservedTurns{return ErrDenied}
 repo:=s.FleetRepository.(fleet.ContributionContinuityRepository)
 state,_,e:=repo.GetContributionContinuity(ctx,c.QueueItemID);if e!=nil{return e}
 appendEvent:=func(kind,counter,operation string) error {
  event:=contribution.ContinuityEvent{SchemaVersion:contribution.ContinuitySchema,QueueItemID:c.QueueItemID,ContractDigest:c.Digest,OwnerID:c.OwnerID,SessionID:c.SessionID,Sequence:state.Sequence+1,PreviousDigest:state.Digest,Kind:kind,Counter:counter,OperationID:operation,At:s.Now()}
  if kind=="initialize" {event.Limits=contribution.Counters{Candidates:uint32(c.CandidateBudget),QueueAttempts:1,RuntimeTurns:128,InfrastructureRechecks:2,DecisionDeliveries:8}}
  event,e=contribution.SealContinuityEvent(event);if e!=nil{return e};state,e=repo.AppendContributionContinuity(ctx,event);return e
 }
 if state.Sequence==0 {if e=appendEvent("initialize","","");e!=nil{return e}}
 for _,budget:=range []struct{counter string;count uint32}{{"runtime-turn",status.ReservedTurns-2},{"decision-delivery",status.DecisionDeliveries}} {
  for n:=uint32(1);n<=budget.count;n++ {op:=fmt.Sprintf("%s-%s-%d",id,budget.counter,n);if state.Reservations[op]!="" {continue};if e=appendEvent("reserve",budget.counter,op);e!=nil{return e}}
 }
 return nil
}

// Called by the real worker only after exact profile/host/readiness preflight,
// immediately before the canonical initial claim transaction.
func (s *Service) reserveContributionInitialAttempt(ctx context.Context, sub core.Subject, c contribution.Contract, attempt string) error {
	if sub.PrincipalID != c.OwnerID || s.requireFleetPrincipal(sub) != nil || attempt == "" {
		return ErrDenied
	}
	repo, ok := s.FleetRepository.(fleet.ContributionContinuityRepository)
	if !ok {
		return ErrDenied
	}
	appendFact := func(kind, counter, operation string, limits contribution.Counters) error {
		state, _, err := repo.GetContributionContinuity(ctx, c.QueueItemID)
		if err != nil {
			return err
		}
		e, err := contribution.SealContinuityEvent(contribution.ContinuityEvent{SchemaVersion: contribution.ContinuitySchema, QueueItemID: c.QueueItemID, ContractDigest: c.Digest, OwnerID: c.OwnerID, SessionID: c.SessionID, Sequence: state.Sequence + 1, PreviousDigest: state.Digest, Kind: kind, Counter: counter, OperationID: operation, Limits: limits, At: s.Now()})
		if err != nil {
			return err
		}
		_, err = repo.AppendContributionContinuity(ctx, e)
		return err
	}
	state, _, err := repo.GetContributionContinuity(ctx, c.QueueItemID)
	if err != nil {
		return err
	}
	if state.Sequence == 0 {
		if err = appendFact("initialize", "", "", contribution.Counters{Candidates: uint32(c.CandidateBudget), QueueAttempts: 1, RuntimeTurns: 128, InfrastructureRechecks: 2, DecisionDeliveries: 8}); err != nil {
			return err
		}
	}
	return appendFact("reserve", "queue-attempt", attempt, contribution.Counters{})
}
