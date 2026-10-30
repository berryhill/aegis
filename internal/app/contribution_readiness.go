package app

import (
	"context"
	"errors"
	"os"
	"slices"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

// ContributionReadinessInput selects a published revision, never a replacement
// definition. ExpectedService is an optional exact caller observation, not a grant.
type ContributionReadinessInput struct {
	Loop            reference.RevisionRef        `json:"loop"`
	ExpectedService *ContributionServiceIdentity `json:"expected_service,omitempty"`
	Scope           string                       `json:"scope,omitempty"`
}
type ContributionRoleReadiness struct {
	Role         string                  `json:"role"`
	Agent        reference.RevisionRef   `json:"agent"`
	Charter      reference.RevisionRef   `json:"charter"`
	Runtime      registry.RuntimeBinding `json:"runtime"`
	Reference    DoerPrerequisiteStatus  `json:"reference"`
	Selection    DoerPrerequisiteStatus  `json:"selection"`
	Provisioning DoerPrerequisiteStatus  `json:"provisioning"`
	Provider     DoerPrerequisiteStatus  `json:"provider"`
}

// Observational only. PreparationEligible is NOT execution, provider qualification
// or whole-contribution PASS. Every operation repeats its own fresh admission.
type ContributionReadiness struct {
	Loop                 reference.RevisionRef             `json:"loop"`
	Service              ContributionServiceIdentity       `json:"service"`
	Provenance           DoerPrerequisiteStatus            `json:"provenance"`
	Profile              DoerPrerequisiteStatus            `json:"profile"`
	Native               DoerPrerequisiteStatus            `json:"native"`
	Remote               DoerPrerequisiteStatus            `json:"remote"`
	RemoteAuthentication DoerPrerequisiteStatus            `json:"remote_authentication"`
	Roles                []ContributionRoleReadiness       `json:"roles"`
	Repository           string                            `json:"repository,omitempty"`
	RepositoryID         uint64                            `json:"repository_id,omitempty"`
	Base                 string                            `json:"base,omitempty"`
	Effect               string                            `json:"effect,omitempty"`
	NativeScope          *loop.ContributionExecutionPolicy `json:"native_scope,omitempty"`
	PreparationEligible  bool                              `json:"preparation_eligible"`
	Reason               string                            `json:"reason"`
	RequiredActions      []string                          `json:"required_actions"`
}

func (s *Service) ReadContributionReadinessAs(ctx context.Context, sub core.Subject, in ContributionReadinessInput) (ContributionReadiness, error) {
	pending := DoerPrerequisiteStatus{State: "not_checked"}
	ready := DoerPrerequisiteStatus{State: "ready"}
	r := ContributionReadiness{Loop: in.Loop, Service: CurrentContributionServiceIdentity(), Provenance: pending, Profile: pending, Native: pending, Remote: pending, RemoteAuthentication: pending, Roles: []ContributionRoleReadiness{}, RequiredActions: []string{}, Reason: "prerequisites_required"}
	if ctx == nil || s.requireFleetPrincipal(sub) != nil {
		return r, ErrDenied
	}
	if err := ctx.Err(); err != nil {
		return r, err
	}
	block := func(status *DoerPrerequisiteStatus, reason, action string) {
		*status = DoerPrerequisiteStatus{State: "blocked", Reason: reason}
		if r.Reason == "prerequisites_required" {
			r.Reason = reason
		}
		if action != "" && !slices.Contains(r.RequiredActions, action) {
			r.RequiredActions = append(r.RequiredActions, action)
		}
	}
	if in.ExpectedService != nil && *in.ExpectedService != r.Service {
		block(&r.Provenance, "candidate_service_mismatch", "select_matching_owning_service")
		return r, nil
	}
	// An unbound development build is observable, but cannot qualify an exact source.
	if r.Service.SourceRevision == "" {
		block(&r.Provenance, "service_source_unqualified", "build_revision_bound_candidate")
	} else {
		r.Provenance = ready
	}
	if in.Loop == (reference.RevisionRef{}) {
		block(&r.Profile, "contribution_profile_absent", "publish_and_select_exact_contribution_loop")
		return r, nil
	}
	if in.Loop.Validate() != nil {
		return r, ErrDenied
	}
	view, err := s.GetLoopViewAs(ctx, sub, in.Loop.ID, in.Loop.Revision)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, fleet.ErrNotFound) {
			block(&r.Profile, "contribution_profile_absent", "publish_and_select_exact_contribution_loop")
			return r, nil
		}
		return r, err
	}
	rev := view.Revision
	if rev.Digest != in.Loop.Digest {
		block(&r.Profile, "contribution_profile_digest_changed", "select_exact_published_revision")
		return r, nil
	}
	if rev.SchemaVersion != loop.ContributionRevisionSchemaVersion || rev.Contribution == nil || loop.ValidateRevision(rev).Outcome != loop.ValidationValid {
		block(&r.Profile, "contribution_profile_required", "publish_and_select_exact_contribution_loop")
		return r, nil
	}
	p := rev.Contribution
	r.Repository, r.RepositoryID, r.Base = p.Repository, p.RepositoryID, p.Base
	effect, err := contributionReadinessScope(p.Effect, in.Scope)
	if err != nil {
		return r, err
	}
	r.Effect = effect
	if view.Lifecycle.State != loop.LifecycleActive || view.Lifecycle.ActiveDigest != rev.Digest || view.Lifecycle.ActiveRevision != rev.Revision {
		block(&r.Profile, "active_contribution_profile_required", "activate_exact_published_revision")
	} else if p.Managed == nil {
		block(&r.Profile, "intake_only_unqualified_profile", "publish_managed_contribution_profile")
	} else {
		r.Profile = ready
		r.NativeScope = p.Managed
	}
	for i, ref := range []reference.RevisionRef{p.Controller, p.Builder, p.Tester} {
		role := ContributionRoleReadiness{Role: []string{"controller", "builder", "tester"}[i], Agent: ref, Reference: pending, Selection: pending, Provisioning: pending, Provider: pending}
		a, e := s.FleetRepository.LatestAgentRevision(ctx, ref.ID)
		if e != nil || a.Revision != ref.Revision || a.Digest != ref.Digest || a.Lifecycle != registry.LifecycleEnabled || a.Ownership.OwnerID != sub.PrincipalID {
			block(&role.Reference, "exact_owned_role_required", "review_exact_role_bindings")
			r.Roles = append(r.Roles, role)
			continue
		}
		role.Reference, role.Charter, role.Runtime = ready, a.Charter, a.Runtime
		ch, e := s.GetCharter(a.Charter.ID, a.Charter.Revision)
		if e != nil || ch.Digest != a.Charter.Digest {
			block(&role.Selection, "exact_charter_unavailable", "review_exact_role_charter")
			r.Roles = append(r.Roles, role)
			continue
		}
		selected, e := s.Select(ch, sub, "", core.Environment{Name: "local"})
		if e != nil || selected.Selected == nil {
			block(&role.Selection, "session_selection_"+selected.Reason, "review_role_trust_selection")
			r.Roles = append(r.Roles, role)
			continue
		}
		role.Selection = ready
		verified, e := s.hasVerifiedReceipt(a.Charter.Digest)
		if e != nil || !verified {
			block(&role.Provisioning, "exact_role_provisioning_required", "protected_role_provisioning")
		} else {
			role.Provisioning = ready
		}
		stanza := selected.Selected
		if stanza.Hermes.ProviderAuthentication != nil {
			if _, e = s.resolveControllerProviderAuthentication(ctx, stanza.Hermes); e != nil {
				block(&role.Provider, "role_provider_authentication_required", "protected_controller_provider_setup")
			} else {
				role.Provider = ready
			}
		} else if stanza.Hermes.LocalInference != nil || stanza.Hermes.Provider == "none" {
			role.Provider = ready
		} else {
			// Omission preserves the historical credential-scope contract.
			// Ordinary session preparation owns credential admission; neither
			// missing controller Codex binding nor provider="none" is inferred.
			role.Provider = DoerPrerequisiteStatus{State: "configured", Reason: "ordinary_provider_credential_admission_required"}
		}
		if i == 0 {
			r.Remote = ready
			if effect != "local-only" && effect != "no-publish" {
				ops := []delivery.Operation{delivery.Commit, delivery.UploadBlob, delivery.UploadTree, delivery.UploadCommit, delivery.Publish, delivery.PullRequest, delivery.UpdatePullRequest, delivery.CheckDelivery}
				if effect == "verified-protected-base-merge" {
					ops = append(ops, delivery.Merge, delivery.ReconcileIssue)
				}
				for _, op := range ops {
					caps := stanza.Grant.Capabilities
					if op != delivery.Commit && (!slices.Contains(caps, string(op)) || !slices.Contains(caps, ContributionDeliveryResourceCapability(op, p.Repository, p.RepositoryID))) {
						block(&r.Remote, "exact_repository_remote_scope_required", "review_controller_repository_authority")
						break
					}
					if slices.Contains(stanza.Approval.RequiredOperations, string(op)) {
						block(&r.Remote, "contribution_delivery_approval_unavailable", "independent_remote_operation_approval")
						break
					}
				}
				// Credential configuration and remote permissions are NOT proven by a
				// capability declaration. Actual delivery repeats broker/actor admission.
				r.RemoteAuthentication = DoerPrerequisiteStatus{State: "configured", Reason: "fresh_remote_actor_permission_admission_required"}
				if s.Config.ContributionDelivery == nil || s.CredentialAuthority == nil {
					block(&r.RemoteAuthentication, "remote_controller_setup_required", "protected_controller_delivery_setup")
				}
			} else {
				r.RemoteAuthentication = DoerPrerequisiteStatus{State: "not_required"}
			}
		}
		r.Roles = append(r.Roles, role)
	}
	if p.Managed != nil {
		controller, e := s.FleetRepository.LatestAgentRevision(ctx, p.Controller.ID)
		if e != nil || s.QueueWorker == nil || s.QueueWorker.CheckContributionHostAuthorization(rev, controller) != nil {
			block(&r.Native, "exact_contribution_native_scope_required", "protected_exact_revision_host_setup")
		} else {
			r.Native = ready
		}
	}
	r.PreparationEligible = r.Provenance.State == "ready" && r.Profile.State == "ready" && r.Native.State == "ready" && r.Remote.State == "ready"
	for _, role := range r.Roles {
		r.PreparationEligible = r.PreparationEligible && role.Reference.State == "ready" && role.Selection.State == "ready" && role.Provisioning.State == "ready" && (role.Provider.State == "ready" || role.Provider.State == "configured")
	}
	if r.RemoteAuthentication.State != "configured" && r.RemoteAuthentication.State != "not_required" {
		r.PreparationEligible = false
	}
	if r.PreparationEligible {
		r.Reason = "preparation_eligible_fresh_admission_required"
	}
	return r, nil
}
