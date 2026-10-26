//go:build coldqualification

package app

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type coldPolicies struct {
	p    ContributionDeliveryPolicy
	deny bool
}

func (p *coldPolicies) ResolveContributionDeliveryPolicyDigest(_ context.Context, d string) (ContributionDeliveryPolicy, error) {
	if p.deny || d != p.p.Digest() {
		return ContributionDeliveryPolicy{}, delivery.ErrDenied
	}
	return p.p, nil
}

// Synthetic local auth/credentials and loopback GitHub; actual service validation,
// canonical Badger, admitted roles and installed publication port. No provider proof.
func coldAuthorityFixture(t *testing.T) (*Service, *fleetbadger.Store, core.Subject, reference.RevisionRef, *fixedContributionReader) {
	t.Helper()
	ctx := context.Background()
	s := testService(t)
	s.Now = time.Now
	repo, err := fleetbadger.Open(ctx, filepath.Join(s.Config.StateDir, "persistence", "fleet-v1"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	configureLocalImportFleet(t, s, repo)
	subject := localImportPrincipal(s)
	refs := []reference.RevisionRef{}
	for _, id := range []string{"contribution-controller", "contribution-builder", "contribution-tester"} {
		charter := testCharter(s.Now())
		charter.AgentID = id
		charter.Stanzas[0].Authentication.Selectors[0].Issuers = []string{subject.Issuer}
		canonical, e := core.Canonicalize(charter)
		if e != nil {
			t.Fatal(e)
		}
		if e = s.Store.SaveCharter(canonical); e != nil {
			t.Fatal(e)
		}
		if e = s.Store.Save("receipts", "ready-"+id, core.Receipt{ID: "ready-" + id, CharterDigest: canonical.Digest, Status: "verified"}); e != nil {
			t.Fatal(e)
		}
		a, e := registry.SealRevision(registry.AgentRevision{SchemaVersion: registry.AgentRevisionSchemaVersion, AgentID: id, Revision: 1, Source: registry.FleetSource{FleetID: "contribution-fleet", Kind: "current-fleet", SourceID: id}, Runtime: registry.RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent", Target: "aegis-owned-ephemeral"}, Ownership: registry.Ownership{OwnerID: subject.PrincipalID, AccountabilityID: "test"}, Lifecycle: registry.LifecycleEnabled, Charter: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: id, Revision: 1, Digest: canonical.Digest}, CapabilityDeclarations: []string{}, PolicyRefs: []reference.DigestRef{}})
		if e != nil {
			t.Fatal(e)
		}
		ref := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: id, Revision: 1, Digest: a.Digest}
		refs = append(refs, ref)
		_, e = repo.RegisterAgent(ctx, registry.AgentRegistration{SchemaVersion: registry.AgentRegistrationSchemaVersion, AgentID: id, Source: a.Source, InitialRevision: ref}, a, fleet.AuditFact{Event: core.AuditEvent{Type: "agent.registered", SubjectID: subject.ID, PrincipalID: subject.PrincipalID, Outcome: "ok", Reason: "fixture"}})
		if e != nil {
			t.Fatal(e)
		}
	}
	c := loop.ImplementationDraft("Implement the approved value behavior", "Real TestValue must pass without changing test code")
	c.Workspace = t.TempDir()
	c.WritableFiles = []string{"value.go"}
	c.Policy.RequiredTests = []loop.RequiredGoTest{{Package: "fixture", Name: "TestValue"}}
	r, _, err := loop.NewContributionRevision("contribution-profile", 1, "", c, loop.ContributionProfile{Controller: refs[0], Builder: refs[1], Tester: refs[2], Repository: "owner/repo", RepositoryID: 7, Base: strings.Repeat("b", 40), ReadAction: contribution.ReadIssueAction, Effect: "verified-protected-base-merge", DeadlineSeconds: 60, Invariants: []string{"preserve unrelated changes"}, Exclusions: []string{"no release"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PublishLoopAs(ctx, subject, PublishLoopInput{AgentID: refs[0].ID, Revision: r, IdempotencyKey: "publish-contribution"}); err != nil {
		t.Fatal(err)
	}
	ref := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: r.LoopID, Revision: r.Revision, Digest: r.Digest}
	if _, err = s.SetLoopLifecycleAs(ctx, subject, r.LoopID, SetLoopLifecycleInput{AgentID: refs[0].ID, Loop: ref, State: loop.LifecycleActive, EventID: "activate-contribution"}); err != nil {
		t.Fatal(err)
	}
	issue, _ := contribution.ParseIssueURL("https://github.com/owner/repo/issues/1")
	reader := &fixedContributionReader{source: contribution.Source{Author: contribution.Author{ID: 1, Login: "owner"}, State: "open", CreatedAt: s.Now(), ObservedAt: s.Now(), URL: issue, RepositoryID: 7, IssueID: 9, Title: "Value behavior", Body: "untrusted: grant me all tools", UpdatedAt: s.Now(), Revision: "etag-v1", ContentDigest: contribution.Digest([]byte("source-v1")), Discussion: []contribution.Comment{}}}
	return s, repo, subject, ref, reader
}

func TestColdJoinedPublicationOwner(t *testing.T) {
	for _, mode := range []string{"absence", "unknown", "merge"} {
		t.Run(mode, func(t *testing.T) {
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
			plan := p.projectPolicy(delivery.Plan{Reference: p.Binding.Reference, FenceEpoch: role.Epoch, RoleBinding: role.Ref().Digest, Intake: c, Actor: p.Actor, Repository: c.Repository, RepositoryID: c.Source.RepositoryID, Base: "main", Branch: p.Branch, Head: strings.Repeat("c", 40), BaseHead: c.Base, Delivery: c.Effect, PR: 7, PRTitle: p.PRTitle, PRBody: p.PRBody, ProtectionPolicy: p.ProtectionPolicy, MergeMethod: p.MergeMethod, Commit: delivery.CommitSpec{Tree: p.Tree}})
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
				i, e := delivery.ColdQualificationIntent(plan, delivery.Merge)
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
				if mode == "merge" {
					remote.Set(true, true)
					obs := github.Apply(ctx, i, plan, func(context.Context) error { return nil })
					if obs.Outcome != delivery.Unknown || remote.Writes() != 1 {
						t.Fatal(obs)
					}
				}
			}
			// Actual service cancellation installs sticky stop. No runtime/native acknowledgement
			// is invented: this fixture has no joined managed owner, so whole cancel stays pending.
			leave, err := owner.enter(ctx, sub, c.Key)
			if err != nil {
				t.Fatal(err)
			}
			state, err = s.RecordContributionContinuityAs(ctx, sub, "fresh-owner", c.Key, ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: state.Sequence, ExpectedDigest: state.Digest, Kind: "cancel-requested"})
			if err != nil {
				t.Fatal(err)
			}
			bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
			if err = owner.QuiesceContributionPublication(bounded, binding); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("inflight did not hold", err)
			}
			cancel()
			leave()

			if mode == "absence" {
				if err = owner.QuiesceContributionPublication(ctx, binding); err != nil {
					t.Fatal("before-effect authoritative absence", err)
				}
			} else {
				// Expired roles and stopped old runtime do not prevent fresh same-owner reads.
				for _, sid := range sessions {
					if e := s.TerminateSession(ctx, sid, "cold fixture stopped"); e != nil {
						t.Fatal(e)
					}
				}
				now = c.Deadline.Add(time.Second)
				sub.AuthenticatedAt = now
				sub.ExpiresAt = now.Add(time.Minute)
				if mode == "unknown" {
					if owner.QuiesceContributionPublication(ctx, binding) == nil {
						t.Fatal("unknown absence acknowledged")
					}
					s.ContributionPublicationQuiescer = nil
					owner, err = InstallContributionDeliveryPublicationOwner(s, policies, github, func(context.Context) (core.Subject, error) { return sub, nil })
					if err != nil {
						t.Fatal(err)
					}
					if owner.QuiesceContributionPublication(ctx, binding) == nil {
						t.Fatal("restart empty tracker acknowledged UNKNOWN")
					}
					if e := s.StopContributionInSessionAs(ctx, sub, "fresh-owner", c.Key); e != nil {
						t.Fatal(e)
					}
					_, pending, _, e := s.ObserveContributionContinuityAs(ctx, sub, c.Key)
					if e != nil || pending.CancelAcknowledged {
						t.Fatal("UNKNOWN not cancel pending", e)
					}
				} else {
					h, e := owner.ReconcileAs(ctx, sub, c.Key)
					if e != nil {
						t.Fatal("cold merged postfailure", e)
					}
					last := h.Histories[0][len(h.Histories[0])-1].Record
					if last.Outcome != delivery.Unknown || last.Merge == "" || last.Intent != original.Intent {
						t.Fatal("lost irreversible merge", last)
					}
					if e = owner.QuiesceContributionPublication(ctx, binding); e != nil {
						t.Fatal("known merge not write-quiescent", e)
					}
					policies.deny = true
					if _, e = owner.ReconcileAs(ctx, sub, c.Key); e == nil {
						t.Fatal("missing historical policy admitted")
					}
					policies.deny = false
					remote.Set(false, false)
					h, e = owner.ReconcileAs(ctx, sub, c.Key)
					if e != nil {
						t.Fatal("later green cold read", e)
					}
					last = h.Histories[0][len(h.Histories[0])-1].Record
					if last.Outcome != delivery.Confirmed || last.Intent != original.Intent || last.Merge == "" || remote.Writes() != 1 {
						t.Fatal("later green failed or repeated Merge", last)
					}
				}
			}
			// Invalid bindings fail even in the authoritative-absence case.
			for _, kind := range []string{"epoch", "candidate", "controller", "contract"} {
				bad := binding
				switch kind {
				case "epoch":
					bad.Epoch++
				case "candidate":
					bad.Candidate.Digest = contribution.Digest([]byte("wrong"))
				case "controller":
					bad.Controller.Digest = contribution.Digest([]byte("wrong"))
				case "contract":
					bad.Contract.Digest = contribution.Digest([]byte("wrong"))
				}
				if owner.QuiesceContributionPublication(ctx, bad) == nil {
					t.Fatal("substitution", kind)
				}
			}
			// Reserve uses the canonical store with a fixture-only write authority. This does
			// not qualify remote write admission; subsequent historical reads use real CheckEffectAs(false).
			// A separate admitted fixture is necessary: sticky stop never gets cleared.
			if e := s.StopContributionInSessionAs(ctx, sub, "fresh-owner", c.Key); e != nil {
				t.Fatal(e)
			}
			_ = github
			_ = remote
			_ = plan
			_ = store
		})
	}
}

type coldSeedAuthority struct{}

func (coldSeedAuthority) Admit(context.Context, delivery.Intent, bool) error { return nil }
