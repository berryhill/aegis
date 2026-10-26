package managergateway

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/credentials"
	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	"github.com/berryhill/aegis/internal/runtime/hermes"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Own one-success native fixture; deterministic protocol, not live-provider proof.
func TestContributionDeliveryControllerNativeLocal(t *testing.T) {
	testContributionDeliveryControllerNativeLocal(t, false)
}

func TestContributionDeliveryLocalNarrowJourney(t *testing.T) {
	testContributionDeliveryControllerNativeLocal(t, true)
}

func testContributionDeliveryControllerNativeLocal(t *testing.T, journey bool) {
	disconnected := []bool{}
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
	runGit("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "base")
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
	s := contributionApp(t)
	clock := &contributionStageClock{at: time.Now().UTC()}
	s.Now = clock.Now
	expiry := len(disconnected) > 1 && disconnected[1]
	lifetime := int64(300)
	subject, e := s.Authenticate(ctx)
	must(e)
	// Fault only the initial subject; policy and fresh admission remain normal.
	_ = subject.ExpiresAt
	if expiry {
		subject.ExpiresAt = clock.Now().Add(time.Minute)
		_ = subject.ExpiresAt
	}
	repository, e := fleetbadger.Open(ctx, filepath.Join(s.Config.StateDir, "persistence", "fleet-v1"))
	must(e)
	defer repository.Close()
	refs := []reference.RevisionRef{}
	for _, id := range []string{"controller", "builder", "tester"} {
		charter := core.Charter{SchemaVersion: core.SchemaVersion, AgentID: id, Name: id, Revision: 1, Runtime: core.RuntimeConstraint{Adapter: "hermes", Runtime: "hermes-agent", VersionConstraint: ">=0.18.0", Target: "aegis-owned-ephemeral"}, CreatedBy: subject.PrincipalID, CreatedAt: s.Now(), Stanzas: []core.TrustStanza{{ID: "principal", Name: "principal", Enabled: true, Authentication: core.AuthenticationPolicy{Methods: []string{"local-os"}, Selectors: []core.IdentitySelector{{SubjectIDs: []string{subject.ID}, PrincipalIDs: []string{subject.PrincipalID}, Issuers: []string{"local-os"}, Environments: []string{"local"}}}}, Session: core.SessionPolicy{MaximumLifetimeSec: lifetime}, Approval: core.ApprovalPolicy{MaximumLifetimeSec: 300, SingleUse: true}, InformationFlow: core.InformationFlowPolicy{CrossStanza: "deny"}, Hermes: core.HermesConfig{Model: "synthetic-protocol", Provider: "none"}}}}
		cc, err := core.Canonicalize(charter)
		must(err)
		must(s.Store.SaveCharter(cc))
		// Same verified-provisioning fixture as app session lifecycle tests; isolated stores only.
		must(s.Store.Save("receipts", id, core.Receipt{ID: id, CharterDigest: cc.Digest, Status: "verified"}))
		agent, err := registry.SealRevision(registry.AgentRevision{SchemaVersion: registry.AgentRevisionSchemaVersion, AgentID: id, Revision: 1, Source: registry.FleetSource{FleetID: "fleet", Kind: "current-fleet", SourceID: id}, Runtime: registry.RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent", Target: "aegis-owned-ephemeral"}, Ownership: registry.Ownership{OwnerID: subject.PrincipalID, AccountabilityID: "fixture"}, Lifecycle: registry.LifecycleEnabled, Charter: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: id, Revision: 1, Digest: cc.Digest}, CapabilityDeclarations: []string{}, PolicyRefs: []reference.DigestRef{}})
		must(err)
		ref := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: id, Revision: 1, Digest: agent.Digest}
		refs = append(refs, ref)
		_, err = repository.RegisterAgent(ctx, registry.AgentRegistration{SchemaVersion: registry.AgentRegistrationSchemaVersion, AgentID: id, Source: agent.Source, InitialRevision: ref}, agent, fleet.AuditFact{Event: core.AuditEvent{Type: "fixture.created", SubjectID: subject.ID, PrincipalID: subject.PrincipalID, Outcome: "succeeded", Reason: "isolated fixture"}})
		must(err)
	}
	nonlocalCancellation := len(disconnected) > 3 && disconnected[3]
	effect := "local-only"
	if nonlocalCancellation {
		effect = "verified-protected-base-merge"
	}
	lr, lv, e := loop.NewContributionRevision("managed", 1, "", c, loop.ContributionProfile{Controller: refs[0], Builder: refs[1], Tester: refs[2], Repository: "fixture/controlled", RepositoryID: 7, Base: base, ReadAction: contribution.ReadIssueAction, Effect: effect, DeadlineSeconds: 300, Invariants: []string{"preserve trusted tests"}, Exclusions: []string{"no GitHub effects"}, Managed: &policy})
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
	routed, err := orchestration.NewRoutedRuntimeAdapter(adapter, s.Config.StateDir)
	must(err)
	fs, err := orchestration.NewFleetService(repository, s.Authority, s.AuthorityCommands, func(_ context.Context, _ orchestration.FleetAction, actor core.Subject) error {
		return s.RequirePrincipal(actor)
	}, nil, s.Now)
	must(err)
	verifier, err := evidence.NewBlobVerifier(s.Store)
	must(err)
	worker, err := orchestration.NewQueueWorker(repository, fs, s.Store, verifier, routed, s.Now)
	must(err)
	must(s.ConfigureFleet(repository, fs, worker))
	goBin, _ := exec.LookPath("go")
	goBin, _ = filepath.EvalSymlinks(goBin)
	s.Config.Implementation = config.Implementation{GoBinary: goBin, AuthorizedContracts: []string{lr.Digest}}
	must(worker.ConfigureImplementation(s.Config.Implementation, s.Config.StateDir, adapter))
	worker.SetContributionFetch(func(context.Context, loop.ContributionExecutionPolicy) error {
		runGit("fetch", "--no-tags", repoPath, "main")
		return nil
	})
	_, err = s.PublishLoopAs(ctx, subject, app.PublishLoopInput{AgentID: refs[0].ID, Revision: lr, IdempotencyKey: "publish"})
	must(err)
	_, err = s.SetLoopLifecycleAs(ctx, subject, lr.LoopID, app.SetLoopLifecycleInput{AgentID: refs[0].ID, Loop: lrRef, State: loop.LifecycleActive, EventID: "activate"})
	must(err)
	issue, _ := contribution.ParseIssueURL("https://github.com/fixture/controlled/issues/1")
	reader := &fixedContributionReader{source: contribution.Source{URL: issue, RepositoryID: 7, IssueID: 9, Author: contribution.Author{ID: 1, Login: "fixture"}, State: "open", CreatedAt: s.Now(), UpdatedAt: s.Now(), ObservedAt: s.Now(), ContentDigest: contribution.Digest([]byte("fixture")), Discussion: []contribution.Comment{}}}
	s.ContributionReader = reader
	gateway, err := New(ctx, s)
	must(err)
	id, token := "approved-native-manager", "isolated-token"
	gateway.sessions[id] = session{id: id, token: sha256.Sum256([]byte(token)), subject: subject, issuedAt: s.Now(), expires: subject.ExpiresAt, mode: "degraded"}
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
	deny := &jointCancelDeny{}
	gh, err := delivery.NewGitHub("https://api.github.com", deny, deny)
	must(err)
	server := &app.ContributionDeliveryServerPolicy{Convention: "contribution-publication.v1", Actor: "fixture", Author: delivery.Identity{Name: "Test Author", Email: "author@example.invalid"}, Committer: delivery.Identity{Name: "Test Controller", Email: "controller@example.invalid"}, ProtectionPolicy: contribution.Digest([]byte("trusted synthetic protection")), RequiredChecks: []string{"native"}, RequiredReviewers: []string{"independent"}, PostMergeChecks: []string{"postmerge"}, MergeMethod: "squash"}
	var controller *app.ContributionDeliveryController
	if journey {
		_, err = app.InstallContributionDeliveryPublicationOwner(s, deny, gh, func(context.Context) (core.Subject, error) { return subject, nil })
		must(err)
		controller, err = app.InstallContributionDeliveryController(s, server, &credentials.Authority{}, deny)
		must(err)
	}
	// Snapshot the source repository before the FIRST automatic manager effect.
	// The owned worktree does not exist yet; its original HEAD/index must match
	// this sampled protected base, not a post-publication baseline.
	privatePath := filepath.Join(repoPath, "private-canary.txt")
	privateBytes := []byte("private source canary: never publish or alter\n")
	must(os.WriteFile(privatePath, privateBytes, 0600))
	preHead, preIndex := runGit("rev-parse", "HEAD"), runGit("write-tree")
	preStatus := runGit("status", "--porcelain=v1", "--untracked-files=all")
	response, err := gateway.Execute(runCtx, subject, id, token, "/contribute run "+issue.URL)
	if err != nil {
		t.Fatalf("manager run: %v state=%s reason=%s data=%+v", err, response.State, response.Reason, response.Data)
	}
	data := response.Data
	if data["completed"] != false {
		t.Fatal("candidate falsely completed")
	}
	if !journey && (response.State != "unavailable" || response.Reason != "delivery_controller_unavailable") {
		t.Fatalf("candidate-only not held: %+v", response)
	}
	key := app.ContributionIntentKey(lrRef, issue.URL, "")
	sealed, state, _, err := s.ObserveContributionContinuityAs(ctx, subject, key)
	must(err)
	candidate, err := s.AcceptedContributionCandidateAs(ctx, subject, key)
	must(err)
	record, err := s.ReadContributionExecutionAs(ctx, subject, key)
	must(err)
	if record.State != "candidate_accepted" || record.Consumed != 1 || len(record.Passes) != 1 || record.Baseline.Outcome != "failed" || record.Passes[0].Check.Outcome != "passed" {
		t.Fatal("real native RED/GREEN missing")
	}
	for _, role := range []string{"builder", "tester"} {
		if !contribution.NativeRoleQuiescent(state, role) {
			t.Fatal("native Wait absent", role)
		}
	}
	if !journey {
		_, err = app.InstallContributionDeliveryPublicationOwner(s, deny, gh, func(context.Context) (core.Subject, error) { return subject, nil })
		must(err)
		controller, err = app.NewContributionDeliveryController(s, server, &credentials.Authority{}, deny)
		must(err)
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
	head := gitOwned("rev-parse", "HEAD")
	index := gitOwned("write-tree")
	status := gitOwned("status", "--porcelain=v1", "--untracked-files=all")
	beforeHistory, beforeErr := s.ReadContributionDeliveryHistoryAs(ctx, subject, key)
	original := reader.source
	// Neither changed task nor discussion may reserve even through typed RunAs.
	for _, kind := range []string{"task", "discussion"} {
		reader.source = original
		if kind == "task" {
			reader.source.State = "closed"
		} else {
			reader.source.Discussion = []contribution.Comment{{}}
		}
		if journey {
			replay, e := gateway.Execute(ctx, subject, id, token, "/contribute run "+issue.URL)
			if e == nil && (replay.State != "unavailable" || replay.Data["held_reason"] == "") {
				t.Fatal("journey instruction drift bypass", kind)
			}
		} else if _, e := controller.RunAs(ctx, subject, key, state.RoleAuthorities["controller"].SessionID); e == nil {
			t.Fatal("instruction drift bypass", kind)
		}
		h, e := s.ReadContributionDeliveryHistoryAs(ctx, subject, key)
		if e != beforeErr {
			t.Fatalf("drift changed missing history: %v / %v", beforeErr, e)
		}
		if !reflect.DeepEqual(h, beforeHistory) {
			t.Fatal("drift reserved", kind)
		}
	}
	reader.source = original
	var result delivery.DeliveryResult
	if journey {
		result = *data["delivery"].(*delivery.DeliveryResult)
	} else {
		result, err = controller.RunAs(ctx, subject, key, state.RoleAuthorities["controller"].SessionID)
		must(err)
	}
	if len(result.Completed) != 1 || result.Completed[0] != delivery.Commit || len(result.Effects) != 1 || result.Effects[0].Outcome != delivery.Confirmed {
		t.Fatalf("typed local result: %+v", result)
	}
	localEffect := result.Effects[0]
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
	if deny.credentials.Load() != 0 || deny.privacy.Load() != 0 || deny.historical.Load() != 0 || deny.authority.Load() != 0 {
		t.Fatal("local effect touched remote path")
	}
	if journey {
		before, e := s.ReadContributionExecutionAs(ctx, subject, key)
		must(e)
		beforeState := state
		replay, e := gateway.Execute(ctx, subject, id, token, "/contribute run "+issue.URL)
		must(e)
		after, e := s.ReadContributionExecutionAs(ctx, subject, key)
		must(e)
		_, afterState, _, e := s.ObserveContributionContinuityAs(ctx, subject, key)
		must(e)
		afterHistory, e := s.ReadContributionDeliveryHistoryAs(ctx, subject, key)
		must(e)
		if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(beforeState, afterState) || !reflect.DeepEqual(history, afterHistory) || replay.Data["completed"] != false {
			t.Fatal("journey replay rotated canonical identity/counters/history")
		}
	}
	t.Logf("actual native one-candidate -> typed RunAs -> canonical Reserve -> Git.Apply confirmed; instruction reads=%d; no credentials/privacy/historical/authority injection", reader.calls)
}
