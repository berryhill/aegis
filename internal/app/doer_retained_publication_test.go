package app

import (
	"context"
	"errors"
	"testing"

	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/registry"
)

type retainedPublicationRepository struct {
	draftAgentRepository
	revision   loop.LoopRevision
	validation loop.LoopValidationResult
	provenance loop.PublicationProvenance
	readErr    error
}

func (r retainedPublicationRepository) GetLoopRevision(context.Context, string, uint64) (loop.LoopRevision, error) {
	return r.revision, r.readErr
}
func (r retainedPublicationRepository) GetLoopValidation(context.Context, string, uint64, string) (loop.LoopValidationResult, error) {
	return r.validation, nil
}
func (r retainedPublicationRepository) GetLoopPublicationProvenance(context.Context, string, uint64) (loop.PublicationProvenance, error) {
	return r.provenance, nil
}
func (r retainedPublicationRepository) GetAgentRevision(_ context.Context, _ string, rev uint64) (registry.AgentRevision, error) {
	if rev == r.latest.Revision {
		return r.latest, nil
	}
	return r.exact, nil
}

func fixtureWorkspaceProvenance(t *testing.T, p loop.PublicationProvenance, a registry.AgentRevision) loop.PublicationProvenance {
	t.Helper()
	w, err := orchestration.NewRegisteredAgentWorkspaceAuthority(p.PrincipalID, agentRevisionRef(a), a.Ownership.OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	p.Authority = loop.NewProvenanceDigest(w.Ref().ID, w.Ref().Digest)
	p.MandateID, p.StanzaID = w.ID, "workspace-control"
	p.Runtime.Runtime = a.Runtime.Runtime
	p, err = loop.NewPublicationProvenance(p)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRetainedDoerPublicationExactReuseAndDenials(t *testing.T) {
	s, sub, in := draftFixture(t)
	ctx := context.Background()
	d, err := s.SaveDoerDraftAs(ctx, sub, in)
	if err != nil {
		t.Fatal(err)
	}
	revision, validation, err := NewDoerLoopRevision(d.LoopID, d.Revision, d.PreviousDigest, d.Contract)
	if err != nil {
		t.Fatal(err)
	}
	a := registry.AgentRevision{AgentID: in.Agent.ID, Revision: in.Agent.Revision, Digest: in.Agent.Digest, Ownership: registry.Ownership{OwnerID: sub.PrincipalID}, Charter: in.Agent, Lifecycle: registry.LifecycleEnabled}
	p, err := loop.NewPublicationProvenance(loop.PublicationProvenance{Loop: loop.NewProvenanceRevision(revision.LoopID, revision.Revision, revision.Digest), PublisherAgent: loop.NewProvenanceRevision(a.AgentID, a.Revision, a.Digest), Charter: loop.NewProvenanceRevision(a.Charter.ID, a.Charter.Revision, a.Charter.Digest), Authority: loop.NewProvenanceDigest("workspace-agent", in.Agent.Digest), MandateID: "workspace-agent", StanzaID: "workspace", Runtime: loop.ProvenanceRuntime{Runtime: "hermes-agent"}, ValidationDigest: validation.Digest, OwnerID: sub.PrincipalID, PrincipalID: sub.PrincipalID, AuthorityKind: "registered-agent-workspace"})
	if err != nil {
		t.Fatal(err)
	}
	a.Runtime.Runtime = "hermes-agent"
	p = fixtureWorkspaceProvenance(t, p, a)
	base := retainedPublicationRepository{draftAgentRepository: draftAgentRepository{exact: a, latest: a}, revision: revision, validation: validation, provenance: p}
	s.FleetRepository = base
	got, err := s.retainedDoerPublication(ctx, sub, d)
	if err != nil || got == nil || !got.Decision.Idempotent || got.Revision.Digest != revision.Digest {
		t.Fatalf("reuse=%+v %v", got, err)
	}
	for _, name := range []string{"unknown", "changed", "corrupt", "foreign-owner", "foreign-agent", "missing-validation", "wrong-validation", "substituted-participant"} {
		t.Run(name, func(t *testing.T) {
			r := base
			task := d
			switch name {
			case "unknown":
				r.readErr = errors.New("unknown publication outcome")
			case "changed":
				task.Contract.Task = "another task"
			case "corrupt":
				r.revision.Digest = in.Agent.Digest
			case "foreign-owner":
				r.provenance.OwnerID = "foreign"
				r.provenance, _ = loop.NewPublicationProvenance(r.provenance)
			case "foreign-agent":
				r.provenance.PublisherAgent.ID = "foreign"
				r.provenance, _ = loop.NewPublicationProvenance(r.provenance)
			case "missing-validation":
				r.validation = loop.LoopValidationResult{}
			case "wrong-validation":
				r.provenance.ValidationDigest = in.Agent.Digest
				r.provenance, _ = loop.NewPublicationProvenance(r.provenance)
			case "substituted-participant":
				task.Agent.Revision++
			}
			s.FleetRepository = r
			if _, err := s.retainedDoerPublication(ctx, sub, task); err == nil {
				t.Fatal("accepted invalid publication")
			}
		})
	}
	base.readErr = fleet.ErrNotFound
	s.FleetRepository = base
	if got, err := s.retainedDoerPublication(ctx, sub, d); err != nil || got != nil {
		t.Fatalf("absence=%+v %v", got, err)
	}
}
