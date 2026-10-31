package app

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/delivery"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"github.com/berryhill/aegis/internal/reference"
)

// Only the verifier may select the pre-issue view. The full snapshot is always
// validated first; callers cannot supply a replacement history or authorization.
type contributionPreIssueViewKey struct{}

type contributionOriginalPolicyResolver struct {
	history *ContributionDeliveryPolicyHistory
}

func (r contributionOriginalPolicyResolver) ResolveContributionDeliveryPolicy(ctx context.Context, ref reference.DigestRef) (ContributionDeliveryPolicy, error) {
	return r.history.ResolveContributionDeliveryPolicyDigest(ctx, ref.Digest)
}

func contributionPreIssueView(ctx context.Context, h delivery.HistorySnapshot) delivery.HistorySnapshot {
	if _, ok := ctx.Value(contributionPreIssueViewKey{}).(*ContributionFinalVerifier); !ok {
		return h
	}
	prior := h
	prior.Histories = nil
	for _, chain := range h.Histories {
		if chain[0].Record.Intent.Operation != delivery.ReconcileIssue {
			prior.Histories = append(prior.Histories, chain)
		}
	}
	return prior
}
func preclosureWire(a *ContributionPreClosureAuthorization) []byte {
	wire, _ := json.Marshal(struct{ Schema, Owner, Key, Contract, Candidate, Native, History, Continuity, Policy, Claim, Attempt string }{"aegis.contribution.preclosure.v1", a.owner, a.key, a.contract, a.candidate, a.native, a.history, a.continuity, a.policy, a.claim, a.attempt})
	return wire
}

// Only fresh controller provenance may differ during same-intent recovery.
// A new check policy, target, credential pin or publication payload is not a
// compatible continuation and cannot reinterpret the original preclosure.
func compatibleIssueRecoveryPolicy(original, current ContributionDeliveryPolicy) bool {
	expected := original.snapshot()
	expected.ControllerSession = current.ControllerSession
	expected.Binding.FenceEpoch = current.Binding.FenceEpoch
	expected.Binding.RoleBinding = current.Binding.RoleBinding
	return expected.Digest() == current.Digest()
}

// reloadOriginalPreclosure repeats current native/source/role/policy admission,
// but looks up the ORIGINAL content address. It never writes a new preclosure.
func (v *ContributionFinalVerifier) reloadOriginalPreclosure(ctx context.Context, sub core.Subject, key string, ref delivery.Reference) (*ContributionPreClosureAuthorization, string, delivery.Record, bool, error) {
	h, err := v.service.ReadContributionDeliveryHistoryAs(ctx, sub, key)
	if err != nil {
		if store, ok := v.service.FleetRepository.(*fleetbadger.Store); ok {
			if absent, e := store.ContributionDeliveryUnstarted(ctx, sub.PrincipalID, key); e == nil && absent {
				return nil, "", delivery.Record{}, false, nil
			}
		}
		return nil, "", delivery.Record{}, false, err
	}
	var old delivery.Record
	for _, chain := range h.Histories {
		if chain[0].Record.Intent.Operation == delivery.ReconcileIssue {
			if old.Revision != 0 {
				return nil, "", old, true, ErrDenied
			}
			old = chain[len(chain)-1].Record
		}
	}
	if old.Revision == 0 {
		return nil, "", old, false, nil
	}
	if old.Intent.Reference != ref || (old.Outcome != delivery.Unknown && old.Outcome != delivery.Confirmed) {
		return nil, "", old, true, ErrDenied
	}
	history, err := NewContributionDeliveryPolicyHistory(v.service)
	if err != nil {
		return nil, "", old, true, err
	}
	p, err := history.ResolveContributionDeliveryPolicyDigest(ctx, old.Intent.Policy)
	if err != nil {
		return nil, "", old, true, err
	}
	currentPolicy, err := v.policies.ResolveContributionDeliveryPolicy(ctx, v.policy)
	if err != nil || currentPolicy.Digest() != v.policy.Digest || !compatibleIssueRecoveryPolicy(p, currentPolicy) {
		return nil, "", old, true, ErrDenied
	}
	originalRef := v.policy
	originalRef.Digest = old.Intent.Policy
	originalPolicies := contributionOriginalPolicyResolver{history}
	original := &ContributionFinalVerifier{v.service, originalRef, originalPolicies}
	internal := context.WithValue(ctx, contributionPreIssueViewKey{}, original)
	proof, _, err := original.VerifyPreClosureAs(internal, sub, key)
	if err != nil {
		return nil, "", old, true, err
	}
	c, state, events, err := v.service.ObserveContributionContinuityAs(ctx, sub, key)
	if err != nil {
		return nil, "", old, true, err
	}
	candidate, candidateErr := v.service.AcceptedContributionCandidateAs(ctx, sub, key)
	if err != nil || candidateErr != nil {
		return nil, "", old, true, ErrDenied
	}
	source := &ContributionDeliverySource{service: v.service, subject: sub, key: key, policyRef: originalRef, policies: originalPolicies}
	plan, err := source.project(ctx, ref, p, candidate)
	if err != nil {
		return nil, "", old, true, err
	}
	plan.PR, err = retainedContributionPR(h, p)
	if err != nil {
		return nil, "", old, true, err
	}
	// A compatible continuation can append continuity without rewriting the
	// original preclosure. Search only canonical prefix digests, never blob scans.
	for _, event := range events {
		recovered := *proof
		recovered.continuity = event.Digest
		if !compatiblePreclosureContinuity(c, state, events, recovered.continuity) {
			continue
		}
		wire := preclosureWire(&recovered)
		address := contribution.Digest(wire)
		retained, e := v.service.Store.GetBlob(address)
		if e != nil || contribution.Digest(retained) != address || !bytes.Equal(retained, wire) {
			continue
		}
		plan.Completion = address
		if delivery.VerifyHistoricalPlan(old.Intent, plan) != nil {
			continue
		}
		return &recovered, address, old, true, nil
	}
	return nil, "", old, true, ErrDenied
}

