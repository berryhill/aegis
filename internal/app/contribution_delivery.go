package app

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/credentials"
	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/orchestration"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"github.com/berryhill/aegis/internal/reference"
)

var ErrContributionDeliveryUnavailable = errors.New("contribution delivery native continuity integration unavailable")

// ContributionDeliveryPolicy is a reproducible controller projection, not a
// grant, runtime request or model artifact. Productive callers use the concrete
// trusted controller, which derives it from canonical state and server policy.
// ContributionDeliveryCredentialPin is immutable nonsecret approval metadata, not authority.
type ContributionDeliveryCredentialPin struct {
	Key             credentials.CredentialBindingKey
	BindingRevision uint64
	SecretRecord    string
	Version         uint64
	Destination     string
	Actor           string
	RepositoryID    uint64
	Operations      []delivery.Operation
}

type ContributionDeliveryPolicy struct {
	// OriginalCommit preserves the exact publication payload, including reviewed files.
	// Nil retains legacy policy digests and denies historical Publish reconstruction.
	OriginalCommit *delivery.CommitSpec               `json:",omitempty"`
	CredentialPin  *ContributionDeliveryCredentialPin `json:",omitempty"`
	// ControllerSession is an explicit trusted controller-role pin, never inferred from intake.
	ControllerSession                                                           string
	Binding                                                                     fleetbadger.ContributionDeliveryBinding
	Loop                                                                        reference.RevisionRef
	Workspace, Source, Tree, BaseHead, CandidatePolicy, Toolchain, QueueAttempt string
	Actor, Branch, ExpectedOld, Timestamp, Message                              string
	Author, Committer                                                           delivery.Identity
	AllowedFiles                                                                []string
	ProtectionPolicy                                                            string
	RequiredChecks, RequiredReviewers, PostMergeChecks                          []string
	MergeMethod, PRTitle, PRBody                                                string
	PR                                                                          uint64
}

// snapshot detaches slices from the trusted immutable resolver store. Callers
// must not mutate incoming policy storage concurrently with resolution.
func (p ContributionDeliveryPolicy) snapshot() ContributionDeliveryPolicy {
	if p.CredentialPin != nil {
		pin := *p.CredentialPin
		pin.Operations = slices.Clone(pin.Operations)
		p.CredentialPin = &pin
	}
	if p.OriginalCommit != nil {
		commit := *p.OriginalCommit
		commit.Files = slices.Clone(commit.Files)
		for n := range commit.Files {
			commit.Files[n].Bytes = slices.Clone(commit.Files[n].Bytes)
		}
		p.OriginalCommit = &commit
	}
	p.AllowedFiles = slices.Clone(p.AllowedFiles)
	p.RequiredChecks = slices.Clone(p.RequiredChecks)
	p.RequiredReviewers = slices.Clone(p.RequiredReviewers)
	p.PostMergeChecks = slices.Clone(p.PostMergeChecks)
	return p
}

// projectPolicy detaches even a retained callback Plan from the resolved snapshot.
func (p ContributionDeliveryPolicy) projectPolicy(plan delivery.Plan) delivery.Plan {
	p = p.snapshot()
	plan.RequiredChecks = p.RequiredChecks
	plan.RequiredReviewers = p.RequiredReviewers
	plan.PostMergeChecks = p.PostMergeChecks
	return plan
}

func (p ContributionDeliveryPolicy) Digest() string {
	b, _ := json.Marshal(p)
	return contribution.Digest(b)
}

// ContributionDeliveryPolicyResolver must be installed by the trusted controller
// owner; no request can supply policy prose, credentials, candidate bytes or PASS.
type ContributionDeliveryPolicyResolver interface {
	ResolveContributionDeliveryPolicy(context.Context, reference.DigestRef) (ContributionDeliveryPolicy, error)
}

type ContributionDeliverySource struct {
	service   *Service
	subject   core.Subject
	key       string
	policyRef reference.DigestRef
	policies  ContributionDeliveryPolicyResolver
	run       contributionDeliveryCustodyRunner
}

var _ delivery.Source = (*ContributionDeliverySource)(nil)

// contributionDeliveryCustodyRunner owns synchronous candidate custody. Production
// delegates exactly once to #377; it never reacquires the #376 source lock.
type contributionDeliveryCustodyRunner func(context.Context, string, func(context.Context, orchestration.ContributionCandidate) error) error
type contributionContinuityCandidateOwner interface {
	WithContributionContinuityCandidateAs(context.Context, core.Subject, string, string, func(context.Context, orchestration.ContributionCandidate) error) error
}

