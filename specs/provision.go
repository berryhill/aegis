package specs

import (
	"context"
	"time"
)

type EffectKind string

const (
	EffectCreateFile      EffectKind = "create_file"
	EffectModifyFile      EffectKind = "modify_file"
	EffectCreateProfile   EffectKind = "create_profile"
	EffectInstallPlugin   EffectKind = "install_plugin"
	EffectConfigureMCP    EffectKind = "configure_mcp"
	EffectStartGateway    EffectKind = "start_gateway"
	EffectInstallService  EffectKind = "install_service"
	EffectCreateCron      EffectKind = "create_cron"
	EffectExternalRequest EffectKind = "external_request"
)

type PlannedEffect struct {
	Kind          EffectKind
	Target        string
	Description   string
	Consequential bool
	Digest        Digest
}

type ProvisioningPlan struct {
	ID            ProvisioningID
	AgentID       AgentID
	Revision      CharterRevision
	CharterDigest Digest
	Runtime       RuntimeDescriptor
	Environment   Environment
	Effects       []PlannedEffect
	CreatedAt     time.Time
}

type ProvisioningStatus string

const (
	ProvisioningPlanned    ProvisioningStatus = "planned"
	ProvisioningApplied    ProvisioningStatus = "applied"
	ProvisioningVerified   ProvisioningStatus = "verified"
	ProvisioningFailed     ProvisioningStatus = "failed"
	ProvisioningRolledBack ProvisioningStatus = "rolled_back"
)

type ArtifactReceipt struct {
	Path     string
	Action   EffectKind
	Digest   Digest
	Verified bool
}

type ProvisioningReceipt struct {
	ID            ProvisioningID
	Status        ProvisioningStatus
	CharterDigest Digest
	ApprovalID    ApprovalID
	Runtime       RuntimeDescriptor
	Artifacts     []ArtifactReceipt
	StartedAt     time.Time
	FinishedAt    time.Time
	Failure       string
}

// Provisioner is separate from Designer. Apply must consume an exact approval
// and apply deterministic application logic, never model-generated shell
// commands. Consequential effects not present in the approved plan are denied.
type Provisioner interface {
	Preview(context.Context, CanonicalCharter, RuntimeDescriptor, Environment) (ProvisioningPlan, error)
	Apply(context.Context, ProvisioningPlan, Approval) (ProvisioningReceipt, error)
	Verify(context.Context, ProvisioningReceipt) (ProvisioningReceipt, error)
	GetReceipt(context.Context, ProvisioningID) (ProvisioningReceipt, error)
}
