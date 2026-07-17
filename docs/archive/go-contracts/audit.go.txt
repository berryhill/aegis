package specs

import (
	"context"
	"time"
)

type AuditEventType string

const (
	AuditAuthentication AuditEventType = "authentication"
	AuditDesignSession  AuditEventType = "design_session"
	AuditCharter        AuditEventType = "charter"
	AuditApproval       AuditEventType = "approval"
	AuditProvisioning   AuditEventType = "provisioning"
	AuditSession        AuditEventType = "session"
	AuditRevocation     AuditEventType = "revocation"
)

type AuditEvent struct {
	ID              AuditEventID
	Type            AuditEventType
	OccurredAt      time.Time
	SubjectID       SubjectID
	PrincipalID     PrincipalID
	AgentID         AgentID
	StanzaID        StanzaID
	SessionID       SessionID
	MandateID       MandateID
	Runtime         RuntimeDescriptor
	CharterRevision CharterRevision
	CharterDigest   Digest
	ApprovalID      ApprovalID
	ProvisioningID  ProvisioningID
	Outcome         string
	Reason          EventReason
	PreviousDigest  Digest
	EventDigest     Digest
	Metadata        map[string]string
}

// AuditSink is the authoritative append-only event surface. Runtime and model
// processes must not receive update or delete authority over committed events.
type AuditSink interface {
	Append(context.Context, AuditEvent) error
}

type AuditFilter struct {
	AgentID   AgentID
	SessionID SessionID
	SubjectID SubjectID
	Type      AuditEventType
	After     time.Time
	Before    time.Time
}

type AuditReader interface {
	List(context.Context, AuditFilter, Page) (PageResult[AuditEvent], error)
	Verify(context.Context) error
}
