package app

import (
	"context"
	"sync"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/delivery"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"github.com/berryhill/aegis/internal/reference"
)

// ContributionDeliveryHistoricalPolicies is an immutable owner-provided lookup,
// not a request-supplied policy or an accepting fallback.
type ContributionDeliveryHistoricalPolicies interface {
	ResolveContributionDeliveryPolicyDigest(context.Context, string) (ContributionDeliveryPolicy, error)
}

// ContributionDeliveryPublicationOwner holds only active call lifetimes. All
// authority, stop state and effect facts remain in the existing canonical store.
// freshSubject retrieves existing fresh read authority; it must not authenticate,
// renew a mandate or rotate credentials as a side effect.
type ContributionDeliveryPublicationOwner struct {
	service      *Service
	policies     ContributionDeliveryHistoricalPolicies
	github       *delivery.GitHub
	freshSubject func(context.Context) (core.Subject, error)
	mu           sync.Mutex
	controller   *ContributionDeliveryController // immutable trusted setup, guarded by mu
	active       map[string]int
	changed      chan struct{}
}

// InstallContributionDeliveryPublicationOwner installs the existing #377 port.
// Trusted composition must retain this same object for delivery execution.
func InstallContributionDeliveryPublicationOwner(s *Service, policies ContributionDeliveryHistoricalPolicies, github *delivery.GitHub, freshSubject func(context.Context) (core.Subject, error)) (*ContributionDeliveryPublicationOwner, error) {
	if s == nil || policies == nil || github == nil || freshSubject == nil || s.ContributionPublicationQuiescer != nil {
		return nil, ErrContributionDeliveryUnavailable
	}
	if _, ok := s.FleetRepository.(*fleetbadger.Store); !ok {
		return nil, ErrContributionDeliveryUnavailable
	}
	o := &ContributionDeliveryPublicationOwner{service: s, policies: policies, github: github, freshSubject: freshSubject, active: make(map[string]int), changed: make(chan struct{})}
	s.ContributionPublicationQuiescer = o
	return o, nil
}

func (o *ContributionDeliveryPublicationOwner) enter(ctx context.Context, sub core.Subject, key string) (func(), error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	// Register before admission, so the cancel join cannot overlook a call which
	// passed admission but has not yet reserved. Sticky canonical Stop wins races.
	control, err := o.service.ReadContributionControlAs(ctx, sub, key)
	if err != nil || control.Stop {
		return nil, delivery.ErrDenied
	}
	o.active[key]++
	return func() {
		o.mu.Lock()
		defer o.mu.Unlock()
		o.active[key]--
		close(o.changed)
		o.changed = make(chan struct{})
	}, nil
}

type contributionColdAuthority struct {
	service *Service
	subject core.Subject
	key     string
	intent  delivery.Intent
}

func (a contributionColdAuthority) Admit(ctx context.Context, i delivery.Intent, writing bool) error {
	if a.service.validateContributionRequestSubject(ctx, a.subject) != nil {
		return delivery.ErrDenied
	}
	if writing || i != a.intent {
		return delivery.ErrDenied
	}
	c, state, _, err := a.service.ObserveContributionContinuityAs(ctx, a.subject, a.key)
	if err != nil || c.OwnerID != a.subject.PrincipalID || c.Digest != i.Contract || c.RunID != i.Run || state.CandidateEvidence.Digest != i.Candidate {
		return delivery.ErrDenied
	}
	role, ok := state.RoleAuthorities["controller"]
	if !ok || role.Epoch != i.FenceEpoch || role.Ref().Digest != i.RoleBinding {
		return delivery.ErrDenied
	}
	_, err = a.service.CheckContributionEffectAs(ctx, a.subject, role.SessionID, a.key, contribution.EffectAdmissionRequest{Contract: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: c.QueueItemID, Digest: c.Digest}, RunID: c.RunID, QueueItemID: c.QueueItemID, Scope: c.Effect, Candidate: i.Candidate, CandidateEvidence: state.CandidateEvidence, Role: "controller", RoleAuthority: role.Ref(), Epoch: role.Epoch}, false)
	return err
}

// ReconcileAs derives every target from canonical root history. No caller target,
// role/epoch, runtime, candidate callback or workspace write grant is accepted.
func (o *ContributionDeliveryPublicationOwner) ReconcileAs(ctx context.Context, sub core.Subject, key string) (delivery.HistorySnapshot, error) {
	return o.reconcileAs(ctx, sub, key, false)
}

