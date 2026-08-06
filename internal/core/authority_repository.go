package core

import (
	"context"
	"time"
)

// AuthorityRepository is the only persistence contract authority-domain callers
// should depend on. The contract deliberately exposes no generic kind, update,
// or replacement operation for immutable authority history.
type AuthorityRepository interface {
	CreateMandate(context.Context, Mandate) error
	GetMandate(context.Context, string) (Mandate, error)
	ListMandates(context.Context) ([]Mandate, error)
	CreateAuthorityContext(context.Context, AuthorityContext) error
	GetAuthorityContext(context.Context, string) (AuthorityContext, error)
	ListAuthorityContexts(context.Context) ([]AuthorityContext, error)
	AppendAuthorityTransitionFact(context.Context, AuthorityTransitionFact) (AuthorityTransitionRoot, error)
	AuthorityTransitionFacts(context.Context, string) ([]AuthorityTransitionFact, error)
	AuthorityTransitionRoot(context.Context, string) (AuthorityTransitionRoot, error)
}

// AuthorityCommandRepository is the narrow transactional authority-command
// boundary. It deliberately exposes no generic mutation or projection-replace
// operation; admission always re-verifies canonical command/fact history.
type AuthorityCommandRepository interface {
	ProcessAuthorityCommand(context.Context, AuthorityCommand, time.Time, string) (AuthorityReceipt, error)
	GetAuthorityReceipt(context.Context, string) (AuthorityReceipt, error)
	CurrentAuthorityProjection(context.Context, string) (AuthorityProjection, error)
	AuthorityOutbox(context.Context, string) ([]AuthorityOutboxEntry, error)
	CommittedAuthorityPosition(context.Context, string) (CommittedAuthorityPosition, error)
	DeliverAuthorityAudit(context.Context, string, int) ([]AuthorityAuditEvidence, error)
	AuthorityAuditEvidence(context.Context, string) ([]AuthorityAuditEvidence, error)
	AuthorityReadiness(context.Context, string, string, time.Time) (AuthorityAdmissionView, CommittedAuthorityPosition, error)
	AuthorityAdmission(context.Context, string, string, time.Time) (AuthorityAdmissionView, error)
}