// NewContributionDeliverySource is productive only with the actual #377 owner.
// The legacy gate parameter is not a substitute for that owner or its private proof.
func NewContributionDeliverySource(s *Service, subject core.Subject, key string, ref reference.DigestRef, policies ContributionDeliveryPolicyResolver, _ orchestration.ContributionQuiescenceGate) (*ContributionDeliverySource, error) {
	if s == nil {
		return nil, ErrContributionDeliveryUnavailable
	}
	return newContributionDeliverySource(s, subject, key, ref, policies, func(ctx context.Context, session string, action func(context.Context, orchestration.ContributionCandidate) error) error {
		return s.WithContributionContinuityCandidateAs(ctx, subject, session, key, action)
	})
}

// NewQualificationContributionDeliverySource deliberately selects the real #376
// lock-held kernel with a SYNTHETIC gate. It is NOT productive #377 admission.
// Only qualification tests should call this constructor; production never falls back.
func NewQualificationContributionDeliverySource(s *Service, subject core.Subject, key string, ref reference.DigestRef, policies ContributionDeliveryPolicyResolver, gate orchestration.ContributionQuiescenceGate) (*ContributionDeliverySource, error) {
	if gate == nil {
		return nil, ErrContributionDeliveryUnavailable
	}
	return newContributionDeliverySource(s, subject, key, ref, policies, func(ctx context.Context, _ string, action func(context.Context, orchestration.ContributionCandidate) error) error {
		return s.WithAcceptedContributionCandidateAs(ctx, subject, key, gate, action)
	})
}
func newContributionDeliverySource(s *Service, subject core.Subject, key string, ref reference.DigestRef, policies ContributionDeliveryPolicyResolver, run contributionDeliveryCustodyRunner) (*ContributionDeliverySource, error) {
	if s == nil || policies == nil || run == nil || ref.Validate() != nil || !contribution.ValidKey(key) {
		return nil, delivery.ErrDenied
	}
	return &ContributionDeliverySource{s, subject, key, ref, policies, run}, nil
}
func (s *ContributionDeliverySource) WithAccepted(ctx context.Context, ref delivery.Reference, action func(context.Context, delivery.Plan) error) error {
	if s == nil || s.service == nil {
		return delivery.ErrDenied
	}
	var bindErr error
	ctx, bindErr = s.service.bindContributionRequestSubject(ctx, s.subject)
	if bindErr != nil {
		return bindErr
	}
	if s == nil || s.run == nil {
		return ErrContributionDeliveryUnavailable
	}
	if action == nil || ctx.Err() != nil {
		return delivery.ErrDenied
	}
	policy, e := s.policies.ResolveContributionDeliveryPolicy(ctx, s.policyRef)
	policy = policy.snapshot()
	if e != nil || policy.Digest() != s.policyRef.Digest || policy.Binding.Reference != ref || policy.ControllerSession == "" {
		return delivery.ErrDenied
	}
	// All source and effect consumers are synchronous; no callback is stored.
	var controlAnchor string
	gate := func(cctx context.Context, c orchestration.ContributionCandidate) error {
		current, err := s.policies.ResolveContributionDeliveryPolicy(cctx, s.policyRef)
		current = current.snapshot()
		if err != nil || current.Digest() != policy.Digest() {
			return delivery.ErrDenied
		}
		control, err := s.service.ReadContributionControlAs(cctx, s.subject, s.key)
		if err != nil || control.Stop {
			return delivery.ErrDenied
		}
		wire, _ := json.Marshal(control)
		anchor := contribution.Digest(wire)
		if controlAnchor != "" && controlAnchor != anchor {
			return delivery.ErrDenied
		}
		controlAnchor = anchor
		return nil
	}
	var mu sync.Mutex
	active, called := true, false
	e = s.run(ctx, policy.ControllerSession, func(cctx context.Context, c orchestration.ContributionCandidate) error {
		mu.Lock()
		defer mu.Unlock()
		if !active || called || cctx == nil || cctx.Err() != nil || ctx.Err() != nil {
			return delivery.ErrDenied
		}
		called = true
		if gate(cctx, c) != nil {
			return delivery.ErrDenied
		}
		p, x := s.project(cctx, ref, policy, c)
		if x != nil {
			return delivery.ErrDenied
		}
		if x = s.service.retainContributionDeliveryPolicy(cctx, policy); x != nil {
			return delivery.ErrDenied
		}
		if x = action(cctx, p); x != nil {
			return x
		}
		if cctx.Err() != nil || ctx.Err() != nil {
			return delivery.ErrDenied
		}
		return gate(cctx, c)
	})
	mu.Lock()
	active = false
	if !called && e == nil {
		e = delivery.ErrDenied
	}
	mu.Unlock()
	if e != nil {
		return delivery.ErrDenied
	}
	return nil
}
func (s *ContributionDeliverySource) project(ctx context.Context, ref delivery.Reference, p ContributionDeliveryPolicy, v orchestration.ContributionCandidate) (delivery.Plan, error) {
	deny := func() (delivery.Plan, error) { return delivery.Plan{}, delivery.ErrDenied }
	current, e := s.policies.ResolveContributionDeliveryPolicy(ctx, s.policyRef)
	current = current.snapshot()
	if e != nil || current.Digest() != p.Digest() || p.Digest() != s.policyRef.Digest {
		return deny()
	}
	c, _, e := s.service.ReadContributionAs(ctx, s.subject, s.key)
	if e != nil {
		return deny()
	}
	b := p.Binding
	if b.Reference != ref || b.OwnerID != s.subject.PrincipalID || b.Key != s.key || b.QueueItemID != c.QueueItemID || b.Run != c.RunID || b.Contract != c.Digest || v.Contract != c.Digest || v.QueueAttempt != p.QueueAttempt || p.QueueAttempt == "" || v.Digest != ref.Candidate || p.Loop != c.Loop || p.Workspace != c.Worktree || p.BaseHead != c.Base || v.Base != c.Base || v.CandidateTree != p.Tree || v.Source.Digest != p.Source || v.Policy != p.CandidatePolicy || v.Toolchain != p.Toolchain || v.Builder.AgentDigest != c.Builder.Digest || v.Builder.CharterDigest != c.Roles[1].Charter.Digest || b.FenceEpoch == 0 || b.RoleBinding == "" {
		return deny()
	}
	revision, e := s.service.FleetRepository.GetLoopRevision(ctx, c.Loop.ID, c.Loop.Revision)
	if e != nil || revision.Digest != c.Loop.Digest || revision.Contribution == nil || revision.Contribution.Managed == nil {
		return deny()
	}
	managed := revision.Contribution.Managed
	if managed.BaseBranch != b.Base {
		return deny()
	}
	control, e := s.service.ReadContributionControlAs(ctx, s.subject, s.key)
	if e != nil || control.Stop {
		return deny()
	}
	target := c.Effect
	if control.Sequence > 0 {
		target, e = contribution.RestrictDelivery(c.Effect, control.Delivery)
		if e != nil {
			return deny()
		}
	}
	g := &delivery.Git{Binary: managed.GitBinary, Directory: c.Worktree}
	commit, objects, e := g.ProjectAccepted(ctx, v, delivery.CommitSpec{Parent: c.Base, Tree: v.CandidateTree, Branch: p.Branch, Author: p.Author, Committer: p.Committer, Timestamp: p.Timestamp, Message: p.Message}, p.AllowedFiles)
	if e != nil {
		return deny()
	}
	return p.projectPolicy(delivery.Plan{Reference: ref, FenceEpoch: b.FenceEpoch, RoleBinding: b.RoleBinding, Intake: c, Actor: p.Actor, Repository: c.Repository, RepositoryID: c.Source.RepositoryID, Base: b.Base, Branch: p.Branch, Head: commit.Commit, BaseHead: c.Base, ExpectedOld: p.ExpectedOld, Policy: s.policyRef.Digest, Delivery: target, Commit: commit, Objects: objects, ProtectionPolicy: p.ProtectionPolicy, RequiredChecks: p.RequiredChecks, RequiredReviewers: p.RequiredReviewers, PostMergeChecks: p.PostMergeChecks, MergeMethod: p.MergeMethod, PR: p.PR, PRTitle: p.PRTitle, PRBody: p.PRBody}), nil
}

