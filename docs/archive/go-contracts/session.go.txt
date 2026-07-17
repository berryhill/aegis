package specs

import (
	"context"
	"time"
)

type SessionStatus string

const (
	SessionPending    SessionStatus = "pending"
	SessionRunning    SessionStatus = "running"
	SessionExpired    SessionStatus = "expired"
	SessionRevoked    SessionStatus = "revoked"
	SessionTerminated SessionStatus = "terminated"
	SessionFailed     SessionStatus = "failed"
)

type SessionPreview struct {
	Subject         AuthenticatedSubject
	AgentID         AgentID
	StanzaID        StanzaID
	CharterRevision CharterRevision
	CharterDigest   Digest
	Runtime         RuntimeDescriptor
	Target          string
	Capabilities    EffectiveCapabilities
	Scopes          ScopeSet
	ExpiresAt       time.Time
	Warnings        []string
}

type StartSessionRequest struct {
	Mandate Mandate
	Preview SessionPreview
}

type Session struct {
	ID             SessionID
	MandateID      MandateID
	RuntimeSession RuntimeSession
	Status         SessionStatus
	StartedAt      time.Time
	ExpiresAt      time.Time
	EndedAt        time.Time
	EndReason      string
}

// SessionService starts a fresh runtime context for one mandate and one stanza.
// It must never reuse transcript, memory, credentials, or tool handles from a
// different stanza unless a future explicit disclosure contract permits it.
type SessionService interface {
	Preview(context.Context, Mandate) (SessionPreview, error)
	Start(context.Context, StartSessionRequest) (Session, error)
	Get(context.Context, SessionID) (Session, error)
	List(context.Context, Page) (PageResult[Session], error)
	Revoke(context.Context, SessionID, AuthenticatedSubject, string) error
	Terminate(context.Context, SessionID, string) error
}
