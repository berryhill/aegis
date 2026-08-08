// Package registry owns the immutable executable-participant registry.
//
// Registry records carry fleet provenance and exact charter/runtime bindings.
// They do not authenticate callers or grant runtime authority; callers must
// perform those controls before invoking a registry mutation or execution read.
package registry

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/berryhill/aegis/internal/reference"
)

const (
	AgentRevisionSchemaVersion     = "aegis.registry.agent-revision.v1"
	AgentRegistrationSchemaVersion = "aegis.registry.registration.v1"

	LifecycleEnabled  Lifecycle = "enabled"
	LifecycleDisabled Lifecycle = "disabled"
	LifecycleRetired  Lifecycle = "retired"
)

var registryIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,254}$`)

type Lifecycle string

type FleetSource struct {
	FleetID  string `json:"fleet_id"`
	Kind     string `json:"kind"`
	SourceID string `json:"source_id"`
}

type RuntimeBinding struct {
	Adapter string `json:"adapter"`
	Runtime string `json:"runtime"`
	Target  string `json:"target"`
}

type Ownership struct {
	OwnerID          string `json:"owner_id"`
	AccountabilityID string `json:"accountability_id"`
}

// AgentRevision is a create-only statement of executable-participant state.
// Digest covers every field except Digest itself.
type AgentRevision struct {
	SchemaVersion          string                `json:"schema_version"`
	AgentID                string                `json:"agent_id"`
	Revision               uint64                `json:"revision"`
	Source                 FleetSource           `json:"source"`
	Runtime                RuntimeBinding        `json:"runtime"`
	Ownership              Ownership             `json:"ownership"`
	Lifecycle              Lifecycle             `json:"lifecycle"`
	Charter                reference.RevisionRef `json:"charter"`
	CapabilityDeclarations []string              `json:"capability_declarations"`
	PolicyRefs             []reference.DigestRef `json:"policy_refs"`
	Digest                 string                `json:"digest"`
}

// AgentRegistration permanently binds one stable AgentID and one fleet-source
// identity to the exact initial Agent revision. It has no update operation.
type AgentRegistration struct {
	SchemaVersion   string                `json:"schema_version"`
	AgentID         string                `json:"agent_id"`
	Source          FleetSource           `json:"source"`
	InitialRevision reference.RevisionRef `json:"initial_revision"`
}

func (source FleetSource) Validate() error {
	if err := validateIdentifier("fleet id", source.FleetID); err != nil {
		return err
	}
	if err := validateIdentifier("source kind", source.Kind); err != nil {
		return err
	}
	return validateIdentifier("source id", source.SourceID)
}

func (source FleetSource) Key() string {
	return source.Kind + "\x00" + source.FleetID + "\x00" + source.SourceID
}

func (binding RuntimeBinding) Validate() error {
	if err := validateIdentifier("runtime adapter", binding.Adapter); err != nil {
		return err
	}
	if err := validateIdentifier("runtime", binding.Runtime); err != nil {
		return err
	}
	return validateIdentifier("runtime target", binding.Target)
}

func (ownership Ownership) Validate() error {
	if err := validateIdentifier("owner id", ownership.OwnerID); err != nil {
		return err
	}
	return validateIdentifier("accountability id", ownership.AccountabilityID)
}

// Validate verifies both canonical content and its bound digest.
func (revision AgentRevision) Validate() error {
	return validateSealedRevision(revision)
}

func (revision AgentRevision) validateContent() error {
	if revision.SchemaVersion != AgentRevisionSchemaVersion {
		return errors.New("unsupported agent revision schema version")
	}
	if err := validateIdentifier("agent id", revision.AgentID); err != nil {
		return err
	}
	if revision.Revision == 0 {
		return errors.New("agent revision must be positive")
	}
	if err := revision.Source.Validate(); err != nil {
		return err
	}
	if err := revision.Runtime.Validate(); err != nil {
		return err
	}
	if err := revision.Ownership.Validate(); err != nil {
		return err
	}
	switch revision.Lifecycle {
	case LifecycleEnabled, LifecycleDisabled, LifecycleRetired:
	default:
		return errors.New("agent lifecycle must be enabled, disabled, or retired")
	}
	if err := revision.Charter.Validate(); err != nil {
		return fmt.Errorf("validate charter reference: %w", err)
	}
	if revision.Charter.ID != revision.AgentID {
		return errors.New("charter reference id must equal agent id")
	}
	if err := validateUniqueIdentifiers("capability declaration", revision.CapabilityDeclarations); err != nil {
		return err
	}
	for index, policy := range revision.PolicyRefs {
		if err := policy.Validate(); err != nil {
			return fmt.Errorf("validate policy reference: %w", err)
		}
		if index > 0 {
			previous := revision.PolicyRefs[index-1]
			if previous.ID > policy.ID || (previous.ID == policy.ID && previous.Digest > policy.Digest) {
				return errors.New("policy references must be sorted")
			}
			if previous.ID == policy.ID {
				return errors.New("duplicate policy reference id")
			}
		}
	}
	return nil
}

func (registration AgentRegistration) Validate() error {
	if registration.SchemaVersion != AgentRegistrationSchemaVersion {
		return errors.New("unsupported registration schema version")
	}
	if err := validateIdentifier("agent id", registration.AgentID); err != nil {
		return err
	}
	if err := registration.Source.Validate(); err != nil {
		return err
	}
	if err := registration.InitialRevision.Validate(); err != nil {
		return fmt.Errorf("validate initial revision reference: %w", err)
	}
	if registration.InitialRevision.ID != registration.AgentID || registration.InitialRevision.Revision != 1 {
		return errors.New("registration must bind agent id to revision 1")
	}
	return nil
}

func validateIdentifier(field, value string) error {
	if !utf8.ValidString(value) || strings.TrimSpace(value) != value || !registryIdentifierPattern.MatchString(value) {
		return fmt.Errorf("%s is malformed", field)
	}
	return nil
}

func validateUniqueIdentifiers(field string, values []string) error {
	if !sort.StringsAreSorted(values) {
		return fmt.Errorf("%ss must be sorted", field)
	}
	for index, value := range values {
		if err := validateIdentifier(field, value); err != nil {
			return err
		}
		if index > 0 && values[index-1] == value {
			return fmt.Errorf("duplicate %s", field)
		}
	}
	return nil
}