// RunContributionDeliveryAs conducts the bounded ordered sequence using existing
// canonical Badger custody and actual native Source integration. It is not an
// automatic manager hook; unavailable #377 composition remains unavailable.
func (s *Service) RunContributionDeliveryAs(ctx context.Context, subject core.Subject, key string, ref delivery.Reference, policyRef reference.DigestRef, policies ContributionDeliveryPolicyResolver, authority delivery.Authority, adapter delivery.Adapter, repair delivery.RepairOwner, completion delivery.CompletionOwner, readiness ...delivery.ReadinessOption) (delivery.DeliveryResult, error) {
	if installed, ok := s.ContributionPublicationQuiescer.(*ContributionDeliveryPublicationOwner); ok {
		if installed.service != s {
			return delivery.DeliveryResult{}, delivery.ErrDenied
		}
		leave, err := installed.enter(ctx, subject, key)
		if err != nil {
			return delivery.DeliveryResult{}, err
		}
		defer leave()
	} else {
		c, _, err := s.ReadContributionAs(ctx, subject, key)
		if err != nil || c.Effect != "local-only" {
			return delivery.DeliveryResult{}, ErrContributionDeliveryUnavailable
		}
	}
	if policies == nil || authority == nil || adapter == nil {
		return delivery.DeliveryResult{}, ErrContributionDeliveryUnavailable
	}
	source, err := NewContributionDeliverySource(s, subject, key, policyRef, policies, nil)
	if err != nil {
		return delivery.DeliveryResult{}, err
	}
	p, err := policies.ResolveContributionDeliveryPolicy(ctx, policyRef)
	p = p.snapshot()
	if err != nil || p.Digest() != policyRef.Digest || p.Binding.Reference != ref {
		return delivery.DeliveryResult{}, delivery.ErrDenied
	}
	store, ok := s.FleetRepository.(*fleetbadger.Store)
	if !ok {
		return delivery.DeliveryResult{}, ErrContributionDeliveryUnavailable
	}
	authority = &contributionInstructionAuthority{service: s, subject: subject, key: key, ref: ref, base: authority}
	owner, err := store.ContributionDelivery(ctx, p.Binding, authority)
	if err != nil {
		return delivery.DeliveryResult{}, err
	}
	return delivery.NewConductor(delivery.NewEngine(owner, source, authority, adapter), repair, completion, readiness...).Run(ctx, ref)
}