func (o *ContributionDeliveryPublicationOwner) reconcileAs(ctx context.Context, sub core.Subject, key string, quiescing bool) (delivery.HistorySnapshot, error) {
	ctx, bindErr := o.service.bindContributionRequestSubject(ctx, sub)
	if bindErr != nil {
		return delivery.HistorySnapshot{}, bindErr
	}
	h, err := o.service.ReadContributionDeliveryHistoryAs(ctx, sub, key)
	if err != nil {
		return h, err
	}
	store, ok := o.service.FleetRepository.(*fleetbadger.Store)
	if !ok {
		return h, ErrContributionDeliveryUnavailable
	}
	for _, chain := range h.Histories {
		old := chain[len(chain)-1].Record
		if quiescing && old.Intent.Operation == delivery.CheckDelivery {
			continue
		}
		if old.Outcome != delivery.Unknown || old.Intent.Operation == delivery.TransferGraph || quiescing && old.Intent.Operation == delivery.Merge && old.Merge != "" {
			continue
		}
		i := old.Intent
		p, e := o.policies.ResolveContributionDeliveryPolicyDigest(ctx, i.Policy)
		p = p.snapshot()
		if e != nil || p.Digest() != i.Policy || p.Binding.Reference != i.Reference || p.Binding.OwnerID != sub.PrincipalID || p.Binding.Key != key || p.Binding.QueueItemID != h.Contract.QueueItemID || p.Binding.FenceEpoch != i.FenceEpoch || p.Binding.RoleBinding != i.RoleBinding || p.Binding.Base != i.Base {
			return h, delivery.ErrDenied
		}
		// Only immutable metadata is reconstructed. Missing completion/graph artifacts
		// fail the exact payload comparison instead of weakening it.
		plan := p.projectPolicy(delivery.Plan{Reference: i.Reference, FenceEpoch: i.FenceEpoch, RoleBinding: i.RoleBinding, Intake: h.Contract, Actor: p.Actor, Repository: h.Contract.Repository, RepositoryID: h.Contract.Source.RepositoryID, Base: p.Binding.Base, Branch: p.Branch, Head: i.Head, BaseHead: p.BaseHead, ExpectedOld: p.ExpectedOld, Policy: i.Policy, Delivery: h.Contract.Effect, PR: i.PR, PRTitle: p.PRTitle, PRBody: p.PRBody, ProtectionPolicy: p.ProtectionPolicy, MergeMethod: p.MergeMethod, Commit: delivery.CommitSpec{Tree: p.Tree}})
		// Exact prior proof may retain a narrowed delivery scope or completion
		// reference which is not present in policy configuration. It is metadata,
		// never new completion authority; the full intent payload must still match.
		if old.ProofJSON != "" {
			proof, parseErr := delivery.ParseReadback(i, old.Observation)
			if parseErr != nil || proof.Requirements == nil {
				return h, delivery.ErrDenied
			}
			plan.Delivery = proof.Requirements.Delivery
			plan.Completion = proof.Requirements.Completion
		}
		if i.Operation == delivery.Publish {
			if p.OriginalCommit == nil {
				return h, delivery.ErrDenied
			}
			plan.Commit = *p.OriginalCommit
		}
		if delivery.VerifyHistoricalPlan(i, plan) != nil {
			return h, delivery.ErrDenied
		}
		authority := contributionColdAuthority{o.service, sub, key, i}
		repo, e := store.ContributionDelivery(ctx, p.Binding, authority)
		if e != nil {
			return h, e
		}
		permit := &contributionHistoricalPermit{owner: o, subject: sub, key: key, intent: i}
		permit.active.Store(true)
		historical := context.WithValue(ctx, contributionHistoricalPermitKey{}, permit)
		_, e = delivery.ReconcileHistorical(historical, repo, o.github, old, plan)
		permit.active.Store(false)
		if e != nil {
			return h, e
		}
	}
	return o.service.ReadContributionDeliveryHistoryAs(ctx, sub, key)
}

func (o *ContributionDeliveryPublicationOwner) drain(ctx context.Context, key string) error {
	for {
		o.mu.Lock()
		n := o.active[key]
		changed := o.changed
		o.mu.Unlock()
		if n == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

var _ ContributionPublicationQuiescer = (*ContributionDeliveryPublicationOwner)(nil)

func (o *ContributionDeliveryPublicationOwner) QuiesceContributionPublication(ctx context.Context, binding ContributionPublicationBinding) error {
	sub, err := o.freshSubject(ctx)
	if err != nil {
		return err
	}
	// The binding carries no contribution key. Resolve it only in the same owned
	// canonical store, never through caller repo/ref or a process-local index.
	store := o.service.FleetRepository.(*fleetbadger.Store)
	contract, err := store.GetContributionPublicationContract(ctx, sub.PrincipalID, binding.QueueItemID)
	if err != nil || contract.OwnerID != sub.PrincipalID {
		return delivery.ErrDenied
	}
	c, state, _, err := o.service.ObserveContributionContinuityAs(ctx, sub, contract.Key)
	if err != nil || binding.Contract != (reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: c.QueueItemID, Digest: c.Digest}) || binding.RunID != c.RunID || binding.QueueItemID != c.QueueItemID || binding.Candidate != state.CandidateEvidence {
		return delivery.ErrDenied
	}
	role, ok := state.RoleAuthorities["controller"]
	if !ok || role.Ref() != binding.Controller || role.Epoch != binding.Epoch || state.RoleEpochs["controller"] != binding.Epoch {
		return delivery.ErrDenied
	}
	control, err := o.service.ReadContributionControlAs(ctx, sub, c.Key)
	if err != nil || !control.Stop {
		return delivery.ErrDenied
	}
	if err := o.drain(ctx, c.Key); err != nil {
		return err
	}
	// An empty tracker is never durable absence, especially after process restart.
	if absent, e := store.ContributionDeliveryAuthoritativeAbsence(ctx, sub.PrincipalID, c.Key); e != nil {
		return e
	} else if absent {
		return nil
	}
	h, err := o.reconcileAs(ctx, sub, c.Key, true)
	if err != nil {
		return err
	}
	for _, chain := range h.Histories {
		r := chain[len(chain)-1].Record
		if r.Intent.Operation == delivery.CheckDelivery || r.Intent.Operation == delivery.TransferGraph {
			continue
		}
		if r.Outcome == delivery.Unknown && !(r.Intent.Operation == delivery.Merge && r.Merge != "") {
			return delivery.ErrDenied
		}
	}
	return nil
}
