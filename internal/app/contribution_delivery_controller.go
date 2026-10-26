package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/credentials"
	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/orchestration"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"github.com/berryhill/aegis/internal/reference"
)

// ContributionDeliveryServerPolicy is trusted composition input, never a request
// model. There is deliberately no default identity, check policy or credential.
// Constructor snapshots it; changing the caller's slices cannot change authority.
type ContributionDeliveryServerPolicy struct {
	// Zero is disabled; only delivery.NewRecheckPolicy constructs enabled bounds.
	Readiness                                          delivery.RecheckPolicy
	Convention                                         string
	Actor                                              string
	Author, Committer                                  delivery.Identity
	ProtectionPolicy                                   string
	RequiredChecks, RequiredReviewers, PostMergeChecks []string
	MergeMethod                                        string
	Binding                                            credentials.CredentialBinding
}

type ContributionDeliveryController struct {
	service     *Service
	policy      ContributionDeliveryServerPolicy
	credentials *credentials.Authority
	privacy     delivery.Privacy
}

func NewContributionDeliveryController(s *Service, p *ContributionDeliveryServerPolicy, a *credentials.Authority, privacy delivery.Privacy) (*ContributionDeliveryController, error) {
	if s == nil || p == nil || a == nil || privacy == nil {
		return nil, ErrContributionDeliveryUnavailable
	}
	q := *p
	q.RequiredChecks = slices.Clone(p.RequiredChecks)
	q.RequiredReviewers = slices.Clone(p.RequiredReviewers)
	q.PostMergeChecks = slices.Clone(p.PostMergeChecks)
	q.Binding.Destinations = slices.Clone(p.Binding.Destinations)
	validIdentity := func(i delivery.Identity) bool {
		return i.Name != "" && i.Email != "" && !strings.ContainsAny(i.Name+i.Email, "<>\x00\r\n")
	}
	if q.Convention != "contribution-publication.v1" || q.Actor == "" || !validIdentity(q.Author) || !validIdentity(q.Committer) || q.ProtectionPolicy == "" || len(q.RequiredChecks) == 0 || len(q.RequiredReviewers) == 0 || len(q.PostMergeChecks) == 0 || (q.MergeMethod != "merge" && q.MergeMethod != "squash" && q.MergeMethod != "rebase") {
		return nil, delivery.ErrDenied
	}
	for _, list := range [][]string{q.RequiredChecks, q.RequiredReviewers, q.PostMergeChecks} {
		seen := map[string]bool{}
		for _, x := range list {
			if x == "" || seen[x] {
				return nil, delivery.ErrDenied
			}
			seen[x] = true
		}
	}
	return &ContributionDeliveryController{s, q, a, privacy}, nil
}

var ErrContributionDeliveryApprovalUnavailable = errors.New("contribution_delivery_approval_unavailable")

func validContributionDeliveryRemoteBinding(b credentials.CredentialBinding) bool {
	return credentials.ValidateBinding(b, b.Key.DeploymentID) == nil && b.Enabled && b.BindingRevision > 0 && b.VersionPolicy == credentials.VersionPinned && len(b.Destinations) == 1
}

type fixedContributionDeliveryPolicy struct{ p ContributionDeliveryPolicy }

func (f fixedContributionDeliveryPolicy) ResolveContributionDeliveryPolicy(_ context.Context, r reference.DigestRef) (ContributionDeliveryPolicy, error) {
	if r.Digest != f.p.Digest() {
		return ContributionDeliveryPolicy{}, delivery.ErrDenied
	}
	return f.p.snapshot(), nil
}

type productiveContributionAuthority struct {
	controller            *ContributionDeliveryController
	subject               core.Subject
	key, session, control string
	request               contribution.EffectAdmissionRequest
	plan                  delivery.Plan
	live                  atomic.Bool
}

