package specs

import (
	"context"
	"time"
)

type InformationFlowMode string

const (
	InformationFlowDeny InformationFlowMode = "deny"
)

type InformationFlowPolicy struct {
	CrossStanza InformationFlowMode
}

type SessionPolicy struct {
	MaximumLifetime time.Duration
	IdleTimeout     time.Duration
	RequireReauth   bool
	Delegation      bool
}

type ApprovalPolicy struct {
	RequiredOperations []string
	MaximumLifetime    time.Duration
	SingleUse          bool
}

type CapabilityGrant struct {
	Capabilities []CapabilityID
	Tools        []ToolID
}

type RuntimeStanzaConfig struct {
	HermesProfile    string
	PersistentHome   bool
	MCPServers       []string
	Plugins          []string
	RuntimeOverrides map[string]string
}

type TrustStanza struct {
	ID              StanzaID
	Name            string
	Authentication  AuthenticationPolicy
	Grant           CapabilityGrant
	Scopes          ScopeSet
	Session         SessionPolicy
	Approval        ApprovalPolicy
	InformationFlow InformationFlowPolicy
	Runtime         RuntimeStanzaConfig
	Enabled         bool
}

type Charter struct {
	SchemaVersion string
	AgentID       AgentID
	Name          string
	Revision      CharterRevision
	Runtime       RuntimeConstraint
	Stanzas       []TrustStanza
	CreatedBy     PrincipalID
	CreatedAt     time.Time
}

type CanonicalCharter struct {
	Charter Charter
	Bytes   []byte
	Digest  Digest
}

// CharterCodec owns deterministic serialization and digest calculation.
// Implementations must reject unknown fields during decoding.
type CharterCodec interface {
	EncodeCanonical(Charter) (CanonicalCharter, error)
	DecodeStrict([]byte) (Charter, error)
}

type CharterValidator interface {
	Validate(context.Context, Charter) error
}

type CharterRepository interface {
	SaveDraft(context.Context, CanonicalCharter) error
	Get(context.Context, AgentID, CharterRevision) (CanonicalCharter, error)
	List(context.Context, AgentID) ([]CanonicalCharter, error)
	MarkApproved(context.Context, AgentID, CharterRevision, ApprovalID) error
}
