package core

import "context"

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
