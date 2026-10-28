package managergateway

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
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
	"github.com/berryhill/aegis/internal/queue"
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
func TestContributionManagerApprovedNativeRepair(t *testing.T) {
	contributionManagerNativeRepair(t, false, false, false, false, true)
}
func TestContributionDeliveryJointNative(t *testing.T) {
	contributionManagerNativeRepair(t, false, false, true)
}
func TestContributionDeliveryJointNativeNonlocalCancellation(t *testing.T) {
	contributionManagerNativeRepair(t, false, false, false, true)
}

func TestContributionManagerDisconnectKeepsValidAuthorityExecution(t *testing.T) {
	contributionManagerNativeRepair(t, true)
}
func TestContributionManagerExpiredFrozenCandidateExplicitFreshResume(t *testing.T) {
	contributionManagerNativeRepair(t, false, true)
}
func contributionManagerNativeRepair(t *testing.T, disconnected ...bool) {
	ctx := context.Background()
	root := os.Getenv("AEGIS_RESTART_ROOT")
	if root == "" {
		root = t.TempDir()
	}
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
	s := contributionAppAt(t, filepath.Join(root, "state"), true)
	expiry := len(disconnected) > 1 && disconnected[1]
	lifetime := int64(300)
	clock := &contributionStageClock{at: time.Now().UTC()}
	s.Now = clock.Now
	ctx, subject, e := s.AuthenticateContributionUnixPeerRequest(ctx, contributionDeliveryConfiguredUID(t, s))
	must(e)
	// Keep the ordinary issued 5m subject lifetime. The expiry journey ages this
	// already-issued authentication before intake, leaving distinct immutable
	// authority and contribution deadlines without racing cold native Go work.
	initialExpiry := subject.ExpiresAt
	repository, e := fleetbadger.Open(ctx, filepath.Join(s.Config.StateDir, "persistence", "fleet-v1"))
	must(e)
	defer repository.Close()
	refs := []reference.RevisionRef{}
	for _, id := range []string{"controller", "builder", "tester"} {
		charter := core.Charter{SchemaVersion: core.SchemaVersion, AgentID: id, Name: id, Revision: 1, Runtime: core.RuntimeConstraint{Adapter: "hermes", Runtime: "hermes-agent", VersionConstraint: ">=0.18.0", Target: "aegis-owned-ephemeral"}, CreatedBy: subject.PrincipalID, CreatedAt: s.Now(), Stanzas: []core.TrustStanza{{ID: "principal", Name: "principal", Enabled: true, Authentication: core.AuthenticationPolicy{Methods: []string{"local-os"}, Selectors: []core.IdentitySelector{{SubjectIDs: []string{subject.ID}, PrincipalIDs: []string{subject.PrincipalID}, Issuers: []string{subject.Issuer}, Environments: []string{"local"}}}}, Session: core.SessionPolicy{MaximumLifetimeSec: lifetime}, Approval: core.ApprovalPolicy{MaximumLifetimeSec: 300, SingleUse: true}, InformationFlow: core.InformationFlowPolicy{CrossStanza: "deny"}, Hermes: core.HermesConfig{Model: "synthetic-protocol", Provider: "none"}}}}
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
	initialOffset, expectedPasses := 1, 2
	if nonlocalCancellation {
		initialOffset, expectedPasses = 2, 1
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
	finishSupervision := func() {}
	if len(disconnected) > 4 && disconnected[4] {
		// Preserve #378's joint-delivery and cancellation flags at indexes 2/3.
		// Exercise the real service supervisor concurrently with resolver-produced
		// builder/tester Close and exact native Wait/stop acknowledgement.
		supervise, stop := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- s.Supervise(supervise) }()
		var once sync.Once
		finishSupervision = func() {
			once.Do(func() {
				stop()
				select {
				case err := <-done:
					if err != nil && !errors.Is(err, context.Canceled) {
						t.Errorf("actual supervisor: %v", err)
					}
				case <-time.After(5 * time.Second):
					t.Error("actual supervisor cleanup did not stop")
				}
			})
		}
		defer finishSupervision()
	}
	paused := false
	var observedMu sync.Mutex
	var observedRun context.Context
	worker.SetContributionCheckpointObserver(func(checkpointCtx context.Context, cp implementation.ContributionCheckpoint) error {
		observedMu.Lock()
		observedRun = checkpointCtx
		observedMu.Unlock()
		logContributionRunContext(t, "checkpoint", checkpointCtx)
		t.Logf("G1 checkpoint stage=%s run=%s attempt=%s subject_live=%t", cp.Stage, cp.RunID, cp.AttemptID, s.Now().Before(subject.ExpiresAt))
		if expiry && cp.Stage == "freeze" && !paused {
			paused = true
			key := app.ContributionIntentKey(lrRef, "https://github.com/fixture/controlled/issues/1", "")
			sealed, _, err := s.ReadContributionAs(checkpointCtx, subject, key)
			must(err)
			if !initialExpiry.Before(sealed.Deadline) {
				t.Fatal("authority expiry must precede original contribution deadline")
			}
			clock.Advance(initialExpiry.Sub(clock.Now()) + time.Second)
			if !clock.Now().Before(sealed.Deadline) {
				t.Fatal("expiry fault exhausted contribution deadline")
			}
			return fmt.Errorf("controlled frozen expiry hold")
		}
		return nil
	})

	goBin, _ := exec.LookPath("go")
	goBin, _ = filepath.EvalSymlinks(goBin)
	s.Config.Implementation = config.Implementation{GoBinary: goBin, AuthorizedContracts: []string{lr.Digest}}
	must(worker.ConfigureImplementation(s.Config.Implementation, s.Config.StateDir, adapter))
	if os.Getenv("AEGIS_RESTART_PHASE") == "initial" {
		installRestartCheckpoint(t, s, repository, worker, root, lrRef)
	}
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
	if expiry {
		// Phase separation BEFORE admission: never edit an issued subject, role,
		// claim or sealed deadline. Strict 60s capping has its own fast regression.
		if initialExpiry.Sub(subject.AuthenticatedAt) != 5*time.Minute {
			t.Fatal("ordinary subject lifetime changed")
		}
		clock.Advance(30 * time.Second)
	}
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
		ctx, subject, e = s.AuthenticateContributionUnixPeerRequest(ctx, contributionDeliveryConfiguredUID(t, s))
		must(e)
		if s.Config.Principal.AuthTTL != 5*time.Minute || lifetime != 300 || !subject.ExpiresAt.Equal(s.Now().Add(5*time.Minute)) {
			t.Fatal("fresh authentication changed baseline policy")
		}
		freshMandate, _, previewErr := s.PreviewSessionAs(ctx, subject, "controller", 1, "", core.Environment{Name: "local"})
		must(previewErr)
		if !freshMandate.ExpiresAt.Equal(s.Now().Add(300 * time.Second)) {
			t.Fatal("fresh mandate inherited initial fault")
		}
		heldContract, held, _, e := s.ObserveContributionContinuityAs(ctx, subject, key)
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
		if !oldClaim.ExpiresAt.Equal(initialExpiry) {
			t.Fatal("original claim not capped by initial subject")
		}
		if !oldClaim.ExpiresAt.Before(heldContract.Deadline) || !s.Now().Before(heldContract.Deadline) {
			t.Fatal("expired-claim resume must retain the original unexpired contribution deadline")
		}
		for _, role := range held.RoleAuthorities {
			mandate, readErr := s.Authority.GetMandate(ctx, role.MandateID)
			must(readErr)
			if !mandate.ExpiresAt.Equal(initialExpiry) {
				t.Fatal("original role mandate not capped by initial subject")
			}
		}
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
		observedMu.Lock()
		lastRun := observedRun
		observedMu.Unlock()
		logContributionRunContext(t, "failure-last-checkpoint", lastRun)
		// Read canonical private state without minting replacement authority. Keep
		// diagnostics closed: references, stages and time relationships only.
		items, readErr := repository.ListQueueItems(ctx)
		if readErr == nil {
			for _, item := range items {
				state, _, stateErr := repository.GetContributionContinuity(ctx, item.ItemID)
				projection, projectionErr := repository.GetQueueProjection(ctx, item.ItemID)
				t.Logf("G1 boundary queue=%s stage=%s sequence=%d continuity_read=%t projection_read=%t state=%s subject_live=%t", item.ItemID, state.Stage, state.Sequence, stateErr == nil, projectionErr == nil, projection.State, s.Now().Before(subject.ExpiresAt))
				if projection.ActiveClaimID != "" {
					claim, claimErr := repository.GetClaim(ctx, projection.ActiveClaimID)
					t.Logf("G1 claim ref=%s read=%t live=%t", projection.ActiveClaimID, claimErr == nil, s.Now().Before(claim.ExpiresAt))
				}
				for name, role := range state.RoleAuthorities {
					mandate, mandateErr := s.Authority.GetMandate(ctx, role.MandateID)
					t.Logf("G1 role=%s epoch=%d session=%s mandate=%s read=%t mandate_live=%t subject_live=%t", name, role.Epoch, role.SessionID, role.MandateID, mandateErr == nil, s.Now().Before(mandate.ExpiresAt), s.Now().Before(mandate.Subject.ExpiresAt))
				}
			}
		}
		t.Fatalf("run error_type=%T denied=%t", err, err == app.ErrDenied)
	}
	if len(disconnected) != 0 && disconnected[0] && runCtx.Err() == nil {
		t.Fatal("builder disconnect not exercised")
	}
	sealed, continuity, events, err := s.ObserveContributionContinuityAs(ctx, subject, key)
	must(err)
	if sealed.CandidateBudget != 2 {
		t.Fatal("fixture changed sealed two-candidate ceiling")
	}
	if continuity.Used.Candidates != uint32(expectedPasses) || continuity.Used.QueueAttempts != 1 || continuity.Used.RuntimeTurns != uint32(2*expectedPasses) || continuity.Stage != "review" {
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
	if record.State != "candidate_accepted" || record.Consumed != uint8(expectedPasses) || len(record.Passes) != expectedPasses || record.Baseline.Outcome != "failed" {
		t.Fatalf("native RED baseline / actual candidate accounting: %+v", record)
	}
	if record.Passes[expectedPasses-1].Check.Outcome != "passed" || (!nonlocalCancellation && record.Passes[0].Check.Outcome != "failed") {
		t.Fatalf("native candidate GREEN / deliberate repair: %+v", record)
	}
	// The one explicit continuation retains its admitted controller through
	// candidate handoff; no second consent or replacement controller is needed.
	candidate, err := s.AcceptedContributionCandidateAs(ctx, subject, key)
	must(err)
	if candidate.Digest != record.Passes[expectedPasses-1].Candidate.Digest {
		t.Fatal("accepted handoff drift")
	}
	controller := continuity.RoleAuthorities["controller"]
	request := contribution.EffectAdmissionRequest{Contract: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: sealed.QueueItemID, Digest: sealed.Digest}, RunID: sealed.RunID, QueueItemID: sealed.QueueItemID, Candidate: candidate.Digest, CandidateEvidence: continuity.CandidateEvidence, Role: "controller", Epoch: controller.Epoch, RoleAuthority: controller.Ref(), Scope: sealed.Effect}
	if _, e := s.CheckContributionEffectAs(ctx, subject, controller.SessionID, key, request, true); e == nil {
		t.Fatal("write outside custody")
	}
	var escaped context.Context
	must(s.WithContributionContinuityCandidateAs(ctx, subject, controller.SessionID, key, func(held context.Context, frozen orchestration.ContributionCandidate) error {
		escaped = held
		if frozen.Digest != candidate.Digest {
			return fmt.Errorf("frozen identity drift")
		}
		_, e := s.CheckContributionEffectAs(held, subject, controller.SessionID, key, request, true)
		return e
	}))
	if escaped == nil {
		t.Fatal("missing held callback")
	}
	if _, e := s.CheckContributionEffectAs(escaped, subject, controller.SessionID, key, request, true); e == nil {
		t.Fatal("escaped custody context")
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
	expectedAuthorities := 4
	if nonlocalCancellation {
		expectedAuthorities = 3 // controller, first builder and first independent tester only
	}
	if expiry {
		expectedAuthorities = 6 // original controller/builder plus independently resumed roles
		_, fresh, history, e := s.ObserveContributionContinuityAs(ctx, subject, key)
		must(e)
		if fresh.RoleEpochs["controller"] != 2 || fresh.RoleEpochs["builder"] != 2 || fresh.RoleEpochs["tester"] != 2 {
			t.Fatalf("lost fresh role epochs: %+v", fresh.RoleEpochs)
		}
		intents := map[string]bool{}
		for _, event := range history {
			if event.Kind == "native-reserved" && event.NativeEffect != nil {
				id := event.NativeEffect.Binding.IntentID
				if intents[id] {
					t.Fatalf("native intent replay: %s", id)
				}
				intents[id] = true
			}
		}
	}
	if len(items) != 1 || reader.calls != 1 || len(before) != expectedAuthorities || len(after) != len(before) || !os.SameFile(info, again) {
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
	// Delivery consumes the accepted candidate while its admitted controller
	// and source custody remain valid; cancellation must fence later effects.
	if nonlocalCancellation {
		contributionDeliveryJointNativeCancellation(t, ctx, s, subject, key, controller.SessionID, candidate)
		return
	}
	if len(disconnected) > 2 && disconnected[2] {
		t.Cleanup(func() {
			for _, a := range after {
				_ = s.RevokeSession(context.Background(), a.SessionID, "joint fixture cleanup")
			}
		})
		contributionDeliveryJointNative(t, ctx, s, subject, key, controller.SessionID, candidate)
		return // Canonical reservation exists; nil publication quiescer remains blocked.
	}
	contributionDeliveryNativeBridge(t, ctx, s, subject, key, controller.SessionID, candidate)
	// Actual manager cancellation of the accepted candidate: no second resume or
	// consent, no authority/counter reset, and Queue terminal only after stop joins.
	_, rootBefore, err := s.ReadContributionAs(ctx, subject, key)
	must(err)
	_, err = gateway.Execute(ctx, subject, id, token, "/contribute stop "+key)
	must(err)
	_, stopped, _, err := s.ObserveContributionContinuityAs(ctx, subject, key)
	must(err)
	if !stopped.CancelRequested || !stopped.CancelAcknowledged || stopped.Used != continuity.Used {
		t.Fatalf("accepted candidate stop not joined: %+v", stopped)
	}
	for role := range stopped.RoleAuthorities {
		if !stopped.RoleStops[role].Acknowledged {
			t.Fatalf("role %s not acknowledged", role)
		}
		if b, ok := stopped.NativeBoundaries[role]; ok && !b.Closed {
			t.Fatalf("native %s open", role)
		}
	}
	_, rootAfter, err := s.ReadContributionAs(ctx, subject, key)
	must(err)
	if rootAfter.Projection.State != queue.StateCancelled || rootBefore.RuntimeAuthorityBinding == nil || rootAfter.RuntimeAuthorityBinding == nil || rootAfter.RuntimeAuthorityBinding.Digest != rootBefore.RuntimeAuthorityBinding.Digest {
		t.Fatal("stop replaced root or missed terminal")
	}
	status, err := s.ReadContributionContinuityViewAs(ctx, subject, key)
	must(err)
	if !status.CancelRequested || !status.CancelAcknowledged {
		t.Fatal("sanitized cancel status missing")
	}
	for _, role := range status.Roles {
		if !role.StopRequested || !role.StopAcknowledged {
			t.Fatal("sanitized stop status missing")
		}
	}
	_, err = gateway.Execute(ctx, subject, id, token, "/contribute stop "+key)
	must(err)
	_, repeated, _, err := s.ObserveContributionContinuityAs(ctx, subject, key)
	must(err)
	if repeated.Digest != stopped.Digest {
		t.Fatal("repeated stop mutated history")
	}
	if _, e := s.CheckContributionEffectAs(ctx, subject, controller.SessionID, key, request, true); e == nil {
		t.Fatal("cancelled write admitted")
	}
	if _, e := s.CheckContributionEffectAs(ctx, subject, controller.SessionID, key, request, false); e != nil {
		t.Fatal("cancelled historical reconciliation", e)
	}
	finalAuthorities, err := s.Authority.ListAuthorityContexts(ctx)
	must(err)
	if len(finalAuthorities) != len(after) {
		t.Fatal("stop minted authority")
	}
	t.Cleanup(func() {
		for _, authority := range after {
			_ = s.RevokeSession(context.Background(), authority.SessionID, "fixture cleanup")
		}
	})

	// Expiry is explicit and last: functional native checks retain all 300s TTLs.
	// Finish the actual concurrent supervisor/native-close journey at its current
	// stage before advancing the fixture-only clock for separate denial assertions.
	// Shutdown errors remain checked; no expired control command is fabricated.
	finishSupervision()
	clock.Advance(301 * time.Second)
	if _, err := gateway.Execute(ctx, subject, id, token, "/contribute status "+key); err == nil {
		t.Fatal("expired manager authority accepted")
	}
	if _, err := s.AcceptedContributionCandidateAs(ctx, subject, key); err == nil {
		t.Fatal("expired app authority accepted")
	}
	t.Log("authenticated manager -> real app role resolver -> Queue -> native multi-file RED/repair/GREEN -> accepted readback and same-intent replay; deterministic Hermes protocol, NOT real-provider proof")
}

// One synchronized logical authority clock is installed before any consumer
// captures s.Now. Native Go tools and context deadlines remain real.
type contributionStageClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *contributionStageClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.at }
func (c *contributionStageClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

// Observe only closed context states, never arbitrary error/cause strings. A
// cancelled last checkpoint after return may be cleanup, not the first denial.
func contributionContextCode(err error) string {
	switch {
	case err == nil:
		return "live"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	default:
		return "other"
	}
}
func logContributionRunContext(t *testing.T, boundary string, ctx context.Context) {
	t.Helper()
	if ctx == nil {
		t.Logf("G1 context boundary=%s observed=false", boundary)
		return
	}
	deadline, hasDeadline := ctx.Deadline()
	t.Logf("G1 context boundary=%s observed=true has_deadline=%t wall_deadline_elapsed=%t err=%s cause=%s", boundary, hasDeadline, hasDeadline && !time.Now().Before(deadline), contributionContextCode(ctx.Err()), contributionContextCode(context.Cause(ctx)))
}

func TestContributionLogicalClockDoesNotExtendAbsoluteRunDeadline(t *testing.T) {
	// Past absolute deadlines are synchronous: no sleeps or changed production clock.
	clock := &contributionStageClock{at: time.Now().UTC().Add(-10 * time.Minute)}
	logicalStart := clock.Now()
	deadline := logicalStart.Add(300 * time.Second)
	run, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	if !clock.Now().Before(deadline) || !clock.Now().Equal(logicalStart) {
		t.Fatal("fixed authority clock unexpectedly advanced")
	}
	if run.Err() != context.DeadlineExceeded || context.Cause(run) != context.DeadlineExceeded {
		t.Fatal("absolute Go deadline did not expire independently of logical authority")
	}
	logContributionRunContext(t, "deterministic-expiry", run)
}

func nativeJSON(s string) string { b, _ := json.Marshal(s); return string(b) }
