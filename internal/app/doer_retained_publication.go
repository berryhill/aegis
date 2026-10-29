package app

import (
	"context"
	"errors"
	"reflect"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/persistence/fleet"
)

// retainedDoerPublication reads immutable creation provenance; the draft Agent
// is the separately approved runtime participant, never a replacement publisher.
// Only authoritative absence allows initial publication. All other read failures
// (including an incomplete/unknown publication) fail closed.
func (s *Service) retainedDoerPublication(ctx context.Context, sub core.Subject, d DoerDraft) (*PublishedLoop, error) {
	candidate, _, err := d.Candidate()
	if err != nil {
		return nil, err
	}
	exact, err := s.FleetRepository.GetLoopRevision(ctx, d.LoopID, d.Revision)
	if errors.Is(err, fleet.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if exact.Digest != candidate.Digest || !reflect.DeepEqual(exact, candidate) {
		return nil, ErrConflict
	}
	p, err := s.FleetRepository.GetLoopPublicationProvenance(ctx, d.LoopID, d.Revision)
	if err != nil {
		return nil, err
	}
	if _, err = loop.MarshalPublicationProvenance(p); err != nil {
		return nil, err
	}
	v, err := s.FleetRepository.GetLoopValidation(ctx, d.LoopID, d.Revision, p.ValidationDigest)
	if err != nil {
		return nil, err
	}
	if _, err = loop.MarshalLoopValidationResult(v); err != nil {
		return nil, err
	}
	if p.Loop != loop.NewProvenanceRevision(exact.LoopID, exact.Revision, exact.Digest) || v.Digest != p.ValidationDigest || v.LoopID != exact.LoopID || v.Revision != exact.Revision || v.RevisionDigest != exact.Digest || v.Outcome != loop.ValidationValid || p.PublisherAgent.ID != d.Agent.ID || p.OwnerID != sub.PrincipalID || p.PrincipalID != sub.PrincipalID {
		return nil, ErrDenied
	}
	publisher, err := s.FleetRepository.GetAgentRevision(ctx, p.PublisherAgent.ID, p.PublisherAgent.Revision)
	if err != nil {
		return nil, err
	}
	if p.PublisherAgent != loop.NewProvenanceRevision(publisher.AgentID, publisher.Revision, publisher.Digest) || p.Charter != loop.NewProvenanceRevision(publisher.Charter.ID, publisher.Charter.Revision, publisher.Charter.Digest) || publisher.Ownership.OwnerID != sub.PrincipalID {
		return nil, ErrDenied
	}
	workspace, e := orchestration.NewRegisteredAgentWorkspaceAuthority(sub.PrincipalID, agentRevisionRef(publisher), publisher.Ownership.OwnerID)
	if e != nil || p.AuthorityKind != "registered-agent-workspace" || p.Authority != loop.NewProvenanceDigest(workspace.Ref().ID, workspace.Ref().Digest) || p.MandateID != workspace.ID || p.StanzaID != "workspace-control" || p.Runtime.Runtime != publisher.Runtime.Runtime {
		return nil, ErrDenied
	}
	if agentRevisionRef(publisher) != d.Agent {
		participant, e := s.FleetRepository.GetAgentRevision(ctx, d.Agent.ID, d.Agent.Revision)
		if e != nil {
			return nil, e
		}
		if agentRevisionRef(participant) != d.Agent || !s.validAgentCharterSuccessor(publisher, participant) {
			return nil, ErrConflict
		}
	}
	return &PublishedLoop{Revision: exact, Validation: v, Decision: loop.PublicationDecision{Idempotent: true}}, nil
}
