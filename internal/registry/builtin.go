package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/berryhill/aegis/internal/reference"
)

const (
	// BuiltInSystemSourceKind distinguishes Aegis-owned identities from imported
	// current-fleet records and runtime profiles.
	BuiltInSystemSourceKind = "aegis-system"
	BuiltInAegisAgentID     = "aegis"
	BuiltInAegisOwnerID     = "aegis-system"
	// BuiltInAegisRuntimeTarget is the exact ephemeral manager runtime contract.
	BuiltInAegisRuntimeTarget = "manager-disposable"
)

var ErrBuiltInImmutable = errors.New("built-in Agent does not accept generic revisions")

// CanonicalBuiltInAegisAgent returns the sealed, product-owned Agent identity
// used by bootstrap. Aegis retains product ownership while the authenticated
// principal is deterministically bound as the accountable party. The charter
// reference is a deterministic system representation, not a claim that an
// operator authored or imported a charter.
func CanonicalBuiltInAegisAgent(accountablePrincipalID string) (AgentRegistration, AgentRevision, error) {
	sealed := sha256.Sum256([]byte("aegis.builtin-agent.v1\x00aegis\x00hermes-agent\x00" + BuiltInAegisRuntimeTarget))
	revision, err := SealRevision(AgentRevision{
		SchemaVersion: AgentRevisionSchemaVersion,
		AgentID:       BuiltInAegisAgentID,
		Revision:      1,
		Source: FleetSource{
			FleetID:  "aegis",
			Kind:     BuiltInSystemSourceKind,
			SourceID: "builtin/aegis",
		},
		Runtime: RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent", Target: BuiltInAegisRuntimeTarget},
		Ownership: Ownership{
			OwnerID:          BuiltInAegisOwnerID,
			AccountabilityID: accountablePrincipalID,
		},
		Lifecycle: LifecycleEnabled,
		Charter: reference.RevisionRef{
			SchemaVersion: reference.RevisionRefSchemaVersion,
			ID:            BuiltInAegisAgentID,
			Revision:      1,
			Digest:        "sha256:" + hex.EncodeToString(sealed[:]),
		},
		CapabilityDeclarations: nil,
		PolicyRefs:             nil,
	})
	if err != nil {
		return AgentRegistration{}, AgentRevision{}, err
	}
	registration := AgentRegistration{
		SchemaVersion: AgentRegistrationSchemaVersion,
		AgentID:       revision.AgentID,
		Source:        revision.Source,
		InitialRevision: reference.RevisionRef{
			SchemaVersion: reference.RevisionRefSchemaVersion,
			ID:            revision.AgentID,
			Revision:      revision.Revision,
			Digest:        revision.Digest,
		},
	}
	return registration, revision, nil
}

// CanonicalBuiltInAegisAgentMatches compares the complete canonical wire
// records, not only their digests. Invalid or non-canonical readback fails closed.
func CanonicalBuiltInAegisAgentMatches(registration AgentRegistration, revision AgentRevision, accountablePrincipalID string) bool {
	wantRegistration, wantRevision, err := CanonicalBuiltInAegisAgent(accountablePrincipalID)
	if err != nil {
		return false
	}
	return registrationsEqual(registration, wantRegistration) && revisionsEqual(revision, wantRevision)
}

// ValidateBuiltInAegisRegistration reserves the built-in Agent ID at repository
// boundaries while admitting its one canonical revision-1 bootstrap record.
// The accountability binding is part of the canonical digest and full-wire
// comparison; callers cannot substitute only a plausible source or runtime.
func ValidateBuiltInAegisRegistration(registration AgentRegistration, revision AgentRevision) error {
	if registration.AgentID != BuiltInAegisAgentID && revision.AgentID != BuiltInAegisAgentID {
		return nil
	}
	if registration.AgentID != BuiltInAegisAgentID || revision.AgentID != BuiltInAegisAgentID ||
		!CanonicalBuiltInAegisAgentMatches(registration, revision, revision.Ownership.AccountabilityID) {
		return ErrBuiltInImmutable
	}
	return nil
}
