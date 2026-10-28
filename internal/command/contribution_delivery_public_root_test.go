package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/loop"
	authoritybadger "github.com/berryhill/aegis/internal/persistence/authority/badger"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/principalauth"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	"github.com/berryhill/aegis/internal/runtime/hermes"
	"github.com/spf13/cobra"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func nativeJSON(s string) string { b, _ := json.Marshal(s); return string(b) }

type publicRootContributionReader struct {
	source contribution.Source
	calls  int
}

func (r *publicRootContributionReader) Read(_ context.Context, _ contribution.IssueURL) (contribution.Source, error) {
	r.calls++
	return r.source, nil
}
func TestPublicRootAuthenticatedContributionJourney(t *testing.T) {
	publicRootAuthenticatedContributionJourney(t, false)
}

func TestPublicRootAuthenticatedContributionFinalJourney(t *testing.T) {
	publicRootAuthenticatedContributionJourney(t, true)
}

func TestPublicRootAuthenticatedContributionFinalDisconnectResumeJourney(t *testing.T) {
	publicRootAuthenticatedContributionJourney(t, true, true)
}

func TestPublicRootContributionDeclaredOperationApprovalHeld(t *testing.T) {
	publicRootAuthenticatedContributionJourney(t, false, false, false, false, false, true)
}