func (a *productiveContributionAuthority) Admit(ctx context.Context, i delivery.Intent, _ bool) error {
	if a == nil || ctx == nil || ctx.Err() != nil || !a.live.Load() {
		return delivery.ErrDenied
	}
	s := a.controller.service
	control, err := s.ReadContributionControlAs(ctx, a.subject, a.key)
	wire, _ := json.Marshal(control)
	if err != nil || control.Stop || contribution.Digest(wire) != a.control {
		return delivery.ErrDenied
	}
	// Productive reads require the SAME fresh runtime/private custody as writes.
	// The engine's writing flag remains independent for Store/adapter semantics.
	role, err := s.CheckContributionEffectAs(ctx, a.subject, a.session, a.key, a.request, true)
	if err != nil || role.Ref() != a.request.RoleAuthority || role.SessionID != a.session {
		return delivery.ErrDenied
	}
	ac, err := s.authorityContextForSession(ctx, a.session)
	if err != nil {
		return delivery.ErrDenied
	}
	// RequiredOperations are exact operation identifiers, not a global veto.
	// No approval consumer is installed here: only the applicable operation holds.
	if slices.Contains(ac.Authority.Approval.RequiredOperations, string(i.Operation)) {
		return ErrContributionDeliveryApprovalUnavailable
	}
	if i.Operation != delivery.Commit {
		b := a.controller.policy.Binding
		if !validContributionDeliveryRemoteBinding(b) || ac.AgentID != b.Key.AgentID || ac.Authority.StanzaID != b.Key.StanzaID || !slices.Contains(ac.Authority.Credentials, b.Key.Scope) {
			return delivery.ErrDenied
		}
		m, e := s.Authority.GetMandate(ctx, role.MandateID)
		if e != nil || m.DeploymentID != b.Key.DeploymentID {
			return delivery.ErrDenied
		}
	}
	p := a.plan
	if i.Operation == delivery.ReconcileIssue {
		return delivery.ErrDenied
	} // #379 owner not installed; issue remains open.
	if i.Operation == delivery.UpdatePullRequest || i.Operation == delivery.CheckDelivery || i.Operation == delivery.Merge {
		h, e := s.ReadContributionDeliveryHistoryAs(ctx, a.subject, a.key)
		if e != nil {
			return delivery.ErrDenied
		}
		for _, chain := range h.Histories {
			r := chain[len(chain)-1].Record
			if r.Intent.Operation == delivery.PullRequest && r.Intent.Reference == p.Reference && r.Outcome == delivery.Confirmed && r.PR != 0 && delivery.ValidateProjectedIntent(p, r.Intent) == nil {
				if p.PR != 0 && p.PR != r.PR {
					return delivery.ErrDenied
				}
				p.PR = r.PR
			}
		}
		if p.PR == 0 {
			return delivery.ErrDenied
		}
	}
	return delivery.ValidateProjectedIntent(p, i)
}

// RunAs is the complete public input surface: no resolver, policy, actor, Plan,
// payload, candidate receipt or PR number can be supplied by the operator.
// No installed manager hook or remote readiness is implied by this constructor.
func (d *ContributionDeliveryController) RunAs(ctx context.Context, subject core.Subject, key, session string) (delivery.DeliveryResult, error) {
	return d.withProductive(ctx, subject, key, session, nil)
}

