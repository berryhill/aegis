package registry

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/berryhill/aegis/internal/reference"
)

var (
	ErrNotFound        = errors.New("registry record not found")
	ErrConflict        = errors.New("registry identity or revision conflict")
	ErrNotEnabled      = errors.New("registered agent is not enabled")
	ErrRetired         = errors.New("retired agent cannot publish another revision")
	ErrAmbiguousSource = errors.New("fleet source identity resolved ambiguously")
)

type Repository interface {
	Register(context.Context, AgentRegistration, AgentRevision) (bool, error)
	PublishRevision(context.Context, AgentRevision) error
	GetAgentRegistration(context.Context, string) (AgentRegistration, error)
	GetAgentRegistrationBySource(context.Context, FleetSource) (AgentRegistration, error)
	GetRevision(context.Context, string, uint64) (AgentRevision, error)
	LatestRevision(context.Context, string) (AgentRevision, error)
	ListAgentRegistrations(context.Context) ([]AgentRegistration, error)
}

// MemoryRepository is the minimal concurrency-safe create-only repository. It
// is suitable for service composition and deterministic tests; durable adapters
// can implement the same contract without gaining an update operation.
type MemoryRepository struct {
	mu            sync.RWMutex
	registrations map[string]AgentRegistration
	sourceAgents  map[string]string
	revisions     map[string]map[uint64]AgentRevision
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		registrations: make(map[string]AgentRegistration),
		sourceAgents:  make(map[string]string),
		revisions:     make(map[string]map[uint64]AgentRevision),
	}
}

func (repository *MemoryRepository) Register(ctx context.Context, registration AgentRegistration, initial AgentRevision) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := registration.Validate(); err != nil {
		return false, err
	}
	if err := validateSealedRevision(initial); err != nil {
		return false, err
	}
	if initial.AgentID != registration.AgentID || initial.Revision != 1 || initial.Source != registration.Source ||
		initial.Digest != registration.InitialRevision.Digest {
		return false, errors.New("registration does not bind the supplied initial revision")
	}

	repository.mu.Lock()
	defer repository.mu.Unlock()
	if existing, exists := repository.registrations[registration.AgentID]; exists {
		if registrationsEqual(existing, registration) {
			stored := repository.revisions[registration.AgentID][1]
			if revisionsEqual(stored, initial) {
				return false, nil
			}
		}
		return false, ErrConflict
	}
	if _, exists := repository.sourceAgents[registration.Source.Key()]; exists {
		return false, ErrConflict
	}
	repository.registrations[registration.AgentID] = registration
	repository.sourceAgents[registration.Source.Key()] = registration.AgentID
	repository.revisions[registration.AgentID] = map[uint64]AgentRevision{1: cloneRevision(initial)}
	return true, nil
}

func (repository *MemoryRepository) PublishRevision(ctx context.Context, revision AgentRevision) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateSealedRevision(revision); err != nil {
		return err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	registration, exists := repository.registrations[revision.AgentID]
	if !exists {
		return ErrNotFound
	}
	if revision.Source != registration.Source {
		return ErrConflict
	}
	revisions := repository.revisions[revision.AgentID]
	if _, exists := revisions[revision.Revision]; exists {
		return ErrConflict
	}
	latest := latestRevision(revisions)
	if latest.Lifecycle == LifecycleRetired {
		return ErrRetired
	}
	if revision.Revision != latest.Revision+1 {
		return ErrConflict
	}
	revisions[revision.Revision] = cloneRevision(revision)
	return nil
}

func (repository *MemoryRepository) GetAgentRegistration(ctx context.Context, agentID string) (AgentRegistration, error) {
	if err := ctx.Err(); err != nil {
		return AgentRegistration{}, err
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	registration, exists := repository.registrations[agentID]
	if !exists {
		return AgentRegistration{}, ErrNotFound
	}
	return registration, nil
}

func (repository *MemoryRepository) GetAgentRegistrationBySource(ctx context.Context, source FleetSource) (AgentRegistration, error) {
	if err := ctx.Err(); err != nil {
		return AgentRegistration{}, err
	}
	if err := source.Validate(); err != nil {
		return AgentRegistration{}, err
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	agentID, exists := repository.sourceAgents[source.Key()]
	if !exists {
		return AgentRegistration{}, ErrNotFound
	}
	return repository.registrations[agentID], nil
}

func (repository *MemoryRepository) GetRevision(ctx context.Context, agentID string, revision uint64) (AgentRevision, error) {
	if err := ctx.Err(); err != nil {
		return AgentRevision{}, err
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	value, exists := repository.revisions[agentID][revision]
	if !exists {
		return AgentRevision{}, ErrNotFound
	}
	return cloneRevision(value), nil
}

func (repository *MemoryRepository) LatestRevision(ctx context.Context, agentID string) (AgentRevision, error) {
	if err := ctx.Err(); err != nil {
		return AgentRevision{}, err
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	revisions, exists := repository.revisions[agentID]
	if !exists {
		return AgentRevision{}, ErrNotFound
	}
	return cloneRevision(latestRevision(revisions)), nil
}

func (repository *MemoryRepository) ListAgentRegistrations(ctx context.Context) ([]AgentRegistration, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	result := make([]AgentRegistration, 0, len(repository.registrations))
	for _, registration := range repository.registrations {
		result = append(result, registration)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].AgentID < result[j].AgentID })
	return result, nil
}

func latestRevision(revisions map[uint64]AgentRevision) AgentRevision {
	var latest AgentRevision
	for number, revision := range revisions {
		if number > latest.Revision {
			latest = revision
		}
	}
	return latest
}

func cloneRevision(revision AgentRevision) AgentRevision {
	revision.CapabilityDeclarations = append([]string(nil), revision.CapabilityDeclarations...)
	revision.PolicyRefs = append([]reference.DigestRef(nil), revision.PolicyRefs...)
	return revision
}

func registrationsEqual(left, right AgentRegistration) bool {
	leftWire, leftErr := MarshalAgentRegistration(left)
	rightWire, rightErr := MarshalAgentRegistration(right)
	return leftErr == nil && rightErr == nil && string(leftWire) == string(rightWire)
}

func revisionsEqual(left, right AgentRevision) bool {
	leftWire, leftErr := MarshalAgentRevision(left)
	rightWire, rightErr := MarshalAgentRevision(right)
	return leftErr == nil && rightErr == nil && string(leftWire) == string(rightWire)
}