func publicRootAuthenticatedContributionJourney(t *testing.T, final bool, disconnected ...bool) {
	approvalHeld := len(disconnected) > 4 && disconnected[4]
	started := time.Now()
	journey := true
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
	files := map[string]string{"go.mod": "module synthetic\n\ngo 1.24\n", "main.go": "package main\nimport (\"fmt\";\"os\")\nfunc main(){if len(os.Args)>1{os.Exit(2)};fmt.Print(Value())}\n", "value.go": "package main\nfunc Value() int{return 0}\n", "offset.go": "package main\nfunc Offset() int{return 0}\n", "main_test.go": `package main
import("testing";"os";"os/exec")
func TestBehavior(t *testing.T){if Value()+Offset()!=42{t.Fatal("multi-file defect")}}
func TestSecurity(t *testing.T){if Offset()<0{t.Fatal("negative")}}
func TestDenial(t *testing.T){p:=os.Getenv("AEGIS_CONTRIBUTION_CANDIDATE");if p==""{return};e:=exec.Command(p,"forbidden").Run();x,ok:=e.(*exec.ExitError);if !ok||x.ExitCode()!=2{t.Fatalf("required argument denial: %v",e)}}
func TestLaunch(t *testing.T){p:=os.Getenv("AEGIS_CONTRIBUTION_CANDIDATE");if p==""{return};b,e:=exec.Command(p).Output();if e!=nil||string(b)!="40"{t.Fatalf("candidate executable %s %v",b,e)}}
`}
	files["README.md"] = "Controlled fixture prints 40; arguments are rejected with exit 2.\n"
	files["SECURITY.md"] = "No credentials or network; unexpected arguments are denied.\n"
	files["generated.txt"] = "fixture output contract: 40\n"
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
	runGit("-c", "user.name=Xander", "-c", "user.email=xander@agents.tarxan.io", "commit", "-m", "base")
	base := runGit("rev-parse", "HEAD")
	runGit("remote", "add", "origin", "https://github.com/fixture/controlled.git")
	c := loop.ImplementationDraft("Repair multi-file behavior", "Behavior, security and built executable pass")
	c.Workspace = filepath.Join(root, "owned")
	c.WritableFiles = []string{"value.go", "offset.go"}
	c.Policy.Packages = []string{"."}
	c.Policy.TimeoutSeconds = 60
	c.Policy.RequiredTests = []loop.RequiredGoTest{{Package: "synthetic", Name: "TestBehavior"}, {Package: "synthetic", Name: "TestSecurity"}, {Package: "synthetic", Name: "TestLaunch"}, {Package: "synthetic", Name: "TestDenial"}}
	c.RepositoryContext = &loop.RepositoryContext{TaskDigest: contribution.Digest([]byte(c.Task)), MaxFiles: 8, MaxFileBytes: 4096, MaxTotalBytes: 16384}
	for _, p := range []string{"go.mod", "main.go", "value.go", "offset.go", "main_test.go"} {
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
	for _, asset := range []struct{ path, scope string }{{"README.md", "launch"}, {"SECURITY.md", "security"}, {"generated.txt", "launch"}, {"main.go", "security"}, {"main_test.go", "security"}} {
		d := contribution.Digest([]byte(files[asset.path]))
		policy.ReviewAssets = append(policy.ReviewAssets, loop.ContributionReviewAsset{ID: asset.path, Path: asset.path, Scope: asset.scope, BaselineDigest: d, ApprovedDigest: d})
	}
	cfg := config.Defaults().WithStateDir(filepath.Join(root, "state"))
	cfg.Principal.ID = "principal-1"
	cfg.Principal.Name = "Fixture"
	cfg.Principal.UID = fmt.Sprint(os.Getuid())
	cfg.Principal.User = os.Getenv("USER")
	port := os.Getenv("AEGIS_ACCEPTANCE_PORT")
	if port == "" {
		port = "0"
	} // Portable CI; workstation acceptance uses the shared helper.
	cfg.API.Listen = "127.0.0.1:" + port
	cfg.API.Console.Origin = "http://" + cfg.API.Listen
	cfg.API.UnixSocket = filepath.Join(root, "api.sock")
	cfg.API.Token = "isolated-contribution-transport-token"
	cfg.API.WriteTimeout = 3 * time.Minute // HTTP operation budget, not authentication lifetime.
	cfg.ContributionDelivery = &config.ContributionDelivery{Convention: "contribution-publication.v1", Actor: "fixture", Author: config.ContributionGitIdentity{Name: "Author", Email: "author@example.invalid"}, Committer: config.ContributionGitIdentity{Name: "Controller", Email: "controller@example.invalid"}, ProtectionPolicy: "exact-protection", RequiredChecks: []string{"native"}, RequiredReviewers: []string{"independent"}, PostMergeChecks: []string{"postmerge"}, MergeMethod: "squash"}
	_, e := authoritybadger.Initialize(ctx, filepath.Join(cfg.StateDir, "persistence", "authority-v1"))
	must(e)
	configPath := filepath.Join(root, "aegis.json")
	raw, e := json.Marshal(cfg)
	must(e)
	must(os.WriteFile(configPath, raw, 0600))
	var build func(*cobra.Command) (*app.Service, error)
	var closeService func() error
	cmdRoot := newRoot(Dependencies{Out: io.Discard, Err: io.Discard, Logger: slog.New(slog.NewTextHandler(os.Stderr, nil))}, func(b func(*cobra.Command) (*app.Service, error), c func() error) { build = b; closeService = c })
	t.Cleanup(func() { must(closeService()) })
	must(cmdRoot.PersistentFlags().Set("config", configPath))
	fixtureCommand := &cobra.Command{Use: "serve"}
	cmdRoot.AddCommand(fixtureCommand)
	fixtureCommand.SetContext(ctx)
	s, e := build(fixtureCommand)
	must(e)
	if _, e = s.CurrentContributionDeliverySubject(ctx); e == nil {
		t.Fatal("startup context granted authority")
	}
	liveClock := s.Now
	var consoleClock atomic.Int64
	s.Now = func() time.Time {
		if sample := consoleClock.Load(); sample != 0 {
			return time.Unix(0, sample).UTC()
		}
		return liveClock()
	}
	verifier, e := principalauth.Enroll(cfg.Principal.ID, []byte("fixture-password"))
	must(e)
	var subject core.Subject
	repository := s.FleetRepository
	lifetime := int64(300)
	ctx, nativeSubject, e := s.AuthenticateContributionUnixPeerRequest(ctx, uint32(os.Getuid()))
	must(e)
	refs := []reference.RevisionRef{}
	for _, id := range []string{"controller", "builder", "tester"} {
		charter := core.Charter{SchemaVersion: core.SchemaVersion, AgentID: id, Name: id, Revision: 1, Runtime: core.RuntimeConstraint{Adapter: "hermes", Runtime: "hermes-agent", VersionConstraint: ">=0.18.0", Target: "aegis-owned-ephemeral"}, CreatedBy: cfg.Principal.ID, CreatedAt: s.Now(), Stanzas: []core.TrustStanza{{ID: "principal", Name: "principal", Enabled: true, Authentication: core.AuthenticationPolicy{Methods: []string{"local-os"}, Selectors: []core.IdentitySelector{{SubjectIDs: []string{nativeSubject.ID}, PrincipalIDs: []string{cfg.Principal.ID}, Issuers: []string{nativeSubject.Issuer}, Environments: []string{"local"}}}}, Session: core.SessionPolicy{MaximumLifetimeSec: lifetime}, Approval: core.ApprovalPolicy{MaximumLifetimeSec: 300, SingleUse: true}, InformationFlow: core.InformationFlowPolicy{CrossStanza: "deny"}, Hermes: core.HermesConfig{Model: "synthetic-protocol", Provider: "none"}}}}
		if approvalHeld && id == "controller" {
			charter.Stanzas[0].Approval.RequiredOperations = []string{string(delivery.Commit)}
		}
		cc, err := core.Canonicalize(charter)
		must(err)
		must(s.Store.SaveCharter(cc))
		// Same verified-provisioning fixture as app session lifecycle tests; isolated stores only.
		must(s.Store.Save("receipts", id, core.Receipt{ID: id, CharterDigest: cc.Digest, Status: "verified"}))
		agent, err := registry.SealRevision(registry.AgentRevision{SchemaVersion: registry.AgentRevisionSchemaVersion, AgentID: id, Revision: 1, Source: registry.FleetSource{FleetID: "fleet", Kind: "current-fleet", SourceID: id}, Runtime: registry.RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent", Target: "aegis-owned-ephemeral"}, Ownership: registry.Ownership{OwnerID: cfg.Principal.ID, AccountabilityID: "fixture"}, Lifecycle: registry.LifecycleEnabled, Charter: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: id, Revision: 1, Digest: cc.Digest}, CapabilityDeclarations: []string{}, PolicyRefs: []reference.DigestRef{}})
		must(err)
		ref := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: id, Revision: 1, Digest: agent.Digest}
		refs = append(refs, ref)
		_, err = repository.RegisterAgent(ctx, registry.AgentRegistration{SchemaVersion: registry.AgentRegistrationSchemaVersion, AgentID: id, Source: agent.Source, InitialRevision: ref}, agent, fleet.AuditFact{Event: core.AuditEvent{Type: "fixture.created", SubjectID: subject.ID, PrincipalID: cfg.Principal.ID, Outcome: "succeeded", Reason: "isolated fixture"}})
		must(err)
	}
	subject = nativeSubject
	nonlocalCancellation := len(disconnected) > 3 && disconnected[3]
	effect := "local-only"
	if nonlocalCancellation {
		effect = "verified-protected-base-merge"
	}
	finalPolicy := ""
	if final {
		finalPolicy = evidence.ContributionFinalPolicy
	}
	lr, lv, e := loop.NewContributionRevision("managed", 1, "", c, loop.ContributionProfile{FinalVerification: finalPolicy, Controller: refs[0], Builder: refs[1], Tester: refs[2], Repository: "fixture/controlled", RepositoryID: 7, Base: base, ReadAction: contribution.ReadIssueAction, Effect: effect, DeadlineSeconds: 300, Invariants: []string{"preserve trusted tests"}, Exclusions: []string{"no GitHub effects"}, Managed: &policy})
	if e != nil {
		t.Fatalf("revision: %v %+v", e, lv.Issues)
	}
	lrRef := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: lr.LoopID, Revision: 1, Digest: lr.Digest}
	message := func(n int) string {
		p, _ := json.Marshal(map[string]any{"edits": []implementation.Edit{{Path: "value.go", Content: []byte("package main\nfunc Value() int{return 40}\n")}, {Path: "offset.go", Content: []byte(fmt.Sprintf("package main\nfunc Offset() int{return %d}\n", n))}}})
		return string(p)
	}
	// Only the independent cancellation target skips the deliberately wrong proposal.
	// Native baseline, trusted checks and the two-candidate ceiling remain real.
	initialOffset := 2
	if nonlocalCancellation {
		initialOffset = 2
	}
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready","payload":{}}}'
read tools
printf '%%s\n' '{"jsonrpc":"2.0","id":"aegis-tools","result":{"total":0,"sections":[]}}'
read create || exit 0
n=0; [ ! -f '%s' ] || n=$(cat '%s'); n=$((n+1)); printf '%%s' "$n" > '%s'
sid="synthetic-runtime-$n"
printf '{"jsonrpc":"2.0","id":"create","result":{"session_id":"%%s"}}\n' "$sid"
read prompt
printf '%%s\n' '{"jsonrpc":"2.0","id":"prompt","result":{"accepted":true}}'
printf '{"jsonrpc":"2.0","method":"event","params":{"type":"message.start","session_id":"%%s","payload":{}}}\n' "$sid"
case "$n" in 1) text='%s';; 3) text='%s';; *) text='"synthetic inspection only"';; esac
printf '{"jsonrpc":"2.0","method":"event","params":{"type":"message.complete","session_id":"%%s","payload":{"status":"complete","text":%%s}}}\n' "$sid" "$text"
while read rest; do :; done
`, filepath.Join(root, "turns"), filepath.Join(root, "turns"), filepath.Join(root, "turns"), nativeJSON(message(initialOffset)), nativeJSON(message(2)))
	if len(disconnected) != 0 && disconnected[0] {
		script = strings.Replace(script, "read prompt", "if [ \"$n\" = 1 ]; then touch '"+filepath.Join(root, "builder-ready")+"'; while [ ! -f '"+filepath.Join(root, "builder-release")+"' ]; do sleep 0.01; done; fi\nread prompt", 1)
	}
	installation := filepath.Join(root, "install")
	must(os.MkdirAll(filepath.Join(installation, "venv", "bin"), 0700))
	executable := filepath.Join(root, "hermes")
	must(os.WriteFile(executable, []byte(`#!/bin/sh
