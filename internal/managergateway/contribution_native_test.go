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
	"strings"
	"sync"
	"testing"
	"time"
)

// Deterministic protocol qualification, not real-provider proof.
func TestContributionManagerApprovedNativeRepair(t *testing.T) { contributionManagerNativeRepair(t) }
func TestContributionManagerDisconnectKeepsValidAuthorityExecution(t *testing.T) {
	contributionManagerNativeRepair(t, true)
}
func TestContributionManagerExpiredFrozenCandidateExplicitFreshResume(t *testing.T) {
	contributionManagerNativeRepair(t, false, true)
}
func contributionManagerNativeRepair(t *testing.T, disconnected ...bool) {
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
	expiry := len(disconnected) > 1 && disconnected[1]
	var clockMu sync.Mutex
	stageNow := time.Now().UTC()
	s.Now = func() time.Time { clockMu.Lock(); defer clockMu.Unlock(); return stageNow }
	lifetime := int64(300)
	if expiry {
		lifetime = 60
		s.Config.Principal.AuthTTL = time.Minute
	}
	subject, e := s.Authenticate(ctx)
	must(e)
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
	lr, lv, e := loop.NewContributionRevision("managed", 1, "", c, loop.ContributionProfile{Controller: refs[0], Builder: refs[1], Tester: refs[2], Repository: "fixture/controlled", RepositoryID: 7, Base: base, ReadAction: contribution.ReadIssueAction, Effect: "local-only", DeadlineSeconds: 300, Invariants: []string{"preserve trusted tests"}, Exclusions: []string{"no GitHub effects"}, Managed: &policy})
	if e != nil {
		t.Fatalf("revision: %v %+v", e, lv.Issues)
	}
	lrRef := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: lr.LoopID, Revision: 1, Digest: lr.Digest}
	message := func(n int) string {
		p, _ := json.Marshal(map[string]any{"edits": []implementation.Edit{{Path: "value.go", Content: []byte("package main\nfunc Value() int{return 40}\n")}, {Path: "offset.go", Content: []byte(fmt.Sprintf("package main\nfunc Offset() int{return %d}\n", n))}}})
		return string(p)
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
`, filepath.Join(root, "turns"), filepath.Join(root, "turns"), filepath.Join(root, "turns"), nativeJSON(message(1)), nativeJSON(message(2)))
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
	adapter := hermes.New(executable, slog.New(slog.NewTextHandler(io.Discard, nil)))
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
	if expiry {
		paused := false
		worker.SetContributionCheckpointObserver(func(_ context.Context, cp implementation.ContributionCheckpoint) error {
			if cp.Stage == "freeze" && !paused {
				paused = true
				clockMu.Lock()
				stageNow = stageNow.Add(61 * time.Second)
				clockMu.Unlock()
				return fmt.Errorf("controlled frozen expiry hold")
			}
			return nil
		})
	}
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
	result, err := gateway.Execute(runCtx, subject, id, token, "/contribute run "+issue.URL)
	key := app.ContributionIntentKey(lrRef, issue.URL, "")
	if expiry {
		if err == nil {
			t.Fatal("expired run unexpectedly finished effects")
		}
		if _, e := s.ResumeContributionAs(ctx, subject, key); e == nil {
			t.Fatal("expired subject resumed")
		}
		subject, e = s.Authenticate(ctx)
		must(e)
		_, held, _, e := s.ObserveContributionContinuityAs(ctx, subject, key)
		must(e)
		retained, e := s.ReadContributionExecutionAs(ctx, subject, key)
		must(e)
		if retained.State != "phase_boundary" || retained.Frozen == nil || retained.Consumed != 1 || held.Used.Candidates != 1 || held.Used.RuntimeTurns != 1 || held.Stage != "freeze" {
			t.Fatalf("expiry did not retain exact frozen stage: %+v %+v", retained, held)
		}
		wrong := subject
		wrong.PrincipalID = "different-owner"
		if _, e = s.ResumeContributionAs(ctx, wrong, key); e == nil {
			t.Fatal("wrong owner resumed")
		}
		if _, e = s.ResumeContributionRoleAs(ctx, subject, key, "controller", app.ContributionContinuityInput{ContractDigest: held.ContractDigest, ExpectedSequence: held.Sequence - 1, ExpectedDigest: held.Digest}); e == nil {
			t.Fatal("stale epoch resumed")
		}
		oldProjection, e := repository.GetQueueProjection(ctx, held.QueueItemID)
		must(e)
		oldClaim, e := repository.GetClaim(ctx, oldProjection.ActiveClaimID)
		must(e)
		oldAttempt, e := repository.GetAttempt(ctx, oldClaim.AttemptID)
		must(e)
		if s.Now().Before(oldClaim.ExpiresAt) {
			t.Fatal("original claim expiry not exercised")
		}
		id = "fresh-explicit-resume-manager"
		gateway.sessions[id] = session{id: id, token: sha256.Sum256([]byte(token)), subject: subject, issuedAt: s.Now(), expires: subject.ExpiresAt, mode: "degraded"}
		result, err = gateway.Execute(ctx, subject, id, token, "/contribute resume "+key)
		if err != nil {
			t.Fatalf("fresh explicit resume: %+v %v", result, err)
		}
		newProjection, e := repository.GetQueueProjection(ctx, held.QueueItemID)
		must(e)
		newClaim, e := repository.GetClaim(ctx, newProjection.ActiveClaimID)
		must(e)
		newAttempt, e := repository.GetAttempt(ctx, newClaim.AttemptID)
		must(e)
		if newClaim != oldClaim || newAttempt != oldAttempt || newProjection.Attempts != 1 {
			t.Fatal("resume changed canonical attempt/claim")
		}
		resumed, e := s.ReadContributionExecutionAs(ctx, subject, key)
		must(e)
		if len(resumed.Passes) != 2 || resumed.Passes[0].Candidate.Digest != retained.Frozen.Digest {
			t.Fatal("resume rebuilt or substituted frozen candidate")
		}
	}
	if err != nil {
		t.Fatalf("run: %+v %v", result, err)
	}
	if len(disconnected) != 0 && disconnected[0] && runCtx.Err() == nil {
		t.Fatal("builder disconnect not exercised")
	}
	_, continuity, events, err := s.ObserveContributionContinuityAs(ctx, subject, key)
	must(err)
	if continuity.Used.Candidates != 2 || continuity.Used.QueueAttempts != 1 || continuity.Used.RuntimeTurns != 4 || continuity.Stage != "review" {
		t.Fatalf("owning counters/stage: %+v", continuity)
	}
	for _, event := range events {
		if event.Kind == "reserve" && event.OperationID == "" {
			t.Fatal("owning reservation missing stable operation identity")
		}
	}
	record, err := s.ReadContributionExecutionAs(ctx, subject, key)
	if err != nil {
		t.Fatalf("readback after %+v: %v", result, err)
	}
	if record.State != "candidate_accepted" || len(record.Passes) != 2 || record.Baseline.Outcome != "failed" || record.Passes[0].Check.Outcome != "failed" || record.Passes[1].Check.Outcome != "passed" {
		t.Fatalf("RED repair GREEN: %+v", record)
	}
	candidate, err := s.AcceptedContributionCandidateAs(ctx, subject, key)
	must(err)
	if candidate.Digest != record.Passes[1].Candidate.Digest {
		t.Fatal("accepted handoff drift")
	}
	before, err := s.Authority.ListAuthorityContexts(ctx)
	must(err)
	info, err := os.Stat(c.Workspace)
	must(err)
	_, err = gateway.Execute(ctx, subject, id, token, "/contribute run "+issue.URL)
	must(err)
	after, err := s.Authority.ListAuthorityContexts(ctx)
	must(err)
	items, err := repository.ListQueueItems(ctx)
	must(err)
	again, err := os.Stat(c.Workspace)
	must(err)
	if len(items) != 1 || reader.calls != 1 || len(before) != 4 || len(after) != len(before) || !os.SameFile(info, again) {
		t.Fatalf("replay duplicated effects queue=%d reads=%d sessions=%d/%d", len(items), reader.calls, len(before), len(after))
	}
	for _, authority := range after {
		if authority.AgentID != "controller" {
			admission, err := s.AuthorityCommands.AuthorityAdmission(ctx, authority.ID, authority.Digest, s.Now())
			must(err)
			if admission.Admitted {
				t.Fatal("role authority survived close")
			}
		}
	}
	t.Cleanup(func() {
		for _, authority := range after {
			_ = s.RevokeSession(context.Background(), authority.SessionID, "fixture cleanup")
		}
	})
	t.Log("authenticated manager -> real app role resolver -> Queue -> native multi-file RED/repair/GREEN -> accepted readback and same-intent replay; deterministic Hermes protocol, NOT real-provider proof")
}
func nativeJSON(s string) string { b, _ := json.Marshal(s); return string(b) }
