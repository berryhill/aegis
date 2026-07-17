package specs

import "time"

type (
	AgentID          string
	ApprovalID       string
	AuditEventID     string
	CapabilityID     string
	CharterRevision  uint64
	CredentialScope  string
	Digest           string
	EventReason      string
	MandateID        string
	MemoryScope      string
	PrincipalID      string
	ProvisioningID   string
	RuntimeAdapterID string
	RuntimeID        string
	RuntimeSessionID string
	SessionID        string
	StanzaID         string
	SubjectID        string
	ToolID           string
)

type SubjectKind string

const (
	SubjectHuman   SubjectKind = "human"
	SubjectAgent   SubjectKind = "agent"
	SubjectService SubjectKind = "service"
	SubjectUnknown SubjectKind = "unknown"
)

// AuthenticatedSubject is constructed only by an Authenticator. Caller text,
// display names, model output, and requested stanza names are never identity
// evidence.
type AuthenticatedSubject struct {
	ID              SubjectID
	Kind            SubjectKind
	PrincipalID     PrincipalID
	Issuer          string
	Authentication  string
	AuthenticatedAt time.Time
	ExpiresAt       time.Time
	Claims          map[string]string
}

type RuntimeDescriptor struct {
	AdapterID      RuntimeAdapterID
	RuntimeID      RuntimeID
	Name           string
	Version        string
	Installation   string
	AdapterVersion string
	Capabilities   []CapabilityID
}

type RuntimeConstraint struct {
	AdapterID         RuntimeAdapterID
	RuntimeID         RuntimeID
	VersionConstraint string
	Target            string
}

type ScopeSet struct {
	Memory      []MemoryScope
	Credentials []CredentialScope
}

type Environment struct {
	Name   string
	Host   string
	Tenant string
}

type Page struct {
	Limit  int
	Cursor string
}

type PageResult[T any] struct {
	Items      []T
	NextCursor string
}
