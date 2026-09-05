package orchestration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/berryhill/aegis/internal/reference"
)

const WorkspaceAuthoritySchemaVersion = "aegis.workspace-authority.v1"

type WorkspaceCapability string

const (
	WorkspaceDefineLoops     WorkspaceCapability = "fleet.loops.define"
	WorkspaceManageLoops     WorkspaceCapability = "fleet.loops.manage-own"
	WorkspaceDefineGraphs    WorkspaceCapability = "fleet.graphs.define"
	WorkspaceSubmitGraphs    WorkspaceCapability = "fleet.graphs.submit-participant"
	WorkspaceManageOwnQueue  WorkspaceCapability = "fleet.queue.manage-own"
	WorkspaceReadDefinitions WorkspaceCapability = "fleet.definitions.read-shared"
)

var registeredAgentWorkspaceCapabilities = []WorkspaceCapability{
	WorkspaceDefineLoops, WorkspaceManageLoops, WorkspaceDefineGraphs,
	WorkspaceSubmitGraphs, WorkspaceManageOwnQueue, WorkspaceReadDefinitions,
}

// WorkspaceAuthority is controller-issued control-plane delegation for one
// exact latest enabled registered Agent. It intentionally carries no runtime,
// session, claim, credential, or provisioning authority.
type WorkspaceAuthority struct {
	SchemaVersion string                `json:"schema_version"`
	ID            string                `json:"id"`
	PrincipalID   string                `json:"principal_id"`
	Agent         reference.RevisionRef `json:"agent"`
	OwnerID       string                `json:"owner_id"`
	Capabilities  []WorkspaceCapability `json:"capabilities"`
	Digest        string                `json:"digest"`
}

func NewRegisteredAgentWorkspaceAuthority(principalID string, agent reference.RevisionRef, ownerID string) (WorkspaceAuthority, error) {
	value := WorkspaceAuthority{SchemaVersion: WorkspaceAuthoritySchemaVersion, ID: "workspace-" + agent.ID, PrincipalID: principalID, Agent: agent, OwnerID: ownerID, Capabilities: append([]WorkspaceCapability(nil), registeredAgentWorkspaceCapabilities...)}
	if err := validateWorkspaceAuthority(value, false); err != nil {
		return WorkspaceAuthority{}, err
	}
	value.Digest = WorkspaceAuthorityDigest(value)
	return value, nil
}

func WorkspaceAuthorityDigest(value WorkspaceAuthority) string {
	value.Digest = ""
	wire, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(wire)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func ValidateWorkspaceAuthority(value WorkspaceAuthority) error {
	return validateWorkspaceAuthority(value, true)
}

func validateWorkspaceAuthority(value WorkspaceAuthority, sealed bool) error {
	if value.SchemaVersion != WorkspaceAuthoritySchemaVersion || value.ID != "workspace-"+value.Agent.ID || value.PrincipalID == "" || value.OwnerID == "" || value.Agent.Validate() != nil {
		return errors.New("registered-Agent workspace authority is incomplete")
	}
	if len(value.Capabilities) != len(registeredAgentWorkspaceCapabilities) {
		return errors.New("registered-Agent workspace authority capability set is not exact")
	}
	for i := range registeredAgentWorkspaceCapabilities {
		if value.Capabilities[i] != registeredAgentWorkspaceCapabilities[i] {
			return errors.New("registered-Agent workspace authority capability set is not exact")
		}
	}
	if sealed && (value.Digest == "" || value.Digest != WorkspaceAuthorityDigest(value)) {
		return errors.New("registered-Agent workspace authority digest does not match")
	}
	return nil
}

func (value WorkspaceAuthority) Ref() reference.DigestRef {
	return reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: value.ID, Digest: value.Digest}
}

func (value WorkspaceAuthority) Allows(capability WorkspaceCapability) bool {
	if ValidateWorkspaceAuthority(value) != nil {
		return false
	}
	for _, candidate := range value.Capabilities {
		if candidate == capability {
			return true
		}
	}
	return false
}
