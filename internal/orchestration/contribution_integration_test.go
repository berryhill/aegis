package orchestration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/execution"
	"reflect"

	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	"github.com/berryhill/aegis/internal/store"
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Synthetic Hermes protocol/runtime and authority fixture, NOT real-provider
// acceptance. Queue, sealed intake, adapter, Git, native build/checks and custody
// are production implementations.
func TestContributionQueueNativeRepairIntegration(t *testing.T) {
	for _, failClose := range []bool{false, true} {
		t.Run(fmt.Sprintf("tester_close_failure_%t", failClose), func(t *testing.T) { contributionQueueNativeRepairIntegration(t, failClose) })
	}
}
func contributionQueueNativeRepairIntegration(t *testing.T, failClose bool) {
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
	service, fixture, _, subject, _, _ := fleetServiceFixture(t)
	now := time.Now().UTC()
	service.now = func() time.Time { return time.Now().UTC() }
	auth := service.authority.(fleetAuthorityRepository)
	subject.AuthenticatedAt = now.Add(-time.Minute)
	subject.ExpiresAt = now.Add(time.Hour)
	auth.mandate.Subject = subject
	auth.mandate.IssuedAt = subject.AuthenticatedAt
	auth.mandate.ExpiresAt = subject.ExpiresAt
	auth.mandate.Hermes = core.HermesConfig{Model: "synthetic-protocol", Provider: "none"}
	auth.authority.IssuedAt = auth.mandate.IssuedAt
	auth.authority.ExpiresAt = auth.mandate.ExpiresAt
	auth.authority.Authority.Hermes = auth.mandate.Hermes
	auth.authority.Digest = core.AuthorityContextDigest(auth.authority)
	service.authority = auth
	service.authorityCommands = fleetAuthorityCommands{authority: auth.authority, admitted: true}
	authorityRef := digestRef(auth.authority.ID, auth.authority.Digest)
	repository, e := fleetbadger.Open(ctx, filepath.Join(root, "fleet-v1"))
	must(e)
	defer repository.Close()
	service.repository = repository
	agents := []registry.AgentRevision{}
	refs := []reference.RevisionRef{}
	roles := []contribution.RoleBinding{}
	fact := fleet.AuditFact{Event: core.AuditEvent{Type: "fixture.created", SubjectID: subject.ID, PrincipalID: subject.PrincipalID, Outcome: "succeeded", Reason: "synthetic fixture"}}
	for i, id := range []string{"agent-1", "builder", "tester"} {
		a := fixture.agent
		a.SchemaVersion = registry.AgentRevisionSchemaVersion
		a.AgentID = id
		a.Source = registry.FleetSource{FleetID: "fleet", Kind: "current-fleet", SourceID: id}
		a.Ownership.OwnerID = subject.PrincipalID
		if i > 0 {
			a.Charter = revisionRef(id, 1, contribution.Digest([]byte(id)))
		}
		a.Digest = ""
		a, e = registry.SealRevision(a)
		must(e)
		ref := revisionRef(id, 1, a.Digest)
		_, e = repository.RegisterAgent(ctx, registry.AgentRegistration{SchemaVersion: registry.AgentRegistrationSchemaVersion, AgentID: id, Source: a.Source, InitialRevision: ref}, a, fact)
		must(e)
		agents = append(agents, a)
		refs = append(refs, ref)
		roles = append(roles, contribution.RoleBinding{Agent: ref, Charter: a.Charter, Runtime: a.Runtime})
	}
	lr, lv, e := loop.NewContributionRevision("managed", 1, "", c, loop.ContributionProfile{Controller: refs[0], Builder: refs[1], Tester: refs[2], Repository: "fixture/controlled", RepositoryID: 7, Base: base, ReadAction: contribution.ReadIssueAction, Effect: "local-only", DeadlineSeconds: 300, Invariants: []string{"preserve trusted tests"}, Exclusions: []string{"no GitHub effects"}, Managed: &policy})
	if e != nil {
		t.Fatalf("revision: %v %+v", e, lv.Issues)
	}
	_, e = service.PublishLoop(ctx, PublishLoopRequest{Subject: subject, Authority: authorityRef, Publisher: refs[0], Publication: loop.PublishRequest{Revision: lr, Validation: lv, IdempotencyKey: "publish"}})
	must(e)
	lrRef := revisionRef(lr.LoopID, 1, lr.Digest)
	_, _, e = service.SetLoopLifecycle(ctx, SetLoopLifecycleRequest{Subject: subject, Authority: authorityRef, Publisher: refs[0], Loop: lrRef, State: loop.LifecycleActive, EventID: "activate"})
	must(e)
	issue, _ := contribution.ParseIssueURL("https://github.com/fixture/controlled/issues/1")
	td, _ := c.Digest()
	sealed, e := contribution.Seal(contribution.Contract{SchemaVersion: contribution.Schema, OwnerID: subject.PrincipalID, SessionID: "intake-session", Key: "integration", PayloadDigest: contribution.Payload(lrRef, issue.URL, ""), QueueItemID: "queue", SubmissionID: "submission", RunID: "run", Loop: lrRef, Controller: refs[0], Builder: refs[1], Tester: refs[2], Source: contribution.Source{URL: issue, RepositoryID: 7, IssueID: 9, Author: contribution.Author{ID: 1, Login: "fixture"}, State: "open", CreatedAt: now, UpdatedAt: now, ObservedAt: now, ContentDigest: contribution.Digest([]byte("synthetic issue")), Discussion: []contribution.Comment{}}, Outcome: c.Task, Acceptance: c.Acceptance, Invariants: []string{"preserve trusted tests"}, Exclusions: []string{"no GitHub effects"}, Repository: "fixture/controlled", Base: base, Worktree: c.Workspace, Effect: "local-only", Roles: roles, TemplateDigest: td, CandidateBudget: c.MaxPasses, AdmittedAt: now, Deadline: now.Add(300 * time.Second)})
	must(e)
	workspace := workspaceFor(t, subject.PrincipalID, agents[0])
	decision, e := service.PrepareLoopRun(ctx, SubmitLoopRequest{SubmitGraphRequest: SubmitGraphRequest{Subject: subject, Workspace: &workspace, Authority: workspace.Ref(), Contribution: &sealed, SubmissionID: "submission", IdempotencyKey: "submit", SnapshotID: "snapshot", QueueItemID: "queue", GraphRunID: "run", TransitionID: "queued", RejectionID: "rejected", MaxAttempts: 1}, Participant: refs[0], Loop: lrRef, NodeID: "contribution"})
	must(e)
	if decision.Accepted == nil {
		t.Fatalf("rejected %+v", decision.Rejection)
	}
	message := func(n int) string {
		p, _ := json.Marshal(map[string]any{"edits": []implementation.Edit{{Path: "value.go", Content: []byte("package main\nfunc Value() int{return 40}\n")}, {Path: "offset.go", Content: []byte(fmt.Sprintf("package main\nfunc Offset() int{return %d}\n", n))}}})
		return string(p)
	}
	script := fmt.Sprintf(`#!/bin/sh
n=0; [ ! -f '%s' ] || n=$(cat '%s'); n=$((n+1)); printf '%%s' "$n" > '%s'
sid="synthetic-runtime-$n"
printf '%%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready","payload":{}}}'
read tools
printf '%%s\n' '{"jsonrpc":"2.0","id":"aegis-tools","result":{"total":0,"sections":[]}}'
read create
printf '{"jsonrpc":"2.0","id":"create","result":{"session_id":"%%s"}}\n' "$sid"
read prompt
printf '%%s\n' '{"jsonrpc":"2.0","id":"prompt","result":{"accepted":true}}'
printf '{"jsonrpc":"2.0","method":"event","params":{"type":"message.start","session_id":"%%s","payload":{}}}\n' "$sid"
case "$n" in 1) text='%s';; 3) text='%s';; *) text='"synthetic inspection only"';; esac
printf '{"jsonrpc":"2.0","method":"event","params":{"type":"message.complete","session_id":"%%s","payload":{"status":"complete","text":%%s}}}\n' "$sid" "$text"
while read rest; do :; done
`, filepath.Join(root, "turns"), filepath.Join(root, "turns"), filepath.Join(root, "turns"), mustJSON(message(1)), mustJSON(message(2)))
	adapter := routedHermesTestAdapter(t, root, script)
	blobs, e := store.Open(filepath.Join(root, "blobs"))
	must(e)
	verifier, _ := evidence.NewBlobVerifier(blobs)
	worker, e := NewQueueWorker(repository, service, blobs, verifier, adapter, service.now)
	must(e)
	goBin, _ := exec.LookPath("go")
	goBin, _ = filepath.EvalSymlinks(goBin)
	must(worker.ConfigureImplementation(config.Implementation{GoBinary: goBin, AuthorizedContracts: []string{lr.Digest}}, filepath.Join(root, "state"), adapter.hermes))
	var issued []ContributionRole
	closed := map[string]bool{}
	builderCalls := 0
	worker.SetContributionRoleResolver(func(_ context.Context, actor core.Subject, contract contribution.Contract, index int) (ContributionRole, error) {
		if actor.ID != subject.ID || contract.Digest != sealed.Digest {
			return ContributionRole{}, errors.New("wrong authenticated role request")
		}
		if index == 1 {
			builderCalls++
		}
		id := fmt.Sprintf("role-%d-%d", index, len(issued))
		r := routedRuntimeRequest("")
		r.Launch.OwnerID = subject.PrincipalID
		r.Launch.Mandate = auth.mandate
		r.Launch.Mandate.ID = id + "-mandate"
		r.Launch.Mandate.AgentID = agents[index].Charter.ID
		r.Launch.Mandate.CharterDigest = agents[index].Charter.Digest
		r.Launch.AuthorityContext = auth.authority
		r.Launch.AuthorityContext.ID = id
		r.Launch.AuthorityContext.AgentID = agents[index].Charter.ID
		r.Launch.AuthorityContext.CharterDigest = agents[index].Charter.Digest
		r.Launch.AuthorityContext.MandateID = r.Launch.Mandate.ID
		r.Launch.AuthorityContext.SessionID = id + "-session"
		r.Launch.AuthorityContext.Digest = core.AuthorityContextDigest(r.Launch.AuthorityContext)
		r.Launch.ParentDispatch.AuthorityContextID = id
		role := ContributionRole{Agent: agents[index], Launch: r.Launch, Admission: integrationRoleAdmission{closed: closed, id: id}, Close: func(context.Context) error {
			closed[id] = true
			if failClose && index == 2 {
				return errors.New("fixture tester close failed")
			}
			return nil
		}}
		issued = append(issued, role)
		return role, nil
	})
	worker.SetContributionFetch(func(context.Context, loop.ContributionExecutionPolicy) error {
		runGit("fetch", "--no-tags", repoPath, "main")
		return nil
	})
	_, _, e = worker.BindRuntime(ctx, BindQueueRuntimeRequest{Subject: subject, Workspace: &workspace, Authority: authorityRef, QueueItemID: "queue", BindingID: "binding", TransitionID: "bound"})
	must(e)
	result, e := worker.Process(ctx, WorkRequest{Subject: subject, Authority: authorityRef, QueueItemID: "queue", WorkerID: "worker", LoopExecutionID: "loop-execution", ClaimID: "claim", AttemptID: "attempt", ClaimTransitionID: "claimed", TerminalTransitionID: "terminal", DispositionID: "disposition", ArtifactID: "artifact", LeaseDuration: 5 * time.Minute})
	if failClose {
		if e == nil || !strings.Contains(e.Error(), "fixture tester close failed") {
			t.Fatalf("close failure lost: %v", e)
		}
		if builderCalls != 1 || len(issued) != 2 {
			t.Fatalf("resumed builder after failed tester close: builder=%d roles=%d", builderCalls, len(issued))
		}
		turns, readErr := os.ReadFile(filepath.Join(root, "turns"))
		if readErr != nil || string(turns) != "2" {
			t.Fatalf("new runtime turn after close failure: %s %v", turns, readErr)
		}
		record, readErr := worker.ContributionExecutionReadback("run")
		if readErr == nil && record.State == "candidate_accepted" {
			t.Fatal("close failure accepted candidate")
		}
		return
	}
	if !errors.Is(e, ErrContributionCandidatePending) {
		var failure *doerFailure
		if errors.As(e, &failure) {
			t.Fatalf("production contribution: %v cause=%+v", e, failure.cause)
		}
		if r, readErr := worker.ContributionExecutionReadback("run"); readErr == nil {
			if output, outputErr := repository.ImplementationStore().Get([]byte("implementation/blob/" + r.Baseline.OutputDigest)); outputErr == nil {
				t.Logf("baseline native output: %s", output)
			}
		}
		t.Fatalf("production contribution: %v", e)
	}
	record, e := worker.ContributionExecutionReadback("run")
	must(e)
	if record.State != "candidate_accepted" || len(record.Passes) != 2 || record.Baseline.Outcome != "failed" || record.Passes[0].Check.Outcome != "failed" || record.Passes[1].Check.Outcome != "passed" {
		t.Fatalf("missing RED/repair/GREEN %+v", record)
	}
	if builderCalls != 1 || len(issued) != 3 || record.Passes[0].Candidate.Builder.SessionID != record.Passes[1].Candidate.Builder.SessionID {
		t.Fatal("builder not retained or tester not fresh")
	}
	for i, p := range record.Passes {
		if p.Tester.SessionID != issued[i+1].Launch.AuthorityContext.SessionID || p.Tester.RuntimeSessionID != fmt.Sprintf("synthetic-runtime-%d", 2+i*2) {
			t.Fatalf("wrong exact live callback %+v", p.Tester)
		}
	}
	for _, r := range issued {
		d, e := r.Admission.CheckRuntimeAdmission(ctx, r.Launch, time.Now())
		if e == nil && d.Allowed {
			t.Fatal("role survived revocation")
		}
	}
	projection, e := repository.GetQueueProjection(ctx, "queue")
	must(e)
	if projection.Attempts != 1 || projection.State != queue.StateClaimed || result.Artifact != nil || len(result.Receipts) != 0 || result.Disposition.DispositionID != "" {
		t.Fatalf("candidate minted final disposition %+v %+v", projection, result)
	}
	candidate, e := worker.AcceptedContributionCandidate(ctx, subject, "queue", lr)
	must(e)
	if candidate.Digest != record.Passes[1].Candidate.Digest {
		t.Fatal("handoff drift")
	}

	// Synthetic controller gate only: not durable process-stop evidence.
	gate := func(context.Context, ContributionCandidate) error { return nil }
	call := func(actor core.Subject, g ContributionQuiescenceGate, action func(context.Context, ContributionCandidate) error) error {
		return worker.WithAcceptedContributionCandidate(ctx, actor, "queue", lr, g, action)
	}
	noop := func(context.Context, ContributionCandidate) error { return nil }
	t.Run("custody_gate_clone_and_budget", func(t *testing.T) {
		before, err := worker.ContributionExecutionReadback("run")
		must(err)
		beforeWire, err := json.Marshal(before)
		must(err)
		gateCalls := 0
		must(call(subject, func(_ context.Context, got ContributionCandidate) error {
			gateCalls++
			got.Digest = "mutated copy"
			return nil
		}, func(_ context.Context, got ContributionCandidate) error {
			if got.Digest != candidate.Digest {
				t.Fatal("gate mutated proof anchor")
			}
			got.Digest = "mutated action copy"
			return nil
		}))
		if gateCalls != 2 {
			t.Fatalf("pre/post gate calls: %d", gateCalls)
		}
		after, err := worker.ContributionExecutionReadback("run")
		must(err)
		afterWire, err := json.Marshal(after)
		must(err)
		if string(beforeWire) != string(afterWire) {
			t.Fatal("custody changed proof or cumulative budget")
		}
	})
	t.Run("custody_callback_source_deep_clone", func(t *testing.T) {
		before, err := worker.ContributionExecutionReadback("run")
		must(err)
		beforeWire, err := json.Marshal(before)
		must(err)
		if len(candidate.Source.Files) == 0 || len(candidate.Source.Files[0].Content) == 0 {
			t.Fatal("fixture needs a nonempty source file")
		}
		for _, target := range []string{"gate", "action"} {
			for _, mutation := range []string{"content_bytes", "slice_element_path"} {
				t.Run(target+"/"+mutation, func(t *testing.T) {
					assertClean := func(got ContributionCandidate) {
						t.Helper()
						if !reflect.DeepEqual(got, candidate) {
							t.Fatal("callback did not receive the original private candidate/source anchor")
						}
					}
					mutate := func(got ContributionCandidate) {
						if mutation == "content_bytes" {
							got.Source.Files[0].Content[0] ^= 0xff
						} else {
							got.Source.Files[0] = implementation.SourceFile{Path: "mutated-callback-path", Content: []byte("copy only")}
						}
					}
					gateCalls, actionCalls := 0, 0
					must(call(subject, func(_ context.Context, got ContributionCandidate) error {
						gateCalls++
						assertClean(got)
						if target == "gate" {
							mutate(got)
						}
						return nil
					}, func(_ context.Context, got ContributionCandidate) error {
						actionCalls++
						assertClean(got)
						if target == "action" {
							mutate(got)
						}
						return nil
					}))
					if gateCalls != 2 || actionCalls != 1 {
						t.Fatalf("callback counts: gates=%d action=%d", gateCalls, actionCalls)
					}
					after, err := worker.ContributionExecutionReadback("run")
					must(err)
					afterWire, err := json.Marshal(after)
					must(err)
					if !bytes.Equal(beforeWire, afterWire) {
						t.Fatal("callback mutated durable proof or cumulative budget")
					}
					reloaded, err := worker.AcceptedContributionCandidate(ctx, subject, "queue", lr)
					must(err)
					assertClean(reloaded)
					source, err := implementation.Inventory(ctx, record.Workspace)
					must(err)
					if !reflect.DeepEqual(source, candidate.Source) {
						t.Fatal("callback mutated actual source")
					}
				})
			}
		}
	})
	t.Run("custody_git_index_pre_post", func(t *testing.T) {
		workspaceGit := func(args ...string) string {
			t.Helper()
			b, err := exec.Command(git, append([]string{"-C", record.Workspace}, args...)...).CombinedOutput()
			if err != nil {
				t.Fatalf("fixture git %v: %s %v", args, b, err)
			}
			return strings.TrimSpace(string(b))
		}
		head := workspaceGit("rev-parse", "HEAD")
		indexPath := workspaceGit("rev-parse", "--path-format=absolute", "--git-path", "index")
		index, err := os.ReadFile(indexPath)
		must(err)
		indexInfo, err := os.Stat(indexPath)
		must(err)
		stage := workspaceGit("ls-files", "--stage", "-z")
		restore := func() {
			t.Helper()
			must(os.WriteFile(indexPath, index, indexInfo.Mode().Perm()))
			if workspaceGit("rev-parse", "HEAD") != head || workspaceGit("ls-files", "--stage", "-z") != stage {
				t.Fatal("fixture Git identity not restored exactly")
			}
			actual, err := os.ReadFile(indexPath)
			must(err)
			if !bytes.Equal(actual, index) {
				t.Fatal("fixture index bytes not restored exactly")
			}
			source, err := implementation.Inventory(ctx, record.Workspace)
			must(err)
			if !reflect.DeepEqual(source, candidate.Source) {
				t.Fatal("Git-only drift changed source bytes")
			}
		}
		defer restore()
		drift := func() {
			t.Helper()
			workspaceGit("update-index", "--force-remove", "value.go")
			if workspaceGit("ls-files", "--stage", "-z") == stage || workspaceGit("rev-parse", "HEAD") != head {
				t.Fatal("fixture did not introduce index-only drift")
			}
			source, err := implementation.Inventory(ctx, record.Workspace)
			must(err)
			if !reflect.DeepEqual(source, candidate.Source) {
				t.Fatal("index-only drift changed source bytes")
			}
		}
		drift()
		called := false
		err = call(subject, gate, func(context.Context, ContributionCandidate) error { called = true; return nil })
		if err == nil || err.Error() != "frozen Git identity changed" || called {
			t.Fatalf("pre-callback index drift: action=%t err=%v", called, err)
		}
		restore()
		err = call(subject, gate, func(context.Context, ContributionCandidate) error { called = true; drift(); return nil })
		if !called || err == nil || err.Error() != "frozen Git identity changed" {
			t.Fatalf("during-callback index drift: action=%t err=%v", called, err)
		}
		restore()
		must(call(subject, gate, noop))
	})
	t.Run("custody_post_gate_denial", func(t *testing.T) {
		denied := errors.New("post-action epoch revoked")
		calls := 0
		called := false
		err := call(subject, func(context.Context, ContributionCandidate) error {
			calls++
			if calls == 2 {
				return denied
			}
			return nil
		}, func(context.Context, ContributionCandidate) error { called = true; return nil })
		if !called || !errors.Is(err, denied) {
			t.Fatalf("post gate ignored: %v", err)
		}
	})
	t.Run("custody_missing_quiescence", func(t *testing.T) {
		if err := call(subject, nil, noop); !errors.Is(err, implementation.ErrContributionQuiescenceUnavailable) {
			t.Fatalf("missing gate: %v", err)
		}
	})
	t.Run("custody_lock_and_callback_error", func(t *testing.T) {
		sentinel := errors.New("callback failed")
		called := false
		err := call(subject, gate, func(_ context.Context, got ContributionCandidate) error {
			called = true
			if got.Digest != candidate.Digest {
				t.Fatal("wrong candidate")
			}
			result := make(chan error, 1)
			go func() {
				f, err := os.Open(record.Workspace)
				if err != nil {
					result <- err
					return
				}
				defer f.Close()
				err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
				if err == nil {
					unix.Flock(int(f.Fd()), unix.LOCK_UN)
				}
				result <- err
			}()
			if err := <-result; !errors.Is(err, unix.EWOULDBLOCK) {
				t.Fatalf("concurrent writer not excluded: %v", err)
			}
			return sentinel
		})
		if !called || !errors.Is(err, sentinel) {
			t.Fatalf("callback error lost: %v", err)
		}
		f, err := os.Open(record.Workspace)
		must(err)
		defer f.Close()
		must(unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB))
		must(unix.Flock(int(f.Fd()), unix.LOCK_UN))
	})
	t.Run("custody_pre_post_authority", func(t *testing.T) {
		expired := subject
		expired.ExpiresAt = time.Now().Add(-time.Hour)
		called := false
		if err := call(expired, gate, func(context.Context, ContributionCandidate) error { called = true; return nil }); err == nil || called {
			t.Fatalf("expired admitted: %v", err)
		}
		saved := service.authority
		defer func() { service.authority = saved }()
		revoke := func() { a := auth; a.authority.ExpiresAt = time.Now().Add(-time.Hour); service.authority = a }
		revoke()
		if err := call(subject, gate, func(context.Context, ContributionCandidate) error { called = true; return nil }); err == nil || called {
			t.Fatalf("revoked admitted: %v", err)
		}
		service.authority = saved
		if err := call(subject, gate, func(context.Context, ContributionCandidate) error { revoke(); return nil }); err == nil {
			t.Fatal("post-callback revocation accepted")
		}
	})
	t.Run("custody_source_pre_post", func(t *testing.T) {
		path := filepath.Join(record.Workspace, "custody-drift")
		must(os.WriteFile(path, []byte("drift"), 0600))
		called := false
		if err := call(subject, gate, func(context.Context, ContributionCandidate) error { called = true; return nil }); err == nil || called {
			t.Fatalf("pre-drift admitted: %v", err)
		}
		must(os.Remove(path))
		if err := call(subject, gate, func(context.Context, ContributionCandidate) error { return os.WriteFile(path, []byte("drift"), 0600) }); err == nil {
			t.Fatal("post-drift accepted")
		}
		must(os.Remove(path))
	})
	t.Run("custody_evidence_pre_post", func(t *testing.T) {
		db := repository.ImplementationStore()
		key := []byte("contribution/candidate/" + candidate.Digest)
		wire, err := db.Get(key)
		must(err)
		defer db.Put(key, wire)
		must(db.Put(key, []byte("corrupt")))
		called := false
		if err := call(subject, gate, func(context.Context, ContributionCandidate) error { called = true; return nil }); err == nil || called {
			t.Fatalf("pre-evidence drift admitted: %v", err)
		}
		must(db.Put(key, wire))
		if err := call(subject, gate, func(context.Context, ContributionCandidate) error { return db.Put(key, []byte("corrupt")) }); err == nil {
			t.Fatal("post-evidence drift accepted")
		}
	})
	t.Run("custody_gate_denial", func(t *testing.T) {
		denied := errors.New("unknown exact stop epoch")
		called := false
		err := call(subject, func(context.Context, ContributionCandidate) error { return denied }, func(context.Context, ContributionCandidate) error { called = true; return nil })
		if called || !errors.Is(err, denied) {
			t.Fatalf("gate bypassed: %v", err)
		}
	})
	stale := lr
	stale.Digest = contribution.Digest([]byte("stale"))
	if _, e = worker.AcceptedContributionCandidate(ctx, subject, "queue", stale); e == nil {
		t.Fatal("stale sealed handoff accepted")
	}
	goBinary := worker.implementation.config.GoBinary
	worker.implementation.config.GoBinary = ""
	if _, e = worker.AcceptedContributionCandidate(ctx, subject, "queue", lr); !errors.Is(e, ErrWorkerDenied) {
		t.Fatalf("missing toolchain: %v", e)
	}
	worker.implementation.config.GoBinary = goBinary
	expired := subject
	expired.ExpiresAt = time.Now().Add(-time.Hour)
	if _, e = worker.AcceptedContributionCandidate(ctx, expired, "queue", lr); !errors.Is(e, ErrWorkerDenied) {
		t.Fatalf("stale authority: %v", e)
	}
	host := worker.implementation.config.AuthorizedContracts
	worker.implementation.config.AuthorizedContracts = nil
	if _, e = worker.AcceptedContributionCandidate(ctx, subject, "queue", lr); !errors.Is(e, ErrWorkerDenied) {
		t.Fatalf("stale host approval: %v", e)
	}
	worker.implementation.config.AuthorizedContracts = host
	must(os.WriteFile(filepath.Join(record.Workspace, "post-review-drift"), []byte("drift"), 0600))
	if _, e = worker.AcceptedContributionCandidate(ctx, subject, "queue", lr); e == nil {
		t.Fatal("source drift exported")
	}
	t.Log("synthetic runtime protocol qualification; native multi-file repair and executable checks passed; not real-provider success")
}
func mustJSON(s string) string { b, _ := json.Marshal(s); return string(b) }

type integrationRoleAdmission struct {
	closed map[string]bool
	id     string
}

func (a integrationRoleAdmission) CheckRuntimeAdmission(_ context.Context, l execution.LaunchContract, at time.Time) (execution.AdmissionDecision, error) {
	return execution.AdmissionDecision{AuthorityContextID: l.AuthorityContext.ID, AuthorityContextDigest: l.AuthorityContext.Digest, CheckedAt: at, Allowed: !a.closed[a.id]}, nil
}