if [ "$1" = "--version" ]; then echo 'Hermes Agent v0.18.2'; echo 'Install directory: `+installation+`'; exit 0; fi
exec sleep 240
`), 0700))
	must(os.WriteFile(filepath.Join(installation, "venv", "bin", "python"), []byte(script), 0700))
	adapter := hermes.NewWithClock(executable, slog.New(slog.NewTextHandler(io.Discard, nil)), s.Now)
	s.Hermes = adapter
	s.Config.HermesExecutable = executable
	worker := s.QueueWorker
	goBin, _ := exec.LookPath("go")
	goBin, _ = filepath.EvalSymlinks(goBin)
	s.Config.Implementation = config.Implementation{GoBinary: goBin, AuthorizedContracts: []string{lr.Digest}}
	must(worker.ConfigureImplementation(s.Config.Implementation, s.Config.StateDir, adapter))
	worker.SetContributionFetch(func(context.Context, loop.ContributionExecutionPolicy) error {
		runGit("fetch", "--no-tags", repoPath, "main")
		return nil
	})
	_, err := s.PublishLoopAs(ctx, subject, app.PublishLoopInput{AgentID: refs[0].ID, Revision: lr, IdempotencyKey: "publish"})
	must(err)
	_, err = s.SetLoopLifecycleAs(ctx, subject, lr.LoopID, app.SetLoopLifecycleInput{AgentID: refs[0].ID, Loop: lrRef, State: loop.LifecycleActive, EventID: "activate"})
	must(err)
	issue, _ := contribution.ParseIssueURL("https://github.com/fixture/controlled/issues/1")
	reader := &publicRootContributionReader{source: contribution.Source{URL: issue, RepositoryID: 7, IssueID: 9, Author: contribution.Author{ID: 1, Login: "fixture"}, State: "open", CreatedAt: s.Now(), UpdatedAt: s.Now(), ObservedAt: s.Now(), ContentDigest: contribution.Digest([]byte("fixture")), Discussion: []contribution.Comment{}}}
	s.ContributionReader = reader
	must(principalauth.Publish(filepath.Join(cfg.StateDir, "auth", principalauth.FileName), verifier))
	// Freeze only the Console login sample, after native adapters captured the
	// live clock. Native authority/worker checks must share real elapsed time.
	consoleClock.Store(liveClock().UnixNano())
	gateway := startContributionHTTPFixture(t, s)
	assertContributionHTTPDenials(t, gateway)
	assertLivePasswordNativeDenial(t, gateway, s)
	consoleClock.Store(0)
	t.Logf("setup complete elapsed=%s", time.Since(started))
	opened, err := gateway.Open(ctx, subject)
	must(err)
	id, token := opened.ID, opened.Token
	_, err = gateway.Execute(ctx, subject, id, token, "/contribute select "+lrRef.ID)
	must(err)
	runCtx, disconnect := context.WithCancel(ctx)
	defer disconnect()
	if len(disconnected) != 0 && disconnected[0] {
		go func() {
			deadline := time.NewTimer(90 * time.Second)
			defer deadline.Stop()
			tick := time.NewTicker(10 * time.Millisecond)
			defer tick.Stop()
			for {
				select {
				case <-deadline.C:
					return
				case <-tick.C:
					if _, e := os.Stat(filepath.Join(root, "builder-ready")); e == nil {
						disconnect()
						_ = os.WriteFile(filepath.Join(root, "builder-release"), []byte("release"), 0600)
						return
					}
				}
			}
		}()
	}
	// Snapshot the source repository before the FIRST automatic manager effect.
	// The owned worktree does not exist yet; its original HEAD/index must match
	// this sampled protected base, not a post-publication baseline.
	privatePath := filepath.Join(repoPath, "private-canary.txt")
	privateBytes := []byte("private source canary: never publish or alter\n")
	must(os.WriteFile(privatePath, privateBytes, 0600))
	preHead, preIndex := runGit("rev-parse", "HEAD"), runGit("write-tree")
	preStatus := runGit("status", "--porcelain=v1", "--untracked-files=all")
	diagnostic := time.AfterFunc(90*time.Second, func() {
		stack := make([]byte, 1<<20)
		n := runtime.Stack(stack, true)
		t.Logf("native execution still active after 90s; elapsed=%s\n%s", time.Since(started), stack[:n])
	})
	defer diagnostic.Stop()
	response, err := gateway.Execute(runCtx, subject, id, token, "/contribute run "+issue.URL)
	if len(disconnected) != 0 && disconnected[0] {
		if !errors.Is(err, context.Canceled) || runCtx.Err() != context.Canceled {
			t.Fatalf("aborted Unix HTTP transport: want context.Canceled, got %v", err)
		}
		key := app.ContributionIntentKey(lrRef, issue.URL, "")
		// Real metadata requests are paced below the unchanged production limiter.
		// Neither observation nor client disconnect may mint another intent/attempt.
		waitCtx, stopWait := context.WithTimeout(ctx, 120*time.Second)
		defer stopWait()
		// Real status calls authenticate and verify canonical history. Keep
		// observation from monopolizing the native runner's 2-CPU race budget.
		tick := time.NewTicker(5 * time.Second)
		defer tick.Stop()
		for ready := false; !ready; {
			select {
			case <-waitCtx.Done():
				t.Fatal("same-intent native execution did not reach accepted/quiescent boundary after HTTP disconnect")
			case <-tick.C:
				status, e := gateway.Execute(waitCtx, subject, id, token, "/contribute status "+key)
				must(e)
				wire, e := json.Marshal(status.Data["execution"])
				must(e)
				var execution app.ContributionExecutionResult
				must(json.Unmarshal(wire, &execution))
				if execution.Candidate == nil || execution.Candidate.State != "candidate_accepted" {
					continue
				}
				_, continuity, _, e := s.ObserveContributionContinuityAs(ctx, subject, key)
				must(e)
				ready = contribution.NativeRoleQuiescent(continuity, "builder") && contribution.NativeRoleQuiescent(continuity, "tester")
				if ready {
					t.Logf("HTTP disconnect retained SAME key=%s accepted candidate=%s counters=%+v; native builder/tester quiescent", key, execution.Candidate.Passes[0].Candidate.Digest, continuity.Used)
				}
			}
		}
		beforeResume, e := s.ReadContributionExecutionAs(ctx, subject, key)
		must(e)
		beforeControl, e := s.ReadContributionControlAs(ctx, subject, key)
		must(e)
		_, beforeContinuity, _, e := s.ObserveContributionContinuityAs(ctx, subject, key)
		must(e)
		beforeAuthorities, e := s.Authority.ListAuthorityContexts(ctx)
		must(e)
		// Fresh HTTP request, original authenticated manager session and key/TTL.
		response, err = gateway.Execute(ctx, subject, id, token, "/contribute resume "+key)
		if err != nil {
			t.Fatalf("same-intent authenticated Unix HTTP resume after accepted/quiescent disconnect: %v state=%s reason=%s", err, response.State, response.Reason)
		}
		afterResume, e := s.ReadContributionExecutionAs(ctx, subject, key)
		must(e)
		afterControl, e := s.ReadContributionControlAs(ctx, subject, key)
		must(e)
		_, afterContinuity, _, e := s.ObserveContributionContinuityAs(ctx, subject, key)
		must(e)
		afterAuthorities, e := s.Authority.ListAuthorityContexts(ctx)
		must(e)
		if !reflect.DeepEqual(beforeResume, afterResume) || !reflect.DeepEqual(beforeControl, afterControl) {
			t.Fatal("resume repeated native implementation/Queue attempt or changed control")
		}
		if !reflect.DeepEqual(beforeAuthorities, afterAuthorities) || !reflect.DeepEqual(beforeContinuity.RoleAuthorities, afterContinuity.RoleAuthorities) || !reflect.DeepEqual(beforeContinuity.RoleEpochs, afterContinuity.RoleEpochs) || beforeContinuity.Used != afterContinuity.Used {
			t.Fatal("same-service delivery reattachment minted authority or reset role epochs/budgets")
		}
	}
	if err != nil {
		if sessions, readErr := s.ListSessions(); readErr == nil {
			for _, session := range sessions {
				t.Logf("fixture session agent=%s status=%s end_reason=%s", session.Mandate.AgentID, session.Status, session.EndReason)
			}
		}
		t.Fatalf("manager run: %v state=%s reason=%s data=%+v", err, response.State, response.Reason, response.Data)
	}
	diagnostic.Stop()
	t.Logf("native execution complete elapsed=%s", time.Since(started))
	data := response.Data
	if data["completed"] != final {
		t.Fatal("candidate falsely completed")
	}
	if !journey && (response.State != "unavailable" || response.Reason != "delivery_controller_unavailable") {
		t.Fatalf("candidate-only not held: %+v", response)
	}
	key := app.ContributionIntentKey(lrRef, issue.URL, "")
	sealed, state, _, err := s.ObserveContributionContinuityAs(ctx, subject, key)
	must(err)
	// Finalization closes the Queue claim. Read the retained accepted pass;
	// do not request a fresh effect/handoff admission after terminal closure.
	record, err := s.ReadContributionExecutionAs(ctx, subject, key)
	must(err)
	if record.State != "candidate_accepted" || record.Consumed != 1 || len(record.Passes) != 1 || record.Baseline.Outcome != "failed" || record.Passes[0].Check.Outcome != "passed" {
		t.Fatal("real native RED/GREEN missing")
	}
	candidate := record.Passes[0].Candidate
	if !final {
		admitted, e := s.AcceptedContributionCandidateAs(ctx, subject, key)
		must(e)
		if !reflect.DeepEqual(candidate, admitted) {
			t.Fatal("accepted candidate differs from retained pass")
		}
	}
	for _, role := range []string{"builder", "tester"} {
		if !contribution.NativeRoleQuiescent(state, role) {
			t.Fatal("native Wait absent", role)
		}
	}
	gitOwned := func(args ...string) string {
		t.Helper()
		b, e := exec.Command(git, append([]string{"-C", sealed.Worktree}, args...)...).CombinedOutput()
		if e != nil {
			t.Fatalf("owned git %v: %s %v", args, b, e)
		}
		return strings.TrimSpace(string(b))
	}
	if runGit("rev-parse", "HEAD") != preHead || runGit("write-tree") != preIndex || runGit("status", "--porcelain=v1", "--untracked-files=all") != preStatus {
		t.Fatal("first automatic delivery changed original source HEAD/index/status")
	}
	actualPrivate, readErr := os.ReadFile(privatePath)
	must(readErr)
	if string(actualPrivate) != string(privateBytes) {
		t.Fatal("first automatic delivery changed original private bytes")
	}
	if gitOwned("rev-parse", "HEAD") != preHead || gitOwned("write-tree") != preIndex {
		t.Fatal("first automatic delivery changed owned worktree original HEAD/index")
	}
	if approvalHeld {
		if response.State != "unavailable" || response.Reason != "delivery_approval_unavailable" || data["completed"] == true {
			t.Fatalf("declared operation not held: %+v", response)
		}
		// Empty publication has no HistorySnapshot root. Use the canonical
		// all-family absence oracle after a real authenticated sticky stop.
		_, e := gateway.Execute(ctx, subject, id, token, "/contribute stop "+key)
		must(e)
		absence := s.FleetRepository.(interface {
			ContributionDeliveryAuthoritativeAbsence(context.Context, string, string) (bool, error)
		})
		absent, e := absence.ContributionDeliveryAuthoritativeAbsence(ctx, subject.PrincipalID, key)
		must(e)
		if !absent {
			t.Fatal("approval denial retained publication intent/effect")
		}
		if gitOwned("for-each-ref", "--format=%(refname)", "refs/heads/contribution/") != "" {
			t.Fatal("approval denial authored local ref")
		}
		if _, e := s.GetFinalContributionResultAs(ctx, subject, key); e == nil {
			t.Fatal("approval denial minted final success")
		}
		if state.Used.Candidates > 2 {
			t.Fatal("approval denial reset/exceeded budget")
		}
		t.Log("actual HTTP native RED/GREEN retained; declared Commit approval unavailable before reservation/effect; private source preserved")
		return
	}
	head := gitOwned("rev-parse", "HEAD")
	index := gitOwned("write-tree")
	status := gitOwned("status", "--porcelain=v1", "--untracked-files=all")
	var terminal app.ContributionResult
	assertTerminal := func(data map[string]any, expected app.ContributionResult) {
		t.Helper()
		if data["completed"] != true || data["terminal"] != true || data["boundary_completed"] != true || data["whole_contribution_succeeded"] != false || data["outcome"] != "narrower-target-completed" {
			t.Fatalf("local terminal truth: %+v", data)
		}
		for name, want := range map[string]any{"result": expected, "disposition": expected.Disposition, "report": expected.Report, "artifact": expected.Artifact, "receipts": expected.Receipts} {
			wire, e := json.Marshal(data[name])
			must(e)
			decoded := reflect.New(reflect.TypeOf(want))
			must(json.Unmarshal(wire, decoded.Interface()))
			if !reflect.DeepEqual(decoded.Elem().Interface(), want) {
				t.Fatalf("terminal %s differs from canonical owner readback", name)
			}
		}
	}
	var result delivery.DeliveryResult
	if final {
		terminal, err = s.GetFinalContributionResultAs(ctx, subject, key)
		must(err)
		assertTerminal(data, terminal)
		if terminal.Report.Validate() != nil || terminal.Report.Key != key || terminal.Report.Run != sealed.RunID || terminal.Report.Contract != sealed.Digest || terminal.Report.Scope != "local-only" || terminal.Report.Candidate != candidate.Digest || len(terminal.Receipts) != 1 || terminal.Receipts[0].Outcome != evidence.Passed || terminal.Receipts[0].ArtifactID != terminal.Artifact.ID || terminal.Receipts[0].ObservedDigest != terminal.Artifact.Digest || len(terminal.Disposition.ArtifactIDs) != 1 || terminal.Disposition.ArtifactIDs[0] != terminal.Artifact.ID || len(terminal.Disposition.ReceiptIDs) != 1 || terminal.Disposition.ReceiptIDs[0] != terminal.Receipts[0].ID {
			t.Fatalf("canonical terminal joins: %+v", terminal)
		}
	} else if journey {
		result = *data["delivery"].(*delivery.DeliveryResult)
	} else {
		t.Fatal("unexpected nonjourney")
		must(err)
	}
	canonicalHistory, err := s.ReadContributionDeliveryHistoryAs(ctx, subject, key)
	must(err)
	if final {
		if len(canonicalHistory.Histories) != 1 || len(canonicalHistory.Histories[0]) != 2 {
			t.Fatalf("final scoped Commit history: %+v", canonicalHistory)
		}
	}
	if !final && (len(result.Completed) != 1 || result.Completed[0] != delivery.Commit || len(result.Effects) != 1 || result.Effects[0].Outcome != delivery.Confirmed) {
		t.Fatalf("typed local result: %+v", result)
	}
	var localEffect delivery.Record
	if final {
		localEffect = canonicalHistory.Histories[0][1].Record
	} else {
		localEffect = result.Effects[0]
	}
	if localEffect.Outcome != delivery.Confirmed || localEffect.Intent.Operation != delivery.Commit {
		t.Fatalf("canonical local Commit not confirmed: %+v", localEffect)
	}
	if gitOwned("rev-parse", "refs/heads/"+localEffect.Intent.Branch) != localEffect.Head || gitOwned("rev-parse", localEffect.Head+"^{tree}") != candidate.CandidateTree {
		t.Fatal("authored ref/tree mismatch")
	}
	if gitOwned("rev-parse", "HEAD") != head || gitOwned("write-tree") != index || gitOwned("status", "--porcelain=v1", "--untracked-files=all") != status {
		t.Fatal("HEAD/index/private dirt changed")
	}
	history, err := s.ReadContributionDeliveryHistoryAs(ctx, subject, key)
	must(err)
	if len(history.Histories) != 1 || len(history.Histories[0]) != 2 || history.Histories[0][1].Record.Outcome != delivery.Confirmed {
		t.Fatalf("canonical scoped Commit reservation/readback: %+v", history)
	}
	if journey {
		before, e := s.ReadContributionExecutionAs(ctx, subject, key)
		must(e)
		beforeState := state
		if final {
			// Direct Journey readback distinguishes the completed local boundary
			// from whole-contribution success without executing another stage.
			direct, e := s.ExecuteContributionJourneyAs(ctx, subject, key)
			must(e)
			if direct.Result == nil || !reflect.DeepEqual(*direct.Result, terminal) || !direct.BoundaryCompleted || direct.Completed {
				t.Fatalf("direct local Journey canonical boundary readback: %+v", direct)
			}
			directExecution, e := s.ReadContributionExecutionAs(ctx, subject, key)
			must(e)
			_, directState, _, e := s.ObserveContributionContinuityAs(ctx, subject, key)
			must(e)
			directHistory, e := s.ReadContributionDeliveryHistoryAs(ctx, subject, key)
			must(e)
			if !reflect.DeepEqual(before, directExecution) || !reflect.DeepEqual(beforeState, directState) || !reflect.DeepEqual(history, directHistory) {
				t.Fatal("direct Journey readback changed native execution/effects/counters/history")
			}
		}
		replay, e := gateway.Execute(ctx, subject, id, token, "/contribute run "+issue.URL)
		must(e)
		after, e := s.ReadContributionExecutionAs(ctx, subject, key)
		must(e)
		_, afterState, _, e := s.ObserveContributionContinuityAs(ctx, subject, key)
		must(e)
		afterHistory, e := s.ReadContributionDeliveryHistoryAs(ctx, subject, key)
		must(e)
		if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(beforeState, afterState) || !reflect.DeepEqual(history, afterHistory) || replay.Data["completed"] != final {
			t.Fatal("journey replay rotated canonical identity/counters/history")
		}
		if final {
			assertTerminal(replay.Data, terminal)
			afterTerminal, e := s.GetFinalContributionResultAs(ctx, subject, key)
			must(e)
			if !reflect.DeepEqual(terminal, afterTerminal) {
				t.Fatal("replay changed canonical terminal")
			}
		}
	}
	gateway.stop()
	must(closeService())
	s, e = build(fixtureCommand)
	must(e)
	gateway = startContributionHTTPFixture(t, s)
	if _, err = gateway.Execute(context.Background(), subject, id, token, "/contribute status"); err == nil {
		t.Fatal("pre-restart manager token survived")
	}
	freshCtx, fresh, err := s.AuthenticateContributionUnixPeerRequest(context.Background(), uint32(os.Getuid()))
	must(err)
	opened, err = gateway.Open(freshCtx, fresh)
	must(err)
	_, err = gateway.Execute(freshCtx, fresh, opened.ID, opened.Token, "/contribute select "+lrRef.ID)
	must(err)
	afterRestart, err := s.ReadContributionExecutionAs(freshCtx, fresh, key)
	must(err)
	afterRestartHistory, err := s.ReadContributionDeliveryHistoryAs(freshCtx, fresh, key)
	must(err)
	if !reflect.DeepEqual(record, afterRestart) || !reflect.DeepEqual(history, afterRestartHistory) {
		t.Fatal("restart changed durable execution/history")
	}
	if final {
		cold, e := s.GetFinalContributionResultAs(freshCtx, fresh, key)
		must(e)
		if !reflect.DeepEqual(terminal, cold) {
			t.Fatal("cold owner read changed terminal")
		}
		status, e := gateway.Execute(freshCtx, fresh, opened.ID, opened.Token, "/contribute status "+key)
		must(e)
		assertTerminal(status.Data, terminal)
		afterCold, e := s.ReadContributionExecutionAs(freshCtx, fresh, key)
		must(e)
		afterColdHistory, e := s.ReadContributionDeliveryHistoryAs(freshCtx, fresh, key)
		must(e)
		if !reflect.DeepEqual(record, afterCold) || !reflect.DeepEqual(history, afterColdHistory) {
			t.Fatal("cold terminal navigation repeated execution/effects")
		}
		t.Logf("FINAL local boundary=%s whole=false disposition=%s artifact=%s receipt=%s; replay/restart identical", terminal.Report.Outcome, terminal.Disposition.GraphRunID, terminal.Artifact.ID, terminal.Receipts[0].ID)
	}
	t.Logf("PUBLIC ROOT actual native baseline=%s passes=%d consumed=%d GREEN=%s; Reserve/Git.Apply confirmed; post-replay identity/counters/history unchanged; instruction reads=%d", record.Baseline.Outcome, len(record.Passes), record.Consumed, record.Passes[0].Check.Outcome, reader.calls)
}
