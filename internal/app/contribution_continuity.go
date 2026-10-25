package app

import (
	"context"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

// ObserveContributionContinuityAs is same-owner readback, not continuation
// admission. A new authenticated session may observe; it gets no runtime rights.
func (s *Service) ObserveContributionContinuityAs(ctx context.Context, subject core.Subject, key string) (contribution.Contract, contribution.Continuity, []contribution.ContinuityEvent, error) {
	if err := s.RequirePrincipalIdentity(subject); err != nil {
		return contribution.Contract{}, contribution.Continuity{}, nil, err
	}
	contracts, ok := s.FleetRepository.(fleet.ContributionRepository)
	continuity, supported := s.FleetRepository.(fleet.ContributionContinuityRepository)
	if !ok || !supported || !contribution.ValidKey(key) {
		return contribution.Contract{}, contribution.Continuity{}, nil, ErrDenied
	}
	c, err := contracts.GetContribution(ctx, subject.PrincipalID, key)
	if err != nil {
		return c, contribution.Continuity{}, nil, err
	}
	if c.OwnerID != subject.PrincipalID {
		return c, contribution.Continuity{}, nil, ErrDenied
	}
	item, err := s.FleetRepository.GetQueueItem(ctx, c.QueueItemID)
	if err != nil {
		return c, contribution.Continuity{}, nil, err
	}
	if item.GraphRunID != c.RunID || item.Submission.ID != c.SubmissionID {
		return c, contribution.Continuity{}, nil, fleet.ErrCorrupt
	}
	state, events, err := continuity.GetContributionContinuity(ctx, c.QueueItemID)
	return c, state, events, err
}

// ContributionContinuityInput is only a retained control fact. It cannot carry
// authority, stop acknowledgements, an approval receipt or an effect outcome.
// Expected head makes uncertain responses reconcilable without double spending.
type ContributionContinuityInput struct {
	ContractDigest   string
	ExpectedSequence uint64
	ExpectedDigest   string
	Kind             string
	Counter          string
	Stage            string
	Candidate        string
	Evidence         reference.DigestRef
	DecisionID       string
	DecisionOutcome  string
}

// RecordContributionContinuityAs is a concrete owning-service/store path.
// Only the original session may append activity facts until fresh explicit role
// continuation admission is integrated. Cancellation can come from a fresh
// same-owner session; it records a fence, never claims runtime quiescence.
func (s *Service) RecordContributionContinuityAs(ctx context.Context, subject core.Subject, session, key string, in ContributionContinuityInput) (contribution.Continuity, error) {
	if in.Kind == "cancel-acknowledged" {
		return contribution.Continuity{}, ErrDenied
	}
	if err := s.requirePrincipal(subject); err != nil {
		return contribution.Continuity{}, err
	}
	c, state, _, err := s.ObserveContributionContinuityAs(ctx, subject, key)
	if err != nil {
		return state, err
	}
	if session == "" || (session != c.SessionID && in.Kind != "cancel-requested") || in.ContractDigest != c.Digest {
		return state, ErrDenied
	}
	if in.ExpectedSequence != state.Sequence || in.ExpectedDigest != state.Digest {
		return state, contribution.ErrContinuityConflict
	}
	// References sealed at intake are not authority. Latest enabled exact role
	// definitions are checked here, but this deliberately does not launch a role.
	if in.Kind != "cancel-requested" {
		view, e := s.FleetRepository.GetLoopRevision(ctx, c.Loop.ID, c.Loop.Revision)
		if e != nil || view.Digest != c.Loop.Digest {
			return state, ErrDenied
		}
		for _, role := range c.Roles {
			agent, e := s.FleetRepository.LatestAgentRevision(ctx, role.Agent.ID)
			if e != nil || agent.AgentID != role.Agent.ID || agent.Revision != role.Agent.Revision || agent.Digest != role.Agent.Digest || agent.Charter != role.Charter || agent.Runtime != role.Runtime || agent.Lifecycle != registry.LifecycleEnabled || agent.Ownership.OwnerID != c.OwnerID {
				return state, ErrDenied
			}
		}
	}
	event := contribution.ContinuityEvent{SchemaVersion: contribution.ContinuitySchema, QueueItemID: c.QueueItemID, ContractDigest: c.Digest, OwnerID: c.OwnerID, SessionID: session, Sequence: state.Sequence + 1, PreviousDigest: state.Digest, Kind: in.Kind, Counter: in.Counter, Stage: in.Stage, Candidate: in.Candidate, Evidence: in.Evidence, DecisionID: in.DecisionID, DecisionOutcome: in.DecisionOutcome, At: s.Now()}
	if in.Kind == "initialize" {
		event.Limits = contribution.ContinuityLimits{Candidates: uint32(c.CandidateBudget), QueueAttempts: 1, RuntimeTurns: 128, InfrastructureRechecks: 2, DecisionDeliveries: 8}
	}
	event, err = contribution.SealContinuityEvent(event)
	if err != nil {
		return state, err
	}
	return s.FleetRepository.(fleet.ContributionContinuityRepository).AppendContributionContinuity(ctx, event)
}