// withProductive keeps authority construction inside the actual private custody
// callback. The public controller always executes the closed conductor path.
func (d *ContributionDeliveryController) withProductive(ctx context.Context, subject core.Subject, key, session string, inspect func(context.Context, delivery.Plan, delivery.Authority, delivery.Repository, ContributionDeliveryPolicy) error) (out delivery.DeliveryResult, err error) {
	if d == nil || d.service == nil || ctx == nil || ctx.Err() != nil || session == "" {
		return out, delivery.ErrDenied
	}
	s := d.service
	installed, ok := s.ContributionPublicationQuiescer.(*ContributionDeliveryPublicationOwner)
	if !ok || installed.service != s {
		return out, ErrContributionDeliveryUnavailable
	}
	leave, err := installed.enter(ctx, subject, key)
	if err != nil {
		return out, err
	}
	defer leave()
	c, state, _, err := s.ObserveContributionContinuityAs(ctx, subject, key)
	if err != nil {
		return out, delivery.ErrDenied
	}
	role, err := s.CheckContributionRoleAs(ctx, subject, session, key, "controller", ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: state.Sequence, ExpectedDigest: state.Digest})
	if err != nil {
		return out, delivery.ErrDenied
	}
	store, ok := s.FleetRepository.(*fleetbadger.Store)
	if !ok {
		return out, ErrContributionDeliveryUnavailable
	}
	revision, err := s.FleetRepository.GetLoopRevision(ctx, c.Loop.ID, c.Loop.Revision)
	if err != nil || revision.Digest != c.Loop.Digest || revision.Contribution == nil || revision.Contribution.Managed == nil {
		return out, delivery.ErrDenied
	}
	var files []string
	for _, step := range revision.Steps {
		if step.Implementation != nil {
			if files != nil {
				return out, delivery.ErrDenied
			}
			files = slices.Clone(step.Implementation.WritableFiles)
		}
	}
	if len(files) == 0 {
		return out, delivery.ErrDenied
	}
	ref := delivery.Reference{Run: c.RunID, Contract: c.Digest, Candidate: state.CandidateEvidence.Digest}
	err = s.WithContributionContinuityCandidateAs(ctx, subject, session, key, func(live context.Context, v orchestration.ContributionCandidate) error {
		p := ContributionDeliveryPolicy{ControllerSession: session, Binding: fleetbadger.ContributionDeliveryBinding{QueueItemID: c.QueueItemID, OwnerID: c.OwnerID, Key: key, Reference: ref, FenceEpoch: role.Epoch, RoleBinding: role.Ref().Digest, Base: revision.Contribution.Managed.BaseBranch}, Loop: c.Loop, Workspace: c.Worktree, Source: v.Source.Digest, Tree: v.CandidateTree, BaseHead: c.Base, CandidatePolicy: v.Policy, Toolchain: v.Toolchain, QueueAttempt: v.QueueAttempt, Actor: d.policy.Actor, Branch: "contribution/v1/" + strings.TrimPrefix(c.Digest, "sha256:"), Timestamp: c.AdmittedAt.UTC().Format(time.RFC3339), Message: fmt.Sprintf("Contribution for issue %d\n", c.Source.URL.Number), Author: d.policy.Author, Committer: d.policy.Committer, AllowedFiles: files, ProtectionPolicy: d.policy.ProtectionPolicy, RequiredChecks: d.policy.RequiredChecks, RequiredReviewers: d.policy.RequiredReviewers, PostMergeChecks: d.policy.PostMergeChecks, MergeMethod: d.policy.MergeMethod, PRTitle: fmt.Sprintf("Contribution for issue %d", c.Source.URL.Number), PRBody: "Controller-owned contribution (contribution-publication.v1)."}
		if c.Effect != "local-only" && c.Effect != "no-publish" {
			b := d.policy.Binding
			if !validContributionDeliveryRemoteBinding(b) {
				return delivery.ErrDenied
			}
			p.CredentialPin = &ContributionDeliveryCredentialPin{Key: b.Key, BindingRevision: b.BindingRevision, SecretRecord: b.SecretRecord, Version: b.PinnedVersion, Destination: b.Destinations[0], Actor: d.policy.Actor, RepositoryID: c.Source.RepositoryID, Operations: []delivery.Operation{delivery.UploadBlob, delivery.UploadTree, delivery.UploadCommit, delivery.Publish, delivery.PullRequest, delivery.UpdatePullRequest, delivery.CheckDelivery, delivery.Merge}}
		}
		policyRef := reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: "contribution-publication-v1", Digest: p.Digest()}
		source := &ContributionDeliverySource{service: s, subject: subject, key: key, policyRef: policyRef, policies: fixedContributionDeliveryPolicy{p}}
		plan, e := source.project(live, ref, p, v)
		if e != nil {
			return e
		}
		p.OriginalCommit = &plan.Commit
		p = p.snapshot()
		policyRef.Digest = p.Digest()
		plan.Policy = policyRef.Digest
		if e = s.retainContributionDeliveryPolicy(live, p); e != nil {
			return e
		}
		control, e := s.ReadContributionControlAs(live, subject, key)
		if e != nil || control.Stop {
			return delivery.ErrDenied
		}
		wire, _ := json.Marshal(control)
		a := &productiveContributionAuthority{controller: d, subject: subject, key: key, session: session, control: contribution.Digest(wire), plan: plan, request: contribution.EffectAdmissionRequest{Contract: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: c.QueueItemID, Digest: c.Digest}, RunID: c.RunID, QueueItemID: c.QueueItemID, Candidate: ref.Candidate, CandidateEvidence: state.CandidateEvidence, Role: "controller", Epoch: role.Epoch, RoleAuthority: role.Ref(), Scope: c.Effect}}
		a.live.Store(true)
		defer a.live.Store(false)
		authority := &contributionInstructionAuthority{service: s, subject: subject, key: key, ref: ref, base: a}
		owner, e := store.ContributionDelivery(live, p.Binding, authority)
		if e != nil {
			return e
		}
		if inspect != nil {
			return inspect(live, plan, authority, owner, p)
		}
		var gh *delivery.GitHub
		if c.Effect != "local-only" && c.Effect != "no-publish" {
			b := d.policy.Binding
			if !validContributionDeliveryRemoteBinding(b) {
				return ErrContributionDeliveryUnavailable
			}
			custody, e := delivery.NewCredentialCustody(d.credentials, b.Key, b.Destinations[0], b.SecretRecord, b.PinnedVersion, b.BindingRevision, c.Repository, d.policy.Actor, c.Source.RepositoryID, []delivery.Operation{delivery.UploadBlob, delivery.UploadTree, delivery.UploadCommit, delivery.Publish, delivery.PullRequest, delivery.UpdatePullRequest, delivery.CheckDelivery, delivery.Merge})
			if e != nil {
				return e
			}
			gh, e = delivery.NewGitHub("https://api.github.com", custody, d.privacy)
			if e != nil {
				return e
			}
		}
		// A synchronous single-use source passes the already-held native projection.
		held := &heldContributionDeliverySource{plan: plan}
		budget := &contributionDeliveryReadiness{live: live, productive: a, instructions: authority}
		out, e = delivery.NewConductor(delivery.NewEngine(owner, held, authority, delivery.ClosedAdapter{Git: &delivery.Git{Binary: revision.Contribution.Managed.GitBinary, Directory: c.Worktree}, GitHub: gh}), nil, nil, delivery.WithReadiness(d.policy.Readiness, budget)).Run(live, ref)
		return e
	})
	return out, err
}

type heldContributionDeliverySource struct {
	plan delivery.Plan
	used atomic.Bool
}

func (s *heldContributionDeliverySource) WithAccepted(ctx context.Context, r delivery.Reference, fn func(context.Context, delivery.Plan) error) error {
	if ctx == nil || ctx.Err() != nil || fn == nil || r != s.plan.Reference || !s.used.CompareAndSwap(false, true) {
		return delivery.ErrDenied
	}
	return fn(ctx, s.plan)
}