// ReconcileContributionIssue uses the original binding exclusively for GET and
// observation CAS; contributionColdAuthority independently admits the CURRENT role.
func (a *ContributionCompletionAdapter) ReconcileContributionIssue(ctx context.Context, old delivery.Record, plan delivery.Plan) (delivery.Record, error) {
	if a.validateCompletionFrame(ctx, a.subject) != nil {
		return old, ErrDenied
	}
	owner, ok := a.verifier.service.ContributionPublicationQuiescer.(*ContributionDeliveryPublicationOwner)
	if !ok {
		return old, ErrDenied
	}
	return owner.reconcileOriginalIssue(ctx, a.subject, a.key, old, plan)
}

func (o *ContributionDeliveryPublicationOwner) reconcileOriginalIssue(ctx context.Context, sub core.Subject, key string, old delivery.Record, plan delivery.Plan) (delivery.Record, error) {
	if old.Intent.Operation != delivery.ReconcileIssue || delivery.VerifyHistoricalPlan(old.Intent, plan) != nil {
		return old, ErrDenied
	}
	p, err := o.policies.ResolveContributionDeliveryPolicyDigest(ctx, old.Intent.Policy)
	if err != nil || p.Digest() != old.Intent.Policy {
		return old, ErrDenied
	}
	store, ok := o.service.FleetRepository.(*fleetbadger.Store)
	if !ok {
		return old, ErrDenied
	}
	authority := contributionColdAuthority{o.service, sub, key, old.Intent}
	if authority.Admit(ctx, old.Intent, false) != nil {
		return old, ErrDenied
	}
	repo, err := store.ContributionDelivery(ctx, p.Binding, authority)
	if err != nil {
		return old, err
	}
	// Even a confirmed original must retain active repository/base custody.
	if err = repo.Fence(ctx, old.Intent, false); err != nil {
		return old, err
	}
	permit := &contributionHistoricalPermit{owner: o, subject: sub, key: key, intent: old.Intent}
	permit.active.Store(true)
	defer permit.active.Store(false)
	return delivery.ReconcileHistorical(context.WithValue(ctx, contributionHistoricalPermitKey{}, permit), repo, o.github, old, plan)
}

// RecoverContributionIssue restores only the exact historical read target and
// opaque verifier-owned authorization inside the current source frame.
func (a *ContributionCompletionAdapter) RecoverContributionIssue(ctx context.Context, ref delivery.Reference, plan delivery.Plan) (delivery.Record, delivery.Plan, bool, error) {
	if err := a.validateCompletionFrame(ctx, a.subject); err != nil {
		return delivery.Record{}, plan, false, err
	}
	proof, address, old, found, err := a.verifier.reloadOriginalPreclosure(ctx, a.subject, a.key, ref)
	if err != nil || !found {
		return old, plan, found, err
	}
	history, e := NewContributionDeliveryPolicyHistory(a.verifier.service)
	if e != nil {
		return old, plan, true, e
	}
	p, e := history.ResolveContributionDeliveryPolicyDigest(ctx, old.Intent.Policy)
	if e != nil {
		return old, plan, true, e
	}
	candidate, e := a.verifier.service.AcceptedContributionCandidateAs(ctx, a.subject, a.key)
	if e != nil {
		return old, plan, true, e
	}
	r := a.verifier.policy
	r.Digest = old.Intent.Policy
	source := &ContributionDeliverySource{service: a.verifier.service, subject: a.subject, key: a.key, policyRef: r, policies: contributionOriginalPolicyResolver{history}}
	plan, e = source.project(ctx, ref, p, candidate)
	if e != nil {
		return old, plan, true, e
	}
	plan.Completion, plan.PR = address, old.Intent.PR
	if delivery.VerifyHistoricalPlan(old.Intent, plan) != nil {
		return old, plan, true, ErrDenied
	}
	a.mu.Lock()
	a.authorization, a.reference = proof, address
	a.mu.Unlock()
	return old, plan, true, nil
}
