package specs

import (
	"context"
	"time"
)

type AuthenticationRequest struct {
	Method      string
	Credential  []byte
	Channel     string
	RequestedAt time.Time
}

// Authenticator verifies identity outside the model. Implementations must fail
// closed and must not derive identity from prompt text, display names, or a
// requested stanza.
type Authenticator interface {
	Authenticate(context.Context, AuthenticationRequest) (AuthenticatedSubject, error)
}

type PrincipalDecision struct {
	Allowed     bool
	PrincipalID PrincipalID
	Reason      EventReason
	FreshUntil  time.Time
}

// PrincipalAuthorizer verifies that an authenticated subject is the principal
// and that its authentication is fresh enough for the requested operation.
type PrincipalAuthorizer interface {
	AuthorizePrincipal(context.Context, AuthenticatedSubject, string) (PrincipalDecision, error)
}

type IdentitySelector struct {
	Kinds        []SubjectKind
	SubjectIDs   []SubjectID
	PrincipalIDs []PrincipalID
	Issuers      []string
	ClaimEquals  map[string]string
}

type AuthenticationPolicy struct {
	AllowedMethods []string
	Selectors      []IdentitySelector
	RequireFresh   bool
	MaxAuthAge     time.Duration
}
