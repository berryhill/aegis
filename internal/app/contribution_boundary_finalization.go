package app

import (
	"context"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/delivery"
)

func effectiveContributionScope(c contribution.Contract, control contribution.Control) (string, error) {
	if control.Sequence == 0 {
		return c.Effect, nil
	}
	return contribution.RestrictDelivery(c.Effect, control.Delivery)
}
func classifyContributionHeldHistory(h delivery.HistorySnapshot, candidate string) (string, string) {
	return delivery.ClassifyContributionNonSuccess(h, candidate)
}

// FinalizeContributionBoundary is a key-owned actual producer join, not a
// caller-supplied outcome. It cannot supply issue-close authorization.
func (a *ContributionCompletionAdapter) FinalizeContributionBoundary(ctx context.Context, ref delivery.Reference) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.verifier.service
	c, _, err := s.ReadContributionAs(ctx, a.subject, a.key)
	if err != nil {
		return err
	}
	control, err := s.ReadContributionControlAs(ctx, a.subject, a.key)
	if err != nil {
		return err
	}
	scope, err := effectiveContributionScope(c, control)
	if err != nil {
		return err
	}
	h, err := s.ReadContributionDeliveryHistoryAs(ctx, a.subject, a.key)
	if err != nil {
		return err
	}
	outcome, _ := classifyContributionHeldHistory(h, ref.Candidate)
	mode := "narrow"
	if outcome != "" {
		mode = "held"
	} else if scope == "verified-protected-base-merge" {
		return ErrDenied
	}
	auth, _, err := a.verifier.verifyContributionAuthorization(ctx, a.subject, a.key, mode)
	if err != nil {
		return err
	}
	_, err = a.verifier.finalizeContributionAs(ctx, a.subject, a.key, auth, "", ref)
	return err
}

// Explicit owner entry for accepted-source retained failures. No raw outcome or
// records are accepted and no native/GitHub effect is dispatched by this method.
func (v *ContributionFinalVerifier) FinalizeRetainedContributionOutcomeAs(ctx context.Context, sub core.Subject, key string) (ContributionResult, error) {
	if v == nil || v.service == nil {
		return ContributionResult{}, ErrDenied
	}
	s := v.service
	if existing, e := s.GetContributionResultAs(ctx, sub, key); e == nil {
		return existing, nil
	}
	auth, _, err := v.verifyContributionAuthorization(ctx, sub, key, "held")
	if err != nil {
		return ContributionResult{}, err
	}
	c, _, err := s.ReadContributionAs(ctx, sub, key)
	if err != nil {
		return ContributionResult{}, err
	}
	return v.finalizeContributionAs(ctx, sub, key, auth, "", delivery.Reference{Run: c.RunID, Contract: c.Digest, Candidate: auth.candidate})
}
