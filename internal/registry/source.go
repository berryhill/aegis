package registry

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/berryhill/aegis/internal/reference"
)

const (
	CurrentFleetFixtureSchemaVersion = "aegis.current-fleet.fixture.v1"
	CurrentFleetSourceKind           = "current-fleet"
)

// Candidate is a source-discovered participant proposal. Discovery conveys no
// authentication or registration authority.
type Candidate struct {
	AgentID                string
	Source                 FleetSource
	Runtime                RuntimeBinding
	Ownership              Ownership
	Lifecycle              Lifecycle
	Charter                reference.RevisionRef
	CapabilityDeclarations []string
	PolicyRefs             []reference.DigestRef
}

type Source interface {
	Discover(context.Context) ([]Candidate, error)
}

type CurrentFleetFixture struct {
	SchemaVersion string              `json:"schema_version"`
	FleetID       string              `json:"fleet_id"`
	Agents        []CurrentFleetAgent `json:"agents"`
}

type CurrentFleetAgent struct {
	SourceID               string                `json:"source_id"`
	AgentID                string                `json:"agent_id"`
	Runtime                RuntimeBinding        `json:"runtime"`
	Ownership              Ownership             `json:"ownership"`
	Lifecycle              Lifecycle             `json:"lifecycle"`
	Charter                reference.RevisionRef `json:"charter"`
	CapabilityDeclarations []string              `json:"capability_declarations"`
	PolicyRefs             []reference.DigestRef `json:"policy_refs"`
}

// CurrentFleetFixtureSource is an explicit deterministic adapter for the current fleet.
// It consumes only the supplied canonical fixture; it never scans profiles,
// environment variables, credentials, dashboards, or legacy handoff stores.
type CurrentFleetFixtureSource struct {
	candidates []Candidate
}

func NewCurrentFleetFixtureSource(data []byte) (*CurrentFleetFixtureSource, error) {
	fixture, err := decodeStrict(data, func(fixture CurrentFleetFixture) error { return fixture.Validate() })
	if err != nil {
		return nil, err
	}
	candidates := make([]Candidate, 0, len(fixture.Agents))
	for _, agent := range fixture.Agents {
		candidates = append(candidates, Candidate{
			AgentID:                agent.AgentID,
			Source:                 FleetSource{FleetID: fixture.FleetID, Kind: CurrentFleetSourceKind, SourceID: agent.SourceID},
			Runtime:                agent.Runtime,
			Ownership:              agent.Ownership,
			Lifecycle:              agent.Lifecycle,
			Charter:                agent.Charter,
			CapabilityDeclarations: append([]string(nil), agent.CapabilityDeclarations...),
			PolicyRefs:             append([]reference.DigestRef(nil), agent.PolicyRefs...),
		})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Source.Key() < candidates[j].Source.Key() })
	return &CurrentFleetFixtureSource{candidates: candidates}, nil
}

func (fixture CurrentFleetFixture) Validate() error {
	if fixture.SchemaVersion != CurrentFleetFixtureSchemaVersion {
		return errors.New("unsupported current-fleet fixture schema version")
	}
	if err := validateIdentifier("fleet id", fixture.FleetID); err != nil {
		return err
	}
	seenSources := make(map[string]struct{}, len(fixture.Agents))
	seenAgents := make(map[string]struct{}, len(fixture.Agents))
	for _, agent := range fixture.Agents {
		if agent.AgentID == BuiltInAegisAgentID {
			return ErrBuiltInImmutable
		}
		if err := validateIdentifier("source id", agent.SourceID); err != nil {
			return err
		}
		if _, exists := seenSources[agent.SourceID]; exists {
			return errors.New("duplicate current-fleet source id")
		}
		seenSources[agent.SourceID] = struct{}{}
		if _, exists := seenAgents[agent.AgentID]; exists {
			return errors.New("duplicate current-fleet agent id")
		}
		seenAgents[agent.AgentID] = struct{}{}
		revision := AgentRevision{
			SchemaVersion:          AgentRevisionSchemaVersion,
			AgentID:                agent.AgentID,
			Revision:               1,
			Source:                 FleetSource{FleetID: fixture.FleetID, Kind: CurrentFleetSourceKind, SourceID: agent.SourceID},
			Runtime:                agent.Runtime,
			Ownership:              agent.Ownership,
			Lifecycle:              agent.Lifecycle,
			Charter:                agent.Charter,
			CapabilityDeclarations: agent.CapabilityDeclarations,
			PolicyRefs:             agent.PolicyRefs,
		}
		if err := revision.validateContent(); err != nil {
			return fmt.Errorf("validate current-fleet agent %q: %w", agent.AgentID, err)
		}
	}
	return nil
}

func (source *CurrentFleetFixtureSource) Discover(ctx context.Context) ([]Candidate, error) {
	if source == nil {
		return nil, errors.New("current-fleet fixture source is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := make([]Candidate, len(source.candidates))
	for index, candidate := range source.candidates {
		result[index] = candidate
		result[index].CapabilityDeclarations = append([]string(nil), candidate.CapabilityDeclarations...)
		result[index].PolicyRefs = append([]reference.DigestRef(nil), candidate.PolicyRefs...)
	}
	return result, nil
}
