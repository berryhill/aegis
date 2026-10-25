package app

import (
 "context"
 "sync/atomic"
 "github.com/berryhill/aegis/internal/contribution"
 "github.com/berryhill/aegis/internal/core"
 "github.com/berryhill/aegis/internal/orchestration"
)

type contributionCustodyKey struct{}
type contributionCustodyProof struct { service *Service; contract,candidate,session string; live atomic.Bool }

// WithContributionContinuityCandidateAs consumes the real #376 lock-held gate,
// retaining its original admission checks. It does not certify legacy native
// producers: without their exact registered boundary the join remains denied.
// The action is synchronous and must not retain the context or launch work.
func(s *Service) WithContributionContinuityCandidateAs(ctx context.Context,sub core.Subject,session,key string,action func(context.Context,orchestration.ContributionCandidate)error)error {
 if action==nil {return ErrDenied}
 gate:=func(ctx context.Context,candidate orchestration.ContributionCandidate)error{
  c,state,_,err:=s.ObserveContributionContinuityAs(ctx,sub,key);if err!=nil{return err}
  if candidate.Contract!=c.Digest || candidate.Digest!=state.Candidate || candidate.Digest!=state.CandidateEvidence.Digest {return ErrDenied}
  if _,err=s.CheckContributionRoleAs(ctx,sub,session,key,"controller",ContributionContinuityInput{ContractDigest:c.Digest,ExpectedSequence:state.Sequence,ExpectedDigest:state.Digest});err!=nil{return err}
  for _,role:=range []string{"builder","tester"}{
   if !contribution.NativeRoleQuiescent(state,role){return contribution.ErrNativeQuiescenceUnavailable}
   for _,binding:=range state.NativeBoundaries[role].Bindings {
    if binding.Candidate!=candidate.Digest || binding.AttemptID!=candidate.QueueAttempt {return ErrDenied}
   }
  }
  return nil
 }
 return s.WithAcceptedContributionCandidateAs(ctx,sub,key,gate,func(ctx context.Context,candidate orchestration.ContributionCandidate)error{
  if err:=gate(ctx,candidate);err!=nil{return err}
  proof:=&contributionCustodyProof{service:s,contract:candidate.Contract,candidate:candidate.Digest,session:session}
  proof.live.Store(true);defer proof.live.Store(false)
  return action(context.WithValue(ctx,contributionCustodyKey{},proof),candidate)
 })
}
