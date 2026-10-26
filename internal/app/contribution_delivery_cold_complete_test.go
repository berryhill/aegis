//go:build coldqualification

package app

import (
	"context"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/delivery"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"github.com/berryhill/aegis/internal/reference"
	"strings"
	"testing"
	"time"
)

func TestColdIsolationComplete(t *testing.T) {
	for _, stage := range []string{"live-prestop", "post-stop-expiry"} {
		t.Run(stage, func(t *testing.T) {
			mode := "unknown"
			ctx := context.Background()
			s, store, sub, ref, reader := coldAuthorityFixture(t)
			now := s.Now()
			s.Now = func() time.Time { return now }
			sub.ExpiresAt = now.Add(30 * time.Second)
			c, err := s.admitContributionAs(ctx, sub, "original", ref, reader.source.URL.URL, "cold-joined", "verified-protected-base-merge", reader)
			if err != nil {
				t.Fatal(err)
			}
			state, err := s.RecordContributionContinuityAs(ctx, sub, "original", c.Key, ContributionContinuityInput{ContractDigest: c.Digest, Kind: "initialize"})
			if err != nil {
				t.Fatal(err)
			}
			input := func() ContributionContinuityInput {
				return ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: state.Sequence, ExpectedDigest: state.Digest}
			}
			sessions := map[string]string{}
			for _, role := range []string{"controller", "builder", "tester"} {
				binding, _ := contribution.SealedRole(c, role)
				m, _, e := s.PreviewSessionAs(ctx, sub, binding.Charter.ID, binding.Charter.Revision, "principal", core.Environment{Name: "local"})
				if e != nil {
					t.Fatal(role, " preview ", e)
				}
				session, e := s.StartSessionAs(ctx, sub, m.ID)
				if e != nil {
					t.Fatal(role, " start ", e)
				}
				sid := session.ID
				t.Cleanup(func() { _ = s.TerminateSession(ctx, sid, "cleanup") })
				sessions[role] = sid
				authority, e := s.authorityContextForSession(ctx, sid)
				if e != nil {
					t.Fatal(e)
				}
				admitted, e := s.AuthorityCommands.AuthorityAdmission(ctx, authority.ID, authority.Digest, s.Now())
				if e != nil || !admitted.Admitted {
					t.Fatal("authority", e)
				}
				if role == "controller" {
					_, _, e := s.BindQueueRuntimeAs(ctx, sub, BindQueueRuntimeInput{AgentID: c.Controller.ID, Authority: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: authority.ID, Digest: authority.Digest}, QueueItemID: c.QueueItemID, BindingID: "root-binding", TransitionID: "root-bound"})
					if e != nil {
						t.Fatal("bind", e)
					}
				}
				state, e = s.AttachContributionObservationAs(ctx, sub, sid, c.Key, input())
				if e != nil {
					t.Fatal(role, " attach ", e)
				}
				state, e = s.AdmitContributionRoleAs(ctx, sub, sid, c.Key, role, input())
				if e != nil {
					t.Fatal(role, " admit ", e)
				}
				got, e := s.CheckContributionRoleAs(ctx, sub, sid, c.Key, role, input())
				if e != nil || got.Epoch != 1 {
					t.Fatal(role, " check ", e)
				}
			}

			checkpoint := input()
			checkpoint.Kind = "checkpoint"
			checkpoint.Stage = "review"
			checkpoint.Candidate = "candidate-one"
			checkpoint.Evidence = reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: "candidate-evidence", Digest: contribution.Digest([]byte("cold candidate"))}
			state, err = s.RecordContributionContinuityAs(ctx, sub, "original", c.Key, checkpoint)
			if err != nil {
				t.Fatal(err)
			}
			role := state.RoleAuthorities["controller"]
			p := ContributionDeliveryPolicy{ControllerSession: role.SessionID, Binding: fleetbadger.ContributionDeliveryBinding{QueueItemID: c.QueueItemID, OwnerID: sub.PrincipalID, Key: c.Key, Reference: delivery.Reference{Run: c.RunID, Contract: c.Digest, Candidate: state.CandidateEvidence.Digest}, FenceEpoch: role.Epoch, RoleBinding: role.Ref().Digest, Base: "main"}, Actor: "actor", Branch: "contribution/run", Tree: strings.Repeat("d", 40), BaseHead: c.Base, RequiredChecks: []string{"test"}, PostMergeChecks: []string{"test"}, RequiredReviewers: []string{"reviewer"}, MergeMethod: "squash", PR: 7, PRTitle: "Scoped contribution", PRBody: "Bounded accepted change", ProtectionPolicy: delivery.ColdQualificationProtection()}
			encrypted, bindings, pin := coldEncryptedAuthority(t)
			s.CredentialAuthority = encrypted
			p.CredentialPin = pin
			if mode == "legacy" {
				p.CredentialPin = nil
			}
			if strings.HasPrefix(mode, "publish") {
				p.CredentialPin.Operations = []delivery.Operation{delivery.Publish}
				p.OriginalCommit = &delivery.CommitSpec{Parent: c.Base, Tree: p.Tree, Commit: strings.Repeat("c", 40), Branch: p.Branch, Author: delivery.Identity{Name: "Fixture", Email: "fixture@example.invalid"}, Committer: delivery.Identity{Name: "Fixture", Email: "fixture@example.invalid"}, Timestamp: "2026-10-25T00:00:00Z", Message: "Exact original publication\n", Files: []delivery.File{{Path: "value.go", Mode: "100644", Bytes: []byte("reviewed")}}}
			}
			p.Loop = c.Loop
			if e := s.retainContributionDeliveryPolicy(ctx, p); e != nil {
				t.Fatal(e)
			}
			plan := p.projectPolicy(delivery.Plan{Reference: p.Binding.Reference, FenceEpoch: role.Epoch, RoleBinding: role.Ref().Digest, Intake: c, Actor: p.Actor, Repository: c.Repository, RepositoryID: c.Source.RepositoryID, Base: "main", Branch: p.Branch, Head: strings.Repeat("c", 40), BaseHead: c.Base, Delivery: c.Effect, PR: 7, PRTitle: p.PRTitle, PRBody: p.PRBody, ProtectionPolicy: p.ProtectionPolicy, MergeMethod: p.MergeMethod, Commit: delivery.CommitSpec{Tree: p.Tree}})
			if p.OriginalCommit != nil {
				plan.Commit = *p.OriginalCommit
			}
			if mode == "publish-legacy" {
				p.OriginalCommit = nil
				if e := s.retainContributionDeliveryPolicy(ctx, p); e != nil {
					t.Fatal(e)
				}
			}
			policies := &coldPolicies{p: p}
			plan.Policy = p.Digest()
			github, remote := delivery.ColdQualificationGitHub(t, plan)
			owner, err := InstallContributionDeliveryPublicationOwner(s, policies, github, func(context.Context) (core.Subject, error) { return sub, nil })
			if err != nil {
				t.Fatal(err)
			}
			if s.ContributionPublicationQuiescer != owner {
				t.Fatal("actual port not assigned")
			}
			binding := ContributionPublicationBinding{Contract: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: c.QueueItemID, Digest: c.Digest}, RunID: c.RunID, QueueItemID: c.QueueItemID, Candidate: state.CandidateEvidence, Controller: role.Ref(), Epoch: role.Epoch}

			var original delivery.Record
			if mode != "absence" {
				op := delivery.Merge
				if strings.HasPrefix(mode, "publish") {
					op = delivery.Publish
				}
				i, e := delivery.ColdQualificationIntent(plan, op)
				if e != nil {
					t.Fatal(e)
				}
				repo, e := store.ContributionDelivery(ctx, p.Binding, coldSeedAuthority{})
				if e != nil {
					t.Fatal(e)
				}
				original, _, e = repo.Reserve(ctx, i)
				if e != nil {
					t.Fatal("seed durable UNKNOWN", e)
				}

			}
			history, _ := NewContributionDeliveryPolicyHistory(s)
			custody, e := NewContributionDeliveryColdCustody(s, history, encrypted, func(context.Context) (core.Subject, error) { return sub, nil })
			if e != nil {
				t.Fatal(e)
			}
			remote.ExistingMerged()
			delivery.ColdQualificationSetCustody(github, custody)
			if custody.Use(ctx, original.Intent, func([]byte) error { t.Fatal("direct secret callback"); return nil }) == nil {
				t.Fatal("direct use admitted")
			}

			_ = binding
			if stage == "post-stop-expiry" {
				stop := input()
				stop.Kind = "cancel-requested"
				state, err = s.RecordContributionContinuityAs(ctx, sub, "fresh-owner", c.Key, stop)
				if err != nil {
					t.Fatal(err)
				}
				for _, sid := range sessions {
					if err = s.TerminateSession(ctx, sid, "cold isolation stopped"); err != nil {
						t.Fatal(err)
					}
				}
				now = c.Deadline.Add(time.Second)
				sub.AuthenticatedAt = now
				sub.ExpiresAt = now.Add(time.Minute)
			}
			guard := func(ctx context.Context) error {
				return (contributionColdAuthority{s, sub, c.Key, original.Intent}).Admit(ctx, original.Intent, false)
			}
			attack := &coldCompleteAttack{custody: custody, github: github, plan: plan, t: t, guard: guard, bindings: bindings, remote: remote}
			delivery.ColdQualificationSetCustody(github, attack)
			h, err := owner.ReconcileAs(ctx, sub, c.Key)
			if err != nil {
				t.Fatalf("outer legitimate read must succeed: %v", err)
			}
			if !attack.called || attack.positives < 2 || remote.Reads() == 0 || remote.Writes() != 0 {
				t.Fatalf("callback/read/write controls: %+v reads=%d writes=%d", attack, remote.Reads(), remote.Writes())
			}
			if len(h.Histories) != 1 || len(h.Histories[0]) != 2 {
				t.Fatalf("canonical append: %+v", h)
			}
			first, last := h.Histories[0][0].Record, h.Histories[0][1].Record
			if first != original || first.Outcome != delivery.Unknown || last.Outcome != delivery.Confirmed || last.Intent != original.Intent || last.Head != plan.Head || last.Merge != strings.Repeat("e", 40) {
				t.Fatalf("legitimate remote result not retained: %+v", last)
			}
			attack.deny(attack.outer, original.Intent)
			done := make(chan error, 1)
			go func() {
				done <- custody.Use(attack.outer, original.Intent, func([]byte) error { t.Error("async callback escaped"); return nil })
			}()
			if <-done == nil {
				t.Fatal("async escaped")
			}
			t.Logf("stage=%s UNKNOWN revision=%d -> CONFIRMED revision=%d head=%s merge=%s GETs=%d mutations=%d encrypted positives=%d", stage, first.Revision, last.Revision, last.Head, last.Merge, remote.Reads(), remote.Writes(), attack.positives)

		})
	}
}

