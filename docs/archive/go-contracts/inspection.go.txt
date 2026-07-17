package specs

import "context"

type AuthorizationExplanation struct {
	SubjectID     SubjectID
	AgentID       AgentID
	Requested     StanzaID
	Allowed       bool
	Selected      StanzaID
	MatchingCount int
	Reason        EventReason
}

type AgentSummary struct {
	AgentID        AgentID
	Name           string
	LatestRevision CharterRevision
	Runtime        RuntimeConstraint
	Stanzas        []StanzaID
}

type EffectiveStanza struct {
	Stanza       TrustStanza
	Capabilities EffectiveCapabilities
	Runtime      RuntimeDescriptor
}

// Inspector supports CLI and API inspection through the same application
// service. Explanations must use trusted policy inputs, not model narration.
type Inspector interface {
	ListAgents(context.Context, Page) (PageResult[AgentSummary], error)
	GetCharter(context.Context, AgentID, CharterRevision) (CanonicalCharter, error)
	EffectiveStanza(context.Context, AgentID, CharterRevision, StanzaID) (EffectiveStanza, error)
	ExplainAuthorization(context.Context, StanzaSelectionRequest) (AuthorizationExplanation, error)
	ListSessions(context.Context, Page) (PageResult[Session], error)
	GetProvisioningReceipt(context.Context, ProvisioningID) (ProvisioningReceipt, error)
}

// Services is the transport-neutral application surface shared by Cobra and
// Echo. Transports authenticate callers and call these services; they do not
// reimplement policy.
type Services struct {
	Authenticator       Authenticator
	PrincipalAuthorizer PrincipalAuthorizer
	Runtimes            RuntimeRegistry
	Designer            Designer
	Charters            CharterRepository
	CharterCodec        CharterCodec
	CharterValidator    CharterValidator
	Stanzas             StanzaSelector
	Capabilities        CapabilityResolver
	Mandates            MandateIssuer
	Approvals           ApprovalService
	Provisioner         Provisioner
	Sessions            SessionService
	Audit               AuditSink
	AuditReader         AuditReader
	Inspector           Inspector
}