// ExecuteContributionDeliveryAs is the typed synchronous downstream consumer.
// It uses the existing canonical Badger adapter and Engine, never a new queue.
// The trusted caller supplies per-effect authority; the actual #377 owner
// supplies custody. Missing integrations deny. No manager continuation or transport is silently installed.
func (s *Service) ExecuteContributionDeliveryAs(ctx context.Context, subject core.Subject, key string, ref delivery.Reference, policyRef reference.DigestRef, policies ContributionDeliveryPolicyResolver, gate orchestration.ContributionQuiescenceGate, authority delivery.Authority, adapter delivery.Adapter, op delivery.Operation) (delivery.Record, error) {
	if installed, ok := s.ContributionPublicationQuiescer.(*ContributionDeliveryPublicationOwner); ok {
		if installed.service != s {
			return delivery.Record{}, delivery.ErrDenied
		}
		leave, err := installed.enter(ctx, subject, key)
		if err != nil {
			return delivery.Record{}, err
		}
		defer leave()
	} else {
		c, _, err := s.ReadContributionAs(ctx, subject, key)
		if err != nil || c.Effect != "local-only" {
			return delivery.Record{}, ErrContributionDeliveryUnavailable
		}
	}
	if authority == nil || adapter == nil {
		return delivery.Record{}, ErrContributionDeliveryUnavailable
	}
	source, e := NewContributionDeliverySource(s, subject, key, policyRef, policies, gate)
	if e != nil {
		return delivery.Record{}, e
	}
	p, e := policies.ResolveContributionDeliveryPolicy(ctx, policyRef)
	p = p.snapshot()
	if e != nil || p.Digest() != policyRef.Digest || p.Binding.Reference != ref {
		return delivery.Record{}, delivery.ErrDenied
	}
	store, ok := s.FleetRepository.(*fleetbadger.Store)
	if !ok {
		return delivery.Record{}, ErrContributionDeliveryUnavailable
	}
	authority = &contributionInstructionAuthority{service: s, subject: subject, key: key, ref: ref, base: authority}
	owner, e := store.ContributionDelivery(ctx, p.Binding, authority)
	if e != nil {
		return delivery.Record{}, delivery.ErrDenied
	}
	return delivery.NewEngine(owner, source, authority, adapter).Execute(ctx, ref, op)
}