// All authority comes from the installed owner's genuine outer reconciliation.
// Logical mode fixtures below confer no productive authority.
type coldCompleteAttack struct {
	remote       *delivery.ColdQualificationRemote
	calls        int
	custody      *ContributionDeliveryColdCustody
	github       *delivery.GitHub
	plan         delivery.Plan
	t            *testing.T
	guard        delivery.Guard
	bindings     *coldMemoryCredentials
	called       bool
	positives    int
	depth        int
	outer, inner context.Context
	nestedDenied bool
	probing      bool
}

func (a *coldCompleteAttack) deny(ctx context.Context, i delivery.Intent) {
	a.t.Helper()
	if a.custody.Use(ctx, i, func([]byte) error { a.t.Fatal("negative custody callback"); return nil }) == nil {
		a.t.Fatal("negative custody admitted")
	}
}
func (a *coldCompleteAttack) positive(ctx context.Context, i delivery.Intent) {
	a.t.Helper()
	if err := a.custody.Use(ctx, i, func(b []byte) error {
		if len(b) == 0 {
			a.t.Fatal("empty encrypted secret")
		}
		a.positives++
		return nil
	}); err != nil {
		a.t.Fatalf("actual encrypted positive: %v", err)
	}
}
func (a *coldCompleteAttack) Use(ctx context.Context, i delivery.Intent, fn func([]byte) error) error {
	a.calls++
	if a.depth > 0 {
		a.inner = ctx
		if a.probing {
			err := a.custody.Use(ctx, i, func([]byte) error { a.t.Fatal("laundered custody callback"); return nil })
			if err == nil {
				a.t.Fatal("laundered custody admitted")
			}
			a.nestedDenied = true
			return err
		}
		return a.custody.Use(ctx, i, fn)
	}
	if !a.called {
		a.called = true
		a.outer = ctx
		a.positive(ctx, i) // FIRST: enabled exact current version/binding, before attack.
		a.bindings.r.Binding.BindingRevision++
		a.deny(ctx, i)
		a.bindings.r.Binding.BindingRevision--
		a.bindings.r.Binding.Enabled = false
		a.deny(ctx, i)
		a.bindings.r.Binding.Enabled = true
		a.bindings.r.Version.Version++
		a.deny(ctx, i)
		a.bindings.r.Version.Version--
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		a.deny(canceled, i)
		for _, field := range []string{"actor", "policy", "candidate", "role"} {
			other := i
			switch field {
			case "actor":
				other.Actor = "foreign"
			case "policy":
				other.Policy = "foreign"
			case "candidate":
				other.Candidate = "foreign"
			case "role":
				other.RoleBinding = "foreign"
			}
			a.deny(ctx, other)
		}
		calls := a.calls
		reads := a.remote.Reads()
		o := a.github.Apply(ctx, i, a.plan, a.guard)
		if a.calls != calls || a.remote.Reads() != reads {
			a.t.Fatal("nested Apply reached custody/HTTP")
		}
		if o.Outcome != delivery.Denied || o.Code != "adapter_mode_denied" {
			a.t.Fatalf("nested Apply boundary: %+v", o)
		}
		a.depth++
		a.probing = true
		reads = a.remote.Reads()
		// Real nested Reconcile must not launder a write-mode ancestor into cold custody.
		// Retain the SAME authentic permit, but no read-mode ancestor. Actual
		// Apply creates its own write mode; its guard probes nested Reconcile
		// then ALWAYS denies, never granting synthetic productive admission.
		permitContext := context.WithValue(context.Background(), contributionHistoricalPermitKey{}, ctx.Value(contributionHistoricalPermitKey{}))
		applyGuardCalled := false
		o = a.github.Apply(permitContext, i, a.plan, func(write context.Context) error {
			applyGuardCalled = true
			nested := a.github.Reconcile(write, i, a.plan, a.guard)
			if !a.nestedDenied || nested.Outcome == delivery.Confirmed {
				a.t.Fatal("real Apply/Reconcile laundering")
			}
			a.nestedDenied = false
			return delivery.ErrDenied
		})
		if !applyGuardCalled || o.Outcome != delivery.Denied {
			a.t.Fatal("real Apply guard fixture not exercised")
		}

		other := i
		other.Actor = "different-ancestor"
		delivery.ColdQualificationNestedMode(ctx, other, true, func(foreign context.Context) {
			o := a.github.Reconcile(foreign, i, a.plan, a.guard)
			if !a.nestedDenied || o.Outcome == delivery.Confirmed {
				a.t.Fatal("different ancestor laundering")
			}
			a.nestedDenied = false
		})
		// Matching genuine nested read succeeds, then its captured context must die
		// while the authentic outer historical permit and outer read are STILL live.
		if a.remote.Reads() != reads {
			a.t.Fatal("negative ancestor reached HTTP")
		}
		a.probing = false
		o = a.github.Reconcile(ctx, i, a.plan, a.guard)
		if o.Outcome != delivery.Confirmed {
			a.t.Fatalf("nested read positive: %+v", o)
		}
		a.depth--
		if a.inner == nil {
			a.t.Fatal("missing nested capture")
		}
		a.deny(a.inner, i)
		permit, _ := ctx.Value(contributionHistoricalPermitKey{}).(*contributionHistoricalPermit)
		if permit == nil || !permit.active.Load() {
			a.t.Fatal("outer historical permit not live")
		}
		a.positive(ctx, i)
	}
	return a.custody.Use(ctx, i, fn)
}
