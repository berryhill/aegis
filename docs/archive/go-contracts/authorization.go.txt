package specs

import (
	"context"
	"time"
)

type StanzaSelectionRequest struct {
	Subject     AuthenticatedSubject
	Charter     CanonicalCharter
	Requested   StanzaID
	Environment Environment
	RequestedAt time.Time
}

type StanzaDecision struct {
	Allowed       bool
	Selected      TrustStanza
	MatchingCount int
	Reason        EventReason
	CharterDigest Digest
}

// StanzaSelector must return exactly one enabled, authorized stanza. Zero
// matches returns ErrNoMatchingStanza; multiple matches return
// ErrAmbiguousStanza. It must never union grants from different stanzas.
type StanzaSelector interface {
	Select(context.Context, StanzaSelectionRequest) (StanzaDecision, error)
}

type EffectiveCapabilities struct {
	Capabilities []CapabilityID
	Tools        []ToolID
	DeniedTools  []ToolID
}

type CapabilityRequest struct {
	Runtime RuntimeDescriptor
	Charter CanonicalCharter
	Stanza  TrustStanza
}

// CapabilityResolver returns a concrete, wildcard-free set before launch.
type CapabilityResolver interface {
	Resolve(context.Context, CapabilityRequest) (EffectiveCapabilities, error)
}

type Mandate struct {
	ID               MandateID
	Subject          AuthenticatedSubject
	AgentID          AgentID
	StanzaID         StanzaID
	CharterRevision  CharterRevision
	CharterDigest    Digest
	Runtime          RuntimeDescriptor
	Capabilities     EffectiveCapabilities
	Scopes           ScopeSet
	IssuedAt         time.Time
	ExpiresAt        time.Time
	RevokedAt        time.Time
	RevocationReason string
}

type MandateRequest struct {
	Decision     StanzaDecision
	Subject      AuthenticatedSubject
	Runtime      RuntimeDescriptor
	Capabilities EffectiveCapabilities
	Now          time.Time
}

type MandateIssuer interface {
	Issue(context.Context, MandateRequest) (Mandate, error)
	Get(context.Context, MandateID) (Mandate, error)
	Revoke(context.Context, MandateID, string) error
	Validate(context.Context, Mandate, time.Time) error
}
