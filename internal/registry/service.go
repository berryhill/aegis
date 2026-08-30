package registry

import (
	"context"
	"errors"

	"github.com/berryhill/aegis/internal/reference"
)

type Service struct {
	repository Repository
}

func NewService(repository Repository) (*Service, error) {
	if repository == nil {
		return nil, errors.New("registry repository is required")
	}
	return &Service{repository: repository}, nil
}

// RegisterFromSource discovers and registers exactly one candidate selected by
// immutable fleet-source identity. Authentication and operator authorization
// must be completed by the application boundary before this method is called.
func (service *Service) RegisterFromSource(ctx context.Context, source Source, identity FleetSource) (AgentRegistration, AgentRevision, bool, error) {
	if source == nil {
		return AgentRegistration{}, AgentRevision{}, false, errors.New("fleet source is required")
	}
	if err := identity.Validate(); err != nil {
		return AgentRegistration{}, AgentRevision{}, false, err
	}
	candidates, err := source.Discover(ctx)
	if err != nil {
		return AgentRegistration{}, AgentRevision{}, false, err
	}
	var selected *Candidate
	for index := range candidates {
		if candidates[index].Source == identity {
			if selected != nil {
				return AgentRegistration{}, AgentRevision{}, false, ErrAmbiguousSource
			}
			candidate := candidates[index]
			selected = &candidate
		}
	}
	if selected == nil {
		return AgentRegistration{}, AgentRevision{}, false, ErrNotFound
	}
	if selected.AgentID == BuiltInAegisAgentID {
		return AgentRegistration{}, AgentRevision{}, false, ErrBuiltInImmutable
	}
	initial, err := SealRevision(AgentRevision{
		SchemaVersion:          AgentRevisionSchemaVersion,
		AgentID:                selected.AgentID,
		Revision:               1,
		Source:                 selected.Source,
		Runtime:                selected.Runtime,
		Ownership:              selected.Ownership,
		Lifecycle:              selected.Lifecycle,
		Charter:                selected.Charter,
		CapabilityDeclarations: append([]string(nil), selected.CapabilityDeclarations...),
		PolicyRefs:             append([]reference.DigestRef(nil), selected.PolicyRefs...),
	})
	if err != nil {
		return AgentRegistration{}, AgentRevision{}, false, err
	}
	registration := AgentRegistration{
		SchemaVersion: AgentRegistrationSchemaVersion,
		AgentID:       initial.AgentID,
		Source:        initial.Source,
		InitialRevision: reference.RevisionRef{
			SchemaVersion: reference.RevisionRefSchemaVersion,
			ID:            initial.AgentID,
			Revision:      initial.Revision,
			Digest:        initial.Digest,
		},
	}
	created, err := service.repository.Register(ctx, registration, initial)
	if err != nil {
		return AgentRegistration{}, AgentRevision{}, false, err
	}
	return registration, initial, created, nil
}

// PublishRevision adds one exact next revision. There is deliberately no
// overwrite, patch, upsert, or publish-latest operation.
func (service *Service) PublishRevision(ctx context.Context, revision AgentRevision) error {
	if revision.AgentID == BuiltInAegisAgentID {
		return ErrBuiltInImmutable
	}
	return service.repository.PublishRevision(ctx, revision)
}

func (service *Service) GetAgentRegistration(ctx context.Context, agentID string) (AgentRegistration, error) {
	return service.repository.GetAgentRegistration(ctx, agentID)
}

func (service *Service) GetAgentRegistrationBySource(ctx context.Context, source FleetSource) (AgentRegistration, error) {
	return service.repository.GetAgentRegistrationBySource(ctx, source)
}

func (service *Service) GetRevision(ctx context.Context, agentID string, revision uint64) (AgentRevision, error) {
	return service.repository.GetRevision(ctx, agentID, revision)
}

func (service *Service) LatestRevision(ctx context.Context, agentID string) (AgentRevision, error) {
	return service.repository.LatestRevision(ctx, agentID)
}

func (service *Service) ListAgentRegistrations(ctx context.Context) ([]AgentRegistration, error) {
	return service.repository.ListAgentRegistrations(ctx)
}

// ResolveExecutable returns only an exact enabled revision. It never resolves
// "latest" and treats disabled and retired participants as fail-closed.
func (service *Service) ResolveExecutable(ctx context.Context, ref reference.RevisionRef) (AgentRevision, error) {
	if err := ref.Validate(); err != nil {
		return AgentRevision{}, err
	}
	revision, err := service.repository.GetRevision(ctx, ref.ID, ref.Revision)
	if err != nil {
		return AgentRevision{}, err
	}
	if revision.Digest != ref.Digest {
		return AgentRevision{}, ErrConflict
	}
	if revision.Lifecycle != LifecycleEnabled {
		return AgentRevision{}, ErrNotEnabled
	}
	return revision, nil
}
