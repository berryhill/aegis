package core

import (
	"errors"
	"time"
)

// IngressObservation is an authenticated transport observation established
// outside the model. EndpointRef is opaque and must never contain credentials.
type IngressObservation struct {
	ID          string    `json:"id"`
	SubjectID   string    `json:"subject_id"`
	ContactID   string    `json:"contact_id"`
	ChannelID   string    `json:"channel_id"`
	Channel     string    `json:"channel"`
	EndpointRef string    `json:"endpoint_ref"`
	ObservedAt  time.Time `json:"observed_at"`
}

// AuthorityContext is the immutable authority instantiated for exactly one
// runtime session. Mandate remains the longer-lived authorization grant; this
// record binds one session to one effective authority and runtime revision.
type AuthorityContext struct {
	ID              string             `json:"id"`
	MandateID       string             `json:"mandate_id"`
	SessionID       string             `json:"session_id"`
	SubjectID       string             `json:"subject_id"`
	AgentID         string             `json:"agent_id"`
	CharterRevision uint64             `json:"charter_revision"`
	CharterDigest   string             `json:"charter_digest"`
	Runtime         RuntimeDescriptor  `json:"runtime"`
	Authority       EffectiveAuthority `json:"authority"`
	IssuedAt        time.Time          `json:"issued_at"`
	ExpiresAt       time.Time          `json:"expires_at"`
	Digest          string             `json:"digest"`
}

func AuthorityContextDigest(context AuthorityContext) string {
	context.Digest = ""
	return Digest(context)
}

func ValidateAuthorityContext(context AuthorityContext, mandate Mandate) error {
	if context.ID == "" || context.MandateID != mandate.ID || context.SessionID == "" ||
		context.SubjectID != mandate.Subject.ID || context.AgentID != mandate.AgentID ||
		context.CharterRevision != mandate.CharterRevision || context.CharterDigest != mandate.CharterDigest ||
		Digest(context.Runtime) != Digest(mandate.Runtime) || context.Authority.StanzaID != mandate.StanzaID ||
		context.IssuedAt.Before(mandate.IssuedAt) || context.ExpiresAt.After(mandate.ExpiresAt) ||
		!context.ExpiresAt.After(context.IssuedAt) || context.Digest != AuthorityContextDigest(context) {
		return errors.New("authority context does not exactly bind its immutable mandate and session")
	}
	if Digest(context.Authority.Capabilities) != Digest(mandate.Capabilities) ||
		Digest(context.Authority.Tools) != Digest(mandate.Tools) ||
		Digest(context.Authority.Memory) != Digest(mandate.Scopes.Memory) ||
		Digest(context.Authority.Credentials) != Digest(mandate.Scopes.Credentials) ||
		Digest(context.Authority.Hermes) != Digest(mandate.Hermes) {
		return errors.New("authority context widens or changes mandate authority")
	}
	return nil
}

// AuthorityRevocation is an append-only fact. It never mutates a mandate or
// authority context in place.
type AuthorityRevocation struct {
	ID                 string    `json:"id"`
	MandateID          string    `json:"mandate_id"`
	AuthorityContextID string    `json:"authority_context_id,omitempty"`
	Reason             string    `json:"reason"`
	RevokedAt          time.Time `json:"revoked_at"`
	RecordedBy         string    `json:"recorded_by"`
}

func AuthorityEffectiveAt(context AuthorityContext, revocations []AuthorityRevocation, at time.Time) bool {
	if at.Before(context.IssuedAt) || !at.Before(context.ExpiresAt) || context.Digest != AuthorityContextDigest(context) {
		return false
	}
	for _, revocation := range revocations {
		if revocation.MandateID == context.MandateID &&
			(revocation.AuthorityContextID == "" || revocation.AuthorityContextID == context.ID) &&
			!at.Before(revocation.RevokedAt) {
			return false
		}
	}
	return true
}
