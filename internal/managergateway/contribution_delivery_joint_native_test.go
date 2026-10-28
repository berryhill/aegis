package managergateway

import (
	"context"
	"encoding/json"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/orchestration"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"github.com/berryhill/aegis/internal/reference"
	"testing"
)

// Uses the actual managed Queue/native Wait fixture; protocol transport remains
// synthetic. Reached tier: local Commit authorization and canonical reservation,
// not remote publication, provider proof, or cancellation publication quiescence.
func contributionDeliveryJointNative(t *testing.T, ctx context.Context, s *app.Service, subject core.Subject, key, controllerSession string, v orchestration.ContributionCandidate) {
	t.Helper()
	c, _, err := s.ReadContributionAs(ctx, subject, key)
	if err != nil {
		t.Fatal(err)
	}
	ref := delivery.Reference{Run: c.RunID, Contract: c.Digest, Candidate: v.Digest}
	p := app.ContributionDeliveryPolicy{
		ControllerSession: controllerSession,
		Binding:           fleetbadger.ContributionDeliveryBinding{QueueItemID: c.QueueItemID, OwnerID: subject.PrincipalID, Key: key, Reference: ref, FenceEpoch: 1, RoleBinding: contribution.Digest([]byte("synthetic exact role binding")), Base: "main"},
		Loop:              c.Loop, Workspace: c.Worktree, Source: v.Source.Digest, Tree: v.CandidateTree, BaseHead: c.Base, CandidatePolicy: v.Policy, Toolchain: v.Toolchain, QueueAttempt: v.QueueAttempt,
		Actor: "fixture", Branch: "delivery/reviewed", Timestamp: "2026-10-25T00:00:00Z", Message: "Implement reviewed behavior\n",
		Author: delivery.Identity{Name: "Reviewed Author", Email: "author@example.invalid"}, Committer: delivery.Identity{Name: "Reviewed Controller", Email: "controller@example.invalid"}, AllowedFiles: []string{"value.go", "offset.go"},
		ProtectionPolicy: contribution.Digest([]byte("synthetic protection")), RequiredChecks: []string{"native"}, RequiredReviewers: []string{"reviewer"}, PostMergeChecks: []string{"postmerge"}, MergeMethod: "squash", PRTitle: "Reviewed fixture", PRBody: "Controlled local fixture", PR: 1,
	}
	resolver := &nativeDeliveryPolicies{policy: p}
	policyRef := func(p app.ContributionDeliveryPolicy) reference.DigestRef {
		return reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: "reviewed-delivery", Digest: p.Digest()}
	}

	_, state, _, err := s.ObserveContributionContinuityAs(ctx, subject, key)
	if err != nil {
		t.Fatal(err)
	}
	role := state.RoleAuthorities["controller"]
	if role.SessionID != controllerSession {
		t.Fatal("controller pin drift")
	}
	for _, name := range []string{"builder", "tester"} {
		if !contribution.NativeRoleQuiescent(state, name) {
			t.Fatal("native Wait missing", name)
		}
	}
	p.Binding.FenceEpoch = role.Epoch
	p.Binding.RoleBinding = role.Ref().Digest
	resolver.policy = p
	source, err := app.NewContributionDeliverySource(s, subject, key, policyRef(p), resolver, nil)
	if err != nil {
		t.Fatal(err)
	}
	request := contribution.EffectAdmissionRequest{Contract: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: c.QueueItemID, Digest: c.Digest}, RunID: c.RunID, QueueItemID: c.QueueItemID, Candidate: v.Digest, CandidateEvidence: state.CandidateEvidence, Role: "controller", Epoch: role.Epoch, RoleAuthority: role.Ref(), Scope: c.Effect}
	var escaped context.Context
	var authority *app.ContributionDeliveryContinuityAuthority
	var intent delivery.Intent
	err = source.WithAccepted(ctx, ref, func(held context.Context, plan delivery.Plan) error {
		escaped = held
		if plan.Commit.Tree != v.CandidateTree || v.QueueAttempt != p.QueueAttempt || v.Source.Digest != p.Source {
			t.Fatal("frozen source drift")
		}
		intent = delivery.Intent{FenceEpoch: plan.FenceEpoch, RoleBinding: plan.RoleBinding, Reference: plan.Reference, Operation: delivery.Commit, Actor: plan.Actor, Repository: plan.Repository, RepositoryID: plan.RepositoryID, Base: plan.Base, Branch: plan.Branch, Head: plan.Head, BaseHead: plan.BaseHead, ExpectedOld: plan.ExpectedOld, Policy: plan.Policy}
		wire, _ := json.Marshal(struct {
			Run, Contract, Repository, Base, Branch string
			RepositoryID                            uint64
			Operation                               delivery.Operation
		}{intent.Run, intent.Contract, intent.Repository, intent.Base, intent.Branch, intent.RepositoryID, intent.Operation})
		intent.ID = contribution.Digest(wire)
		intent.Payload = contribution.Digest([]byte("exact local authorization fixture"))
		var e error
		authority, e = app.NewContributionDeliveryContinuityAuthority(held, s, subject, key, controllerSession, request, intent)
		if e != nil {
			return e
		}
		if e = authority.Admit(ctx, intent, true); e == nil {
			t.Fatal("outside source admitted")
		}
		if e = authority.Admit(held, intent, true); e != nil {
			return e
		}
		for _, kind := range []string{"epoch", "role", "candidate", "operation", "target"} {
			bad := intent
			switch kind {
			case "epoch":
				bad.FenceEpoch++
			case "role":
				bad.RoleBinding = contribution.Digest([]byte("wrong"))
			case "candidate":
				bad.Candidate = contribution.Digest([]byte("wrong"))
			case "operation":
				bad.Operation = delivery.Publish
			case "target":
				bad.Repository = "other/repo"
			}
			if authority.Admit(held, bad, true) == nil {
				t.Fatal("substitution", kind)
			}
		}
		wrong := request
		wrong.Role = "builder"
		if _, e = app.NewContributionDeliveryContinuityAuthority(held, s, subject, key, controllerSession, wrong, intent); e == nil {
			t.Fatal("wrong current role")
		}
		owner, e := s.FleetRepository.(*fleetbadger.Store).ContributionDelivery(held, p.Binding, authority)
		if e != nil {
			return e
		}
		record, fresh, e := owner.Reserve(held, intent)
		if e != nil {
			return e
		}
		if !fresh || record.Intent != intent {
			t.Fatal("canonical permission reservation absent")
		}
		return owner.Fence(held, intent, true)
	})
	if err != nil {
		t.Fatal("joint native/source/authority reservation", err)
	}
	if escaped == nil || authority == nil {
		t.Fatal("callback absent")
	}
	if authority.Admit(escaped, intent, true) == nil {
		t.Fatal("escaped source admitted")
	}
	if authority.Admit(ctx, intent, true) == nil {
		t.Fatal("outside source admitted")
	}
	t.Log("actual native Wait + Source private context + real authority + canonical Store Reserve/Fence; local authorization only")
}
