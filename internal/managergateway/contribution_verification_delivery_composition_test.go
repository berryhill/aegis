package managergateway

import (
	"context"
	"encoding/json"

	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	"github.com/berryhill/aegis/internal/runtime/hermes"
)

type assessmentFixtureAuthority struct{}

func (assessmentFixtureAuthority) Admit(context.Context, delivery.Intent, bool) error { return nil }

type assessmentFixtureAdapter struct{ git *delivery.Git }

func (a assessmentFixtureAdapter) Apply(ctx context.Context, _ delivery.Intent, p delivery.Plan, g delivery.Guard) delivery.Observation {
	return a.git.Commit(ctx, p.Commit, g)
}
func (a assessmentFixtureAdapter) Reconcile(ctx context.Context, _ delivery.Intent, p delivery.Plan, g delivery.Guard) delivery.Observation {
	if g(ctx) != nil {
		return delivery.Observation{Outcome: delivery.Denied}
	}
	return a.git.Reconcile(ctx, p.Commit)
}

// Actual Service/Queue/canonical Badger/native compiler and admitted local commit.
// Hermes protocol, reservation authority and quiescence gate are synthetic ONLY;
// no #377 native Wait, real provider, GitHub or final disposition is qualified.
func TestRetainedDeliveryNativeComposition(t *testing.T) {
	retainedDeliveryNativeComposition(t, false)
}

var finalQualificationFactory func(*testing.T, delivery.Plan) (delivery.Adapter, func() int)
var finalQualificationProtection func() string

func retainedDeliveryNativeComposition(t *testing.T, finalization bool, lateMode ...bool) {
	ctx := context.Background()
	root := t.TempDir()
	repoPath := filepath.Join(root, "source")
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	must(os.Mkdir(repoPath, 0700))
	files := map[string]string{"go.mod": "module synthetic\n\ngo 1.24\n", "main.go": "package main\nfunc main(){}\n", "value.go": "package main\nfunc Value() int{return 0}\n", "main_test.go": `package main
import "testing"
func TestBehavior(t *testing.T){if Value()!=42{t.Fatal("actual defect")}}
func TestSecurity(t *testing.T){if Value()<0{t.Fatal("negative")}}
func TestLaunch(t *testing.T){if Value()==-1{t.Fatal("launch")}}
func TestDenial(t *testing.T){if Value()==-2{t.Fatal("denial")}}
`, "README.md": "Fixture value contract\n", "SECURITY.md": "Fixture denial contract\n"}
	for p, b := range files {
		must(os.WriteFile(filepath.Join(repoPath, p), []byte(b), 0600))
	}
	git, _ := exec.LookPath("git")
	git, _ = filepath.EvalSymlinks(git)
	runGit := func(args ...string) string {
		t.Helper()
		b, e := exec.Command(git, append([]string{"-C", repoPath}, args...)...).CombinedOutput()
		if e != nil {
			t.Fatalf("git %v: %s %v", args, b, e)
		}
		return strings.TrimSpace(string(b))
	}
	runGit("init", "-b", "main")
	runGit("add", ".")
	runGit("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "base")
	base := runGit("rev-parse", "HEAD")
	runGit("remote", "add", "origin", "https://github.com/fixture/controlled.git")
	c := loop.ImplementationDraft("Repair real value behavior", "Native required tests pass")
	c.Workspace = filepath.Join(root, "owned")
	c.WritableFiles = []string{"value.go"}
	c.Policy.Packages = []string{"."}
	c.Policy.TimeoutSeconds = 60
	c.Policy.RequiredTests = nil
	for _, name := range []string{"TestBehavior", "TestSecurity", "TestLaunch", "TestDenial"} {
		c.Policy.RequiredTests = append(c.Policy.RequiredTests, loop.RequiredGoTest{Package: "synthetic", Name: name})
	}
	c.RepositoryContext = &loop.RepositoryContext{TaskDigest: contribution.Digest([]byte(c.Task)), MaxFiles: 8, MaxFileBytes: 4096, MaxTotalBytes: 16384}
	for _, p := range []string{"go.mod", "main.go", "value.go", "main_test.go"} {
		role := "source"
		if p == "go.mod" {
			role = "checker_input"
		}
		if p == "main_test.go" {
			role = "trusted_test"
		}
		c.RepositoryContext.Files = append(c.RepositoryContext.Files, loop.RepositoryContextFile{Path: p, Role: role, Bytes: int64(len(files[p])), Digest: contribution.Digest([]byte(files[p]))})
	}
	c.RepositoryContext.SourceDigest = implementation.RepositorySourceDigest(c.RepositoryContext)
	policy := loop.ContributionExecutionPolicy{Kind: "go-stdlib-behavior.v1", RepositoryPath: repoPath, GitBinary: git, BaseBranch: "main", BuildPackage: "."}
	for i, k := range []string{"behavior", "security", "launch", "denial"} {
		policy.Criteria = append(policy.Criteria, loop.ContributionCriterion{ID: k, Kind: k, Test: c.Policy.RequiredTests[i]})
	}
	for _, a := range []struct{ path, scope string }{{"README.md", "launch"}, {"SECURITY.md", "security"}} {
		d := contribution.Digest([]byte(files[a.path]))
		policy.ReviewAssets = append(policy.ReviewAssets, loop.ContributionReviewAsset{ID: a.path, Path: a.path, Scope: a.scope, BaselineDigest: d, ApprovedDigest: d})
	}
	s := contributionApp(t)
	clock := &contributionStageClock{at: time.Now().UTC()}
	s.Now = clock.Now
	subject, e := s.Authenticate(ctx)
	must(e)
	if strings.Contains(t.Name(), "ManagerJoined") {
		ctx, subject, e = s.AuthenticateContributionUnixPeerRequest(ctx, contributionDeliveryConfiguredUID(t, s))
		must(e)
	}
	repository, e := fleetbadger.Open(ctx, filepath.Join(s.Config.StateDir, "persistence", "fleet-v1"))
	must(e)
	defer repository.Close()
	refs := []reference.RevisionRef{}
	for _, id := range []string{"controller", "builder", "tester"} {
		charter := core.Charter{SchemaVersion: core.SchemaVersion, AgentID: id, Name: id, Revision: 1, Runtime: core.RuntimeConstraint{Adapter: "hermes", Runtime: "hermes-agent", VersionConstraint: ">=0.18.0", Target: "aegis-owned-ephemeral"}, CreatedBy: subject.PrincipalID, CreatedAt: s.Now(), Stanzas: []core.TrustStanza{{ID: "principal", Name: "principal", Enabled: true, Authentication: core.AuthenticationPolicy{Methods: []string{"local-os"}, Selectors: []core.IdentitySelector{{SubjectIDs: []string{subject.ID}, PrincipalIDs: []string{subject.PrincipalID}, Issuers: []string{"local-os"}, Environments: []string{"local"}}}}, Session: core.SessionPolicy{MaximumLifetimeSec: 300}, Approval: core.ApprovalPolicy{MaximumLifetimeSec: 300, SingleUse: true}, InformationFlow: core.InformationFlowPolicy{CrossStanza: "deny"}, Hermes: core.HermesConfig{Model: "synthetic-protocol", Provider: "none"}}}}
		if strings.Contains(t.Name(), "ManagerJoined") {
			charter.Stanzas[0].Authentication.Selectors[0].Issuers = []string{subject.Issuer}
		}
		cc, e := core.Canonicalize(charter)
		must(e)
		must(s.Store.SaveCharter(cc))
		must(s.Store.Save("receipts", id, core.Receipt{ID: id, CharterDigest: cc.Digest, Status: "verified"}))
		agent, e := registry.SealRevision(registry.AgentRevision{SchemaVersion: registry.AgentRevisionSchemaVersion, AgentID: id, Revision: 1, Source: registry.FleetSource{FleetID: "fleet", Kind: "current-fleet", SourceID: id}, Runtime: registry.RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent", Target: "aegis-owned-ephemeral"}, Ownership: registry.Ownership{OwnerID: subject.PrincipalID, AccountabilityID: "fixture"}, Lifecycle: registry.LifecycleEnabled, Charter: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: id, Revision: 1, Digest: cc.Digest}, CapabilityDeclarations: []string{}, PolicyRefs: []reference.DigestRef{}})
		must(e)
		ref := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: id, Revision: 1, Digest: agent.Digest}
		refs = append(refs, ref)
		_, e = repository.RegisterAgent(ctx, registry.AgentRegistration{SchemaVersion: registry.AgentRegistrationSchemaVersion, AgentID: id, Source: agent.Source, InitialRevision: ref}, agent, fleet.AuditFact{Event: core.AuditEvent{Type: "fixture.created", SubjectID: subject.ID, PrincipalID: subject.PrincipalID, Outcome: "succeeded", Reason: "isolated fixture"}})
		must(e)
	}
	scope, finalPolicy := "local-only", ""
	if finalization {
		scope, finalPolicy = "verified-protected-base-merge", evidence.ContributionFinalPolicy
		if strings.Contains(t.Name(), "ManagerJoined") || strings.Contains(t.Name(), "ManagerTerminal") {
			scope = "local-only"
		}
		if strings.Contains(t.Name(), "LocalTarget") {
			scope = "local-only"
		}
		if strings.Contains(t.Name(), "NoPublishTarget") {
			scope = "no-publish"
		}
		if strings.Contains(t.Name(), "NoMergeTarget") {
			scope = "no-merge"
		}
	}
	deadline := uint32(300)
	late := len(lateMode) > 0 && lateMode[0]
	if late {
		deadline = 600
	} // admitted fixture deadline, normal 300s mandate unchanged.
	lr, validation, e := loop.NewContributionRevision("assessment", 1, "", c, loop.ContributionProfile{FinalVerification: finalPolicy, Controller: refs[0], Builder: refs[1], Tester: refs[2], Repository: "fixture/controlled", RepositoryID: 7, Base: base, ReadAction: contribution.ReadIssueAction, Effect: scope, DeadlineSeconds: deadline, Invariants: []string{"preserve tests"}, Exclusions: []string{"no external network effects"}, Managed: &policy})
	if e != nil {
		t.Fatalf("loop: %v %+v", e, validation)
	}
	lrRef := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: lr.LoopID, Revision: 1, Digest: lr.Digest}
	proposal, _ := json.Marshal(map[string]any{"edits": []implementation.Edit{{Path: "value.go", Content: []byte("package main\nfunc Value() int{return 42}\n")}}})
	installation := filepath.Join(root, "install")
	must(os.MkdirAll(filepath.Join(installation, "venv", "bin"), 0700))
	executable := filepath.Join(root, "hermes")
	// The controlled process stays available across the admitted late fixture
	// interval; real mandate/session expiry and owned stop/Wait remain enforced.
	must(os.WriteFile(executable, []byte("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 'Hermes Agent v0.18.2'; echo 'Install directory: "+installation+"'; exit 0; fi\nexec sleep 600\n"), 0700))
	script := `#!/bin/sh
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready","payload":{}}}'
read tools
printf '%s\n' '{"jsonrpc":"2.0","id":"aegis-tools","result":{"total":0,"sections":[]}}'
read create || exit 0
sid="synthetic-$$"
printf '{"jsonrpc":"2.0","id":"create","result":{"session_id":"%s"}}\n' "$sid"
read prompt
printf '%s\n' '{"jsonrpc":"2.0","id":"prompt","result":{"accepted":true}}'
printf '{"jsonrpc":"2.0","method":"event","params":{"type":"message.start","session_id":"%s","payload":{}}}\n' "$sid"
text='` + nativeJSON(string(proposal)) + `'
printf '{"jsonrpc":"2.0","method":"event","params":{"type":"message.complete","session_id":"%s","payload":{"status":"complete","text":%s}}}\n' "$sid" "$text"
while read rest; do :; done
`
	must(os.WriteFile(filepath.Join(installation, "venv", "bin", "python"), []byte(script), 0700))
	adapter := hermes.New(executable, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.Hermes = adapter
	s.Config.HermesExecutable = executable
	routed, e := orchestration.NewRoutedRuntimeAdapter(adapter, s.Config.StateDir)
	must(e)
	fs, e := orchestration.NewFleetService(repository, s.Authority, s.AuthorityCommands, func(_ context.Context, _ orchestration.FleetAction, actor core.Subject) error {
		return s.RequirePrincipal(actor)
	}, nil, s.Now)
	must(e)
	verifier, e := evidence.NewBlobVerifier(s.Store)
	must(e)
	worker, e := orchestration.NewQueueWorker(repository, fs, s.Store, verifier, routed, s.Now)
	must(e)
	must(s.ConfigureFleet(repository, fs, worker))
	goBin, _ := exec.LookPath("go")
	goBin, _ = filepath.EvalSymlinks(goBin)
	s.Config.Implementation = config.Implementation{GoBinary: goBin, AuthorizedContracts: []string{lr.Digest}}
	must(worker.ConfigureImplementation(s.Config.Implementation, s.Config.StateDir, adapter))
	worker.SetContributionFetch(func(context.Context, loop.ContributionExecutionPolicy) error {
		runGit("fetch", "--no-tags", repoPath, "main")
		return nil
	})
	_, e = s.PublishLoopAs(ctx, subject, app.PublishLoopInput{AgentID: refs[0].ID, Revision: lr, IdempotencyKey: "publish"})
	must(e)
	_, e = s.SetLoopLifecycleAs(ctx, subject, lr.LoopID, app.SetLoopLifecycleInput{AgentID: refs[0].ID, Loop: lrRef, State: loop.LifecycleActive, EventID: "activate"})
	must(e)
	issue, _ := contribution.ParseIssueURL("https://github.com/fixture/controlled/issues/1")
	s.ContributionReader = &fixedContributionReader{source: contribution.Source{URL: issue, RepositoryID: 7, IssueID: 9, Author: contribution.Author{ID: 1, Login: "fixture"}, State: "open", CreatedAt: s.Now(), UpdatedAt: s.Now(), ObservedAt: s.Now(), ContentDigest: contribution.Digest([]byte("fixture")), Discussion: []contribution.Comment{}}}
	if strings.Contains(t.Name(), "ManagerJoined") {
		// This early-return journey bypasses the shared path's later cleanup.
		// Revoke actual fixture-owned roles while both repositories are open;
		// the earlier deferred repository.Close runs only after this defer.
		defer func() {
			authorities, err := s.Authority.ListAuthorityContexts(context.Background())
			if err != nil {
				t.Error("joined role cleanup discovery", err)
				return
			}
			for _, authority := range authorities {
				if err := s.RevokeSession(context.Background(), authority.SessionID, "joined fixture cleanup"); err != nil {
					t.Error("joined role cleanup", err)
				}
			}
		}()
		managerJoinedHeld(t, ctx, s, subject, lrRef, issue)
		return
	}
	intentKey := "assessment-native"
	if strings.Contains(t.Name(), "ManagerTerminal") {
		intentKey = app.ContributionIntentKey(lrRef, issue.URL, scope)
	}
	sealed, e := s.AdmitContributionAs(ctx, subject, "assessment-session", lrRef, issue.URL, intentKey, scope)
	must(e)
	result, e := s.ExecuteContributionAs(ctx, subject, sealed.Key)
	must(e)
	if result.Candidate == nil || result.Candidate.State != "candidate_accepted" {
		t.Fatalf("actual native execution: %+v", result)
	}
	v, e := s.AcceptedContributionCandidateAs(ctx, subject, sealed.Key)
	must(e)
	p := app.ContributionDeliveryPolicy{Binding: fleetbadger.ContributionDeliveryBinding{QueueItemID: sealed.QueueItemID, OwnerID: subject.PrincipalID, Key: sealed.Key, Reference: delivery.Reference{Run: sealed.RunID, Contract: sealed.Digest, Candidate: v.Digest}, FenceEpoch: 1, RoleBinding: contribution.Digest([]byte("qualification role")), Base: "main"}, Loop: sealed.Loop, Workspace: sealed.Worktree, Source: v.Source.Digest, Tree: v.CandidateTree, BaseHead: sealed.Base, CandidatePolicy: v.Policy, Toolchain: v.Toolchain, QueueAttempt: v.QueueAttempt, Actor: "fixture", Branch: "delivery/assessment", Timestamp: "2026-10-25T00:00:00Z", Message: "Reviewed local repair\n", Author: delivery.Identity{Name: "Reviewed Author", Email: "author@example.invalid"}, Committer: delivery.Identity{Name: "Controller", Email: "controller@example.invalid"}, AllowedFiles: []string{"value.go"}}
	policies := &nativeDeliveryPolicies{policy: p}
	authorities, e := s.Authority.ListAuthorityContexts(ctx)
	must(e)
	for _, a := range authorities {
		if a.AgentID == "controller" {
			p.ControllerSession = a.SessionID
		}
	}
	if p.ControllerSession == "" {
		t.Fatal("missing real controller session")
	}
	if finalization {
		_, state, _, stateErr := s.ObserveContributionContinuityAs(ctx, subject, sealed.Key)
		must(stateErr)
		controller, ok := state.RoleAuthorities["controller"]
		if !ok {
			t.Fatal("canonical finalization: real native execution did not supply controller RoleAuthority")
		}
		p.ControllerSession, p.Binding.FenceEpoch, p.Binding.RoleBinding = controller.SessionID, controller.Epoch, controller.Ref().Digest
		lastNative := result.Candidate.Passes[len(result.Candidate.Passes)-1]
		t.Logf("runtime-domain identity: builder_supervisor_equals_turn=%t tester_supervisor_equals_turn=%t", state.RoleAuthorities["builder"].RuntimeSessionID == lastNative.Candidate.Builder.RuntimeSessionID, state.RoleAuthorities["tester"].RuntimeSessionID == lastNative.Tester.RuntimeSessionID)
		if finalQualificationProtection == nil && scope != "local-only" && scope != "no-publish" {
			t.Fatal("run scripts/test-contribution-finalization.py for test-only HTTP overlay")
		}
		p.RequiredChecks, p.RequiredReviewers, p.PostMergeChecks = []string{"test"}, []string{"reviewer"}, []string{"test"}
		p.MergeMethod, p.PRTitle, p.PRBody = "squash", "Controlled accepted repair", "Controlled fixture without issue-closing keywords"
		p.ProtectionPolicy = contribution.Digest([]byte("local-only fixture without remote rights"))
		if finalQualificationProtection != nil {
			p.ProtectionPolicy = finalQualificationProtection()
		}
	}
	policies.policy = p
	t.Cleanup(func() {
		for _, a := range authorities {
			_ = s.RevokeSession(context.Background(), a.SessionID, "fixture cleanup")
		}
	})
	pref := reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: "reviewed", Digest: p.Digest()}
	if finalization {
		productive, err := app.NewContributionDeliverySource(s, subject, sealed.Key, pref, policies, nil)
		must(err)
		var admission delivery.Authority = assessmentFixtureAuthority{}
		var issueProbe *issueIntentFrameCompletion
		if strings.Contains(t.Name(), "IssueIntentAdmission") {
			issueProbe = &issueIntentFrameCompletion{managerFrameCompletion: &managerFrameCompletion{s: s, subject: subject, key: sealed.Key, policy: pref}, t: t}
			admission = issueProbe
		}
		owned, err := repository.ContributionDelivery(ctx, p.Binding, admission)
		must(err)
		var projected delivery.Plan
		must(productive.WithAccepted(ctx, p.Binding.Reference, func(_ context.Context, plan delivery.Plan) error { projected = plan; return nil }))
		if finalQualificationFactory == nil && scope != "local-only" && scope != "no-publish" {
			t.Fatal("missing test-only HTTP factory")
		}
		var remote delivery.Adapter
		countCalls := func() int { return 0 }
		if scope != "local-only" && scope != "no-publish" {
			remote, countCalls = finalQualificationFactory(t, projected)
		}
		pipeline := finalQualificationPipeline{local: &delivery.Git{Binary: git, Directory: sealed.Worktree}, remote: remote}
		verifier, err := app.NewContributionFinalVerifier(s, pref, policies)
		must(err)
		completion, err := app.NewContributionCompletionAdapter(verifier, subject, sealed.Key)
		must(err)
		var completionProbe *continuationCompletionProbe
		var completionOwner delivery.CompletionOwner = completion
		var frameOwner *managerFrameCompletion
		if strings.Contains(t.Name(), "ManagerTerminal") {
			frameOwner = &managerFrameCompletion{s: s, subject: subject, key: sealed.Key, policy: pref}
			completionOwner = frameOwner
		}
		if issueProbe != nil {
			completionOwner = issueProbe
		}
		if late {
			completionOwner = pauseFinalContribution{completion}
		}
		concurrent := strings.Contains(t.Name(), "Concurrent")
		var duplicate *app.ContributionCompletionAdapter
		if concurrent {
			duplicate, err = app.NewContributionCompletionAdapter(verifier, subject, sealed.Key)
			must(err)
			completionOwner = concurrentPreclosure{completion, duplicate}
		}
		conducted, finalErr := delivery.NewConductor(delivery.NewEngine(owned, productive, admission, pipeline), nil, completionOwner).Run(ctx, p.Binding.Reference)
		if late {
			// The owning Source intentionally sanitizes callback errors. Confirm
			// the exact post-issue held stage instead of relying on private error text.
			if finalErr == nil || conducted.Reason != "final_disposition_unavailable" {
				t.Fatalf("late checkpoint before terminal: %v", finalErr)
			}
			projection, e := repository.GetQueueProjection(ctx, sealed.QueueItemID)
			must(e)
			oldClaim, e := repository.GetClaim(ctx, projection.ActiveClaimID)
			must(e)
			oldBinding, e := repository.GetQueueRuntimeBinding(ctx, sealed.QueueItemID)
			must(e)
			_, before, _, e := s.ObserveContributionContinuityAs(ctx, subject, sealed.Key)
			must(e)
			// This is a warm owner: obtain the actual adapter's normal stop/Wait
			// acknowledgement before advancing time, not a hard-restart recovery
			// proof while the original supervisor is still owned by this process.
			_, e = s.StopContributionRoleAs(ctx, subject, sealed.Key, "controller", app.ContributionContinuityInput{ContractDigest: sealed.Digest, ExpectedSequence: before.Sequence, ExpectedDigest: before.Digest})
			must(e)
			clock.Advance(oldClaim.ExpiresAt.Sub(clock.Now()) + time.Second)
			if !clock.Now().Before(sealed.Deadline) {
				t.Fatal("fixture contract has no continuation interval")
			}
			fresh, e := s.Authenticate(ctx)
			must(e)
			if e = completion.FinalizeContributionCompletionAs(ctx, fresh, p.Binding.Reference); e == nil {
				t.Fatal("fresh observer without current role/continuation finalized")
			}
			foreign := fresh
			foreign.PrincipalID = "foreign"
			if e = completion.FinalizeContributionCompletionAs(ctx, foreign, p.Binding.Reference); e == nil {
				t.Fatal("foreign owner finalized")
			}
			_, e = s.ResumeContributionAs(ctx, fresh, sealed.Key)
			must(e)
			_, resumed, _, e := s.ObserveContributionContinuityAs(ctx, fresh, sealed.Key)
			must(e)
			resumedSession := resumed.RoleAuthorities["controller"].SessionID
			t.Cleanup(func() { _ = s.RevokeSession(context.Background(), resumedSession, "continued fixture cleanup") })
			completionProbe = &continuationCompletionProbe{Store: repository, t: t}
			s.FleetRepository = completionProbe
			if concurrent {
				var failedWriter bool
				finalErr, failedWriter = exerciseConcurrentFinalizers(t, s, fresh, sealed.Key, p.Binding.Reference, completion, duplicate, completionProbe)
				if failedWriter {
					return
				}
			} else {
				finalErr = completion.FinalizeContributionCompletionAs(ctx, fresh, p.Binding.Reference)
			}
			subject = fresh
			unchangedClaim, e := repository.GetClaim(ctx, oldClaim.ClaimID)
			must(e)
			unchangedBinding, e := repository.GetQueueRuntimeBinding(ctx, sealed.QueueItemID)
			must(e)
			first, _ := json.Marshal(oldBinding)
			second, _ := json.Marshal(unchangedBinding)
			if unchangedClaim != oldClaim || string(first) != string(second) {
				t.Fatal("continuation rewrote original execution")
			}
			_, after, _, e := s.ObserveContributionContinuityAs(ctx, fresh, sealed.Key)
			must(e)
			if before.Used != after.Used {
				t.Fatal("continuation reset/spent execution counters")
			}
		}
		terminal, resultErr := s.GetContributionResultAs(ctx, subject, sealed.Key)
		if finalErr != nil || resultErr != nil {
			_, diagnostic, diagnosisErr := verifier.VerifyPreClosureAs(ctx, subject, sealed.Key)
			history, historyErr := s.ReadContributionDeliveryHistoryAs(ctx, subject, sealed.Key)
			operations := []delivery.Operation{}
			for _, chain := range history.Histories {
				operations = append(operations, chain[len(chain)-1].Record.Intent.Operation)
			}
			outcomes := []string{}
			for _, effect := range conducted.Effects {
				outcomes = append(outcomes, string(effect.Intent.Operation)+":"+string(effect.Outcome)+":"+effect.Code)
			}
			t.Fatalf("canonical finalization FIRST unresolved producer join: conductor=%v reason=%s held=%s effects=%v indexed_result=%v disposition=%s code=%s diagnostic=%v history=%v operations=%v", finalErr, conducted.Reason, conducted.Held, outcomes, resultErr, terminal.Disposition.DispositionID, diagnostic.Code, diagnosisErr, historyErr, operations)
		}
		expectedState, expectedOutcome := "succeeded", "succeeded"
		if scope != "verified-protected-base-merge" {
			expectedOutcome = "narrower-target-completed"
		}
		if strings.Contains(t.Name(), "FailedChecks") {
			expectedState, expectedOutcome = "failed", "failed"
		}
		if string(terminal.Disposition.State) != expectedState || terminal.Report.Outcome != expectedOutcome || terminal.Report.Scope != scope {
			t.Fatal("canonical finalization did not produce SUCCEEDED")
		}
		if expectedOutcome != "succeeded" {
			h, e := s.ReadContributionDeliveryHistoryAs(ctx, subject, sealed.Key)
			must(e)
			for _, chain := range h.Histories {
				op := chain[0].Record.Intent.Operation
				if op == delivery.Merge || op == delivery.ReconcileIssue || ((scope == "local-only" || scope == "no-publish") && op != delivery.Commit) {
					t.Fatal("forbidden effect in bounded result", op)
				}
			}
		}
		if late && (completionProbe == nil || !completionProbe.genericDenied || terminal.Report.Current == nil) {
			t.Fatal("dedicated current completion not proven")
		}
		beforeReads := countCalls()
		fresh, freshErr := s.Authenticate(ctx)
		must(freshErr)
		again, freshErr := s.GetContributionResultAs(ctx, fresh, sealed.Key)
		must(freshErr)
		first, _ := json.Marshal(terminal)
		second, _ := json.Marshal(again)
		if string(first) != string(second) {
			t.Fatal("retained result changed")
		}
		if countCalls() != beforeReads {
			t.Fatal("result retrieval repeated remote effects")
		}
		t.Logf("canonical result disposition=%s artifact=%s receipt=%s", terminal.Disposition.DispositionID, terminal.Artifact.ID, terminal.Receipts[0].ID)
		if strings.Contains(t.Name(), "ManagerTerminal") {
			if frameOwner.actual == nil {
				t.Fatal("actual in-frame composition helper not called")
			}
			if frameOwner.actual.FinalizeContributionBoundary(ctx, p.Binding.Reference) == nil {
				t.Fatal("in-frame helper authority escaped source lifetime")
			}
			managerTerminalReadback(t, s, subject, sealed, clock)
		}
		return
	}
	source, e := app.NewQualificationContributionDeliverySource(s, subject, sealed.Key, pref, policies, func(context.Context, orchestration.ContributionCandidate) error { return nil })
	must(e)
	authority := assessmentFixtureAuthority{}
	owner, e := repository.ContributionDelivery(ctx, p.Binding, authority)
	must(e)
	record, e := delivery.NewEngine(owner, source, authority, assessmentFixtureAdapter{&delivery.Git{Binary: git, Directory: sealed.Worktree}}).Execute(ctx, p.Binding.Reference, delivery.Commit)
	must(e)
	if record.Outcome != delivery.Confirmed {
		t.Fatalf("reserved native commit: %+v", record)
	}
	assessed, e := s.AssessContributionRetainedDeliveryAs(ctx, subject, sealed.Key, pref, policies)
	must(e)
	if assessed.Status != "retained-delivery-evidence-assessed" || assessed.Code != "continuity_verification_required" || assessed.NativeRef == "" {
		t.Fatalf("positive actual composition: %+v", assessed)
	}
	_, e = s.Store.GetBlob(assessed.NativeRef)
	must(e)
	projection, e := repository.GetQueueProjection(ctx, sealed.QueueItemID)
	must(e)
	if projection.State != queue.StateClaimed || projection.Attempts != 1 {
		t.Fatal("assessment changed Queue disposition")
	}
	for _, kind := range []string{"author", "committer", "message", "timestamp", "paths", "source", "tree", "toolchain", "actor", "candidate", "role"} {
		t.Run(kind, func(t *testing.T) {
			bad := p
			switch kind {
			case "author":
				bad.Author.Name = "Other"
			case "committer":
				bad.Committer.Name = "Other"
			case "message":
				bad.Message = "Other"
			case "timestamp":
				bad.Timestamp = "2026-10-24T00:00:00Z"
			case "paths":
				bad.AllowedFiles = []string{"main.go"}
			case "source":
				bad.Source = contribution.Digest([]byte("other"))
			case "tree":
				bad.Tree = strings.Repeat("a", 40)
			case "toolchain":
				bad.Toolchain = contribution.Digest([]byte("other"))
			case "actor":
				bad.Actor = "other"
			case "candidate":
				bad.Binding.Candidate = contribution.Digest([]byte("other"))
			case "role":
				bad.Binding.RoleBinding = contribution.Digest([]byte("other"))
			}
			policies.policy = bad
			badref := pref
			badref.Digest = bad.Digest()
			got, e := s.AssessContributionRetainedDeliveryAs(ctx, subject, sealed.Key, badref, policies)
			if e == nil && got.Status == "retained-delivery-evidence-assessed" {
				t.Fatal("substitution accepted")
			}
		})
	}
	policies.policy = p
	for _, kind := range []string{"stale", "foreign"} {
		bad := subject
		if kind == "stale" {
			bad.ExpiresAt = s.Now().Add(-time.Second)
		} else {
			bad.PrincipalID = "foreign"
		}
		if _, e := s.AssessContributionRetainedDeliveryAs(ctx, bad, sealed.Key, pref, policies); e == nil {
			t.Fatal(kind)
		}
	}
	must(s.StopContributionInSessionAs(ctx, subject, "assessment-stop", sealed.Key))
	if _, e := s.AssessContributionRetainedDeliveryAs(ctx, subject, sealed.Key, pref, policies); e == nil {
		t.Fatal("stop accepted")
	}
}
