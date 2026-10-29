package implementation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/loop"
)

func roleIdentity(s string) ContributionRoleIdentity {
	return ContributionRoleIdentity{s, s, s, s, s, s, s, s}
}

// Real native compilation, executable invocation, immutable tests and Git linked
// worktree; deterministic role proposals/transport only, NOT real-provider proof.
func TestManagedContributionRepairAndCustody(t *testing.T) {
	ctx := context.Background()
	kernel, c := fixture(t)
	repo := c.Workspace
	files := map[string]string{
		"go.mod":    "module synthetic\n\ngo 1.24\n",
		"main.go":   "package main\nimport (\"fmt\";\"os\")\nfunc main(){if len(os.Args)>1{os.Exit(2)};fmt.Print(Value())}\n",
		"value.go":  "package main\nfunc Value() int{return 0}\n",
		"offset.go": "package main\nfunc Offset() int{return 0}\n",
		"main_test.go": `package main
import("testing";"os";"os/exec")
func TestBehavior(t *testing.T){if Value()+Offset()!=42 {t.Fatal("wrong multi-file result")}}
func TestSecurity(t *testing.T){if Offset()<0 {t.Fatal("negative offset")}}
func TestDenial(t *testing.T){p:=os.Getenv("AEGIS_CONTRIBUTION_CANDIDATE");if p==""{return};e:=exec.Command(p,"forbidden").Run();x,ok:=e.(*exec.ExitError);if !ok||x.ExitCode()!=2{t.Fatalf("required argument denial: %v",e)}}
func TestLaunch(t *testing.T){p:=os.Getenv("AEGIS_CONTRIBUTION_CANDIDATE");if p==""{return};b,e:=exec.Command(p).Output();if e!=nil||string(b)!="40" {t.Fatalf("executable: %s %v",b,e)}}
`,
		".gitignore": "ignored\n",
	}
	os.Remove(filepath.Join(repo, "value_test.go"))
	files["README.md"] = "Controlled fixture prints 40; arguments are rejected with exit 2.\n"
	files["SECURITY.md"] = "No credentials or network; unexpected arguments are denied.\n"
	files["generated.txt"] = "fixture output contract: 40\n"
	for p, b := range files {
		if err := os.WriteFile(filepath.Join(repo, p), []byte(b), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git, _ := exec.LookPath("git")
	git, _ = filepath.EvalSymlinks(git)
	runGit := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(git, append([]string{"-C", repo}, args...)...)
		b, e := cmd.CombinedOutput()
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
	c.Workspace = filepath.Join(t.TempDir(), "owned")
	c.WritableFiles = []string{"value.go", "offset.go"}
	c.Policy.RequiredTests = []loop.RequiredGoTest{{Package: "synthetic", Name: "TestBehavior"}, {Package: "synthetic", Name: "TestSecurity"}, {Package: "synthetic", Name: "TestLaunch"}, {Package: "synthetic", Name: "TestDenial"}}
	c.RepositoryContext = &loop.RepositoryContext{TaskDigest: digest([]byte(c.Task)), MaxFiles: 8, MaxFileBytes: 4096, MaxTotalBytes: 16384}
	for _, p := range []string{"go.mod", "main.go", "value.go", "offset.go", "main_test.go"} {
		r := "source"
		if p == "go.mod" {
			r = "checker_input"
		}
		if p == "value.go" || p == "offset.go" {
			r = "source"
		}
		if p == "main_test.go" {
			r = "trusted_test"
		}
		c.RepositoryContext.Files = append(c.RepositoryContext.Files, loop.RepositoryContextFile{Path: p, Role: r, Bytes: int64(len(files[p])), Digest: digest([]byte(files[p]))})
	}
	c.RepositoryContext.SourceDigest = RepositorySourceDigest(c.RepositoryContext)
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	policy := loop.ContributionExecutionPolicy{Kind: "go-stdlib-behavior.v1", RepositoryPath: repo, GitBinary: git, BaseBranch: "main", BuildPackage: ".", Criteria: []loop.ContributionCriterion{{ID: "behavior", Kind: "behavior", Test: c.Policy.RequiredTests[0]}, {ID: "security", Kind: "security", Test: c.Policy.RequiredTests[1]}, {ID: "launch", Kind: "launch", Test: c.Policy.RequiredTests[2]}, {ID: "denial", Kind: "denial", Test: c.Policy.RequiredTests[3]}}}
	for _, asset := range []struct{ path, scope string }{{"README.md", "launch"}, {"SECURITY.md", "security"}, {"generated.txt", "launch"}, {"main.go", "security"}, {"main_test.go", "security"}} {
		d := digest([]byte(files[asset.path]))
		policy.ReviewAssets = append(policy.ReviewAssets, loop.ContributionReviewAsset{ID: asset.path, Path: asset.path, Scope: asset.scope, BaselineDigest: d, ApprovedDigest: d})
	}
	e := &ManagedContribution{DB: kernel.DB, GoBinary: kernel.GoBinary, StateRoot: t.TempDir(), Repository: "fixture/controlled", Admit: kernel.Admit}
	e.Fetch = func(context.Context, loop.ContributionExecutionPolicy) error {
		runGit("fetch", "--no-tags", repo, "main")
		return nil
	}
	calls, testers := 0, 0
	e.Build = func(_ context.Context, r Request) ([]Edit, ContributionRoleIdentity, error) {
		calls++
		if len(r.PreviousCheck) == 0 {
			t.Fatal("missing retained real failure")
		}
		if calls == 1 {
			if _, err := e.Run(ctx, "run", "contract", "other-attempt", base, c, policy); err == nil {
				t.Fatal("concurrent reservation accepted")
			}
		}
		n := 1
		if calls == 2 {
			n = 2
		}
		return []Edit{{Path: "value.go", Content: []byte("package main\nfunc Value() int{return 40}\n")}, {Path: "offset.go", Content: []byte(fmt.Sprintf("package main\nfunc Offset() int{return %d}\n", n))}}, roleIdentity("builder"), nil
	}
	e.Test = func(checkCtx context.Context, v ContributionCandidate, execute func(context.Context, ContributionRoleIdentity) error) (ContributionRoleIdentity, error) {
		testers++
		if v.Executable == "" || v.Source.Digest == "" {
			t.Fatal("unfrozen tester")
		}
		identity := roleIdentity(fmt.Sprintf("tester-%d", testers))
		return identity, execute(checkCtx, identity)
	}
	r, err := e.Run(ctx, "run", "contract", "attempt", base, c, policy)
	if err != nil || r.State != "candidate_accepted" || calls != 2 || testers != 2 {
		t.Fatalf("state=%s calls=%d testers=%d err=%v", r.State, calls, testers, err)
	}
	if r.Baseline.Outcome != "failed" || r.Passes[0].Check.Outcome != "failed" || r.Passes[1].Check.Outcome != "passed" {
		t.Fatal("missing RED/wrong patch/GREEN chain")
	}
	if _, err = e.Read("run"); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Run(ctx, "run", "contract", "restart", base, c, policy); err == nil {
		t.Fatal("restart reset budget")
	}
	v := r.Passes[1].Candidate
	t.Run("independent-missing-baseline-rejected", func(t *testing.T) {
		key := contributionKey("run")
		original, err := e.DB.Get(key)
		if err != nil {
			t.Fatal(err)
		}
		defer e.DB.Put(key, original)
		copy := r
		copy.Baseline = ContributionCheck{Outcome: "failed"}
		wire, _ := json.Marshal(copy)
		if err := e.DB.Put(key, wire); err != nil {
			t.Fatal(err)
		}
		if _, err := e.ReloadAcceptedEvidence(ctx, "run", "contract", "attempt", v.Digest, policy); err == nil {
			t.Fatal("native export accepted missing immutable RED baseline")
		}
	})
	// The contribution verifier reloads the actual producer records and native
	for _, mutation := range []string{"immutable-missing", "output-missing", "wrong-tests", "fake-red", "baseline-criteria", "baseline-outcome", "pass-outcome"} {
		t.Run("baseline-proof-"+mutation, func(t *testing.T) {
			key := contributionKey("run")
			original, _ := e.DB.Get(key)
			baselineKey := []byte("contribution/baseline/" + digest([]byte("run")))
			immutable, _ := e.DB.Get(baselineKey)
			defer e.DB.Put(key, original)
			defer e.DB.Put(baselineKey, immutable)
			var copy ContributionRecord
			if err := json.Unmarshal(original, &copy); err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "immutable-missing":
				if err := e.DB.Put(baselineKey, nil); err != nil {
					t.Fatal(err)
				}
			case "output-missing":
				copy.Baseline.OutputDigest = digest([]byte("absent"))
			case "wrong-tests":
				copy.Baseline.RequiredTests = copy.Baseline.RequiredTests[:1]
			case "fake-red":
				copy.Baseline.OutputDigest = copy.Passes[1].Check.OutputDigest
			case "baseline-criteria":
				copy.Baseline.Criteria = copy.Passes[1].Check.Criteria
			case "baseline-outcome":
				copy.Baseline.Outcome = "passed"
			case "pass-outcome":
				copy.Passes[0].Check.Outcome = "unknown"
				passKey := []byte(fmt.Sprintf("contribution/acceptance/%s/1", digest([]byte("run"))))
				old, _ := e.DB.Get(passKey)
				defer e.DB.Put(passKey, old)
				wire, _ := json.Marshal(copy.Passes[0])
				e.DB.Put(passKey, wire)
			}
			if mutation != "immutable-missing" {
				wire, _ := json.Marshal(copy.Baseline)
				e.DB.Put(baselineKey, wire)
			}
			wire, _ := json.Marshal(copy)
			e.DB.Put(key, wire)
			if _, err := e.ReloadAcceptedEvidence(ctx, "run", "contract", "attempt", v.Digest, policy); err == nil {
				t.Fatal("invalid baseline/pass proof accepted")
			}
		})
	}
	// observations, not a caller's PASS report or a copied candidate digest.
	exported, exportErr := e.ReloadAcceptedEvidence(ctx, "run", "contract", "attempt", v.Digest, policy)
	if exportErr != nil || len(exported) == 0 {
		t.Fatalf("native verifier export: %v", exportErr)
	}
	for _, mismatch := range []string{"run", "contract", "attempt", "candidate", "policy", "cancelled", "producer-json"} {
		t.Run("verification-"+mismatch, func(t *testing.T) {
			run, contract, attempt, candidate, p := "run", "contract", "attempt", v.Digest, policy
			verifyCtx := ctx
			switch mismatch {
			case "run":
				run = "other-run"
			case "contract":
				contract = "other-contract"
			case "attempt":
				attempt = "other-attempt"
			case "candidate":
				candidate = r.Passes[0].Candidate.Digest
			case "policy":
				p.BuildPackage = "./other"
			case "cancelled":
				var cancel context.CancelFunc
				verifyCtx, cancel = context.WithCancel(ctx)
				cancel()
			case "producer-json":
				key := contributionKey("run")
				original, _ := e.DB.Get(key)
				// Read's decoder accepts whitespace; the verifier requires exact
				// canonical producer bytes, not merely equivalent parsed values.
				if err := e.DB.Put(key, append([]byte(" "), original...)); err != nil {
					t.Fatal(err)
				}
				defer e.DB.Put(key, original)
			}
			if _, err := e.ReloadAcceptedEvidence(verifyCtx, run, contract, attempt, candidate, p); err == nil {
				t.Fatal("mismatched native verification accepted")
			}
		})
	}
	if v.CandidateCommit == base || v.CandidateTree == v.HeadTree {
		t.Fatal("candidate Git identity represents base, not repaired bytes")
	}
	cmd := exec.Command(git, "-C", c.Workspace, "show", v.CandidateCommit+":offset.go")
	gitBytes, gitErr := cmd.Output()
	if gitErr != nil || !strings.Contains(string(gitBytes), "return 2") {
		t.Fatalf("candidate commit does not contain tested bytes: %s %v", gitBytes, gitErr)
	}
	if _, err := e.AcceptedCandidate("run", "contract", policy); err != nil {
		t.Fatal(err)
	}
	for _, missing := range []string{"go", "admission"} {
		copy := *e
		if missing == "go" {
			copy.GoBinary = ""
		} else {
			copy.Admit = nil
		}
		if _, err := copy.AcceptedCandidate("run", "contract", policy); !errors.Is(err, ErrContributionHandoffDenied) {
			t.Fatalf("missing %s: %v", missing, err)
		}
	}
	stale := policy
	stale.BuildPackage = "./other"
	if _, err := e.AcceptedCandidate("run", "contract", stale); err == nil {
		t.Fatal("stale export policy accepted")
	}
	if _, err := e.AcceptedCandidate("absent", "contract", policy); err == nil {
		t.Fatal("absent export accepted")
	}
	if len(r.Passes[0].Check.Criteria) != 4 || r.Passes[0].Check.Criteria[0].Status != CriterionFailed {
		t.Fatal("typed failure packet missing")
	}
	t.Logf("candidate source=%s executable=%s head-anchor=%s", v.Source.Digest, v.Executable, v.HeadCommit)
	for _, mode := range []string{"terminal-last", "failed-action", "failed-post-gate", "source-drift", "generic-unchanged"} {
		t.Run("terminal-lifecycle-"+mode, func(t *testing.T) {
			originalAdmit := e.Admit
			defer func() { e.Admit = originalAdmit }()
			terminalCalled, actionCalled, gates := false, false, 0
			e.Admit = func(ctx context.Context, effect string) error {
				if terminalCalled {
					t.Error("admission/effect after terminal commit")
					return errors.New("after terminal")
				}
				return originalAdmit(ctx, effect)
			}
			path := filepath.Join(c.Workspace, "value.go")
			original, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			defer os.WriteFile(path, original, 0600)
			gate := func(context.Context, ContributionCandidate) error {
				gates++
				if mode == "failed-post-gate" && gates == 2 {
					return errors.New("post gate denied")
				}
				return nil
			}
			action := func(context.Context, ContributionCandidate) error {
				actionCalled = true
				if mode == "failed-action" {
					return errors.New("action failed")
				}
				if mode == "source-drift" {
					return os.WriteFile(path, []byte("changed after acceptance"), 0600)
				}
				return nil
			}
			commit := func(context.Context, ContributionCandidate) error {
				if !actionCalled || gates != 2 {
					t.Fatal("commit before action/post gate")
				}
				unlock, err := lockWorkspace(c.Workspace)
				if err == nil {
					unlock()
					t.Fatal("workspace lock released before terminal commit")
				}
				terminalCalled = true
				return nil
			}
			var terminalErr error
			if mode == "generic-unchanged" {
				terminalErr = e.WithAcceptedCandidate(ctx, "run", "contract", policy, gate, action)
			} else {
				terminalErr = e.WithAcceptedCandidateFinalization(ctx, "run", "contract", policy, gate, action, commit)
			}
			wantCommit := mode == "terminal-last"
			wantError := mode == "failed-action" || mode == "failed-post-gate" || mode == "source-drift"
			if terminalCalled != wantCommit || (terminalErr != nil) != wantError || gates != 2 {
				t.Fatalf("mode=%s commit=%t gates=%d err=%v", mode, terminalCalled, gates, terminalErr)
			}
		})
	}
	for _, kind := range []string{"source", "new", "ignored", "mode", "index", "toolchain", "executable", "README.md", "generated.txt", "review-ledger"} {
		t.Run(kind, func(t *testing.T) {
			p := filepath.Join(c.Workspace, "value.go")
			old, _ := os.ReadFile(p)
			switch kind {
			case "README.md", "generated.txt":
				p = filepath.Join(c.Workspace, kind)
				b, _ := os.ReadFile(p)
				os.WriteFile(p, []byte("post-review mutation"), 0600)
				defer os.WriteFile(p, b, 0600)
				if _, err := e.AcceptedCandidate("run", "contract", policy); err == nil {
					t.Fatal("post-review export accepted")
				}
			case "review-ledger":
				key := []byte("contribution/review/" + r.Passes[1].Check.Review)
				b, _ := e.DB.Get(key)
				e.DB.Put(key, []byte("builder PASS"))
				defer e.DB.Put(key, b)
				if _, err := e.AcceptedCandidate("run", "contract", policy); err == nil {
					t.Fatal("forged review export accepted")
				}
				return
			case "source":
				os.WriteFile(p, []byte("changed"), 0600)
				defer os.WriteFile(p, old, 0600)
			case "new", "ignored":
				p = filepath.Join(c.Workspace, kind)
				os.WriteFile(p, []byte("new"), 0600)
				defer os.Remove(p)
			case "mode":
				os.Chmod(p, 0700)
				defer os.Chmod(p, 0600)
			case "index":
				cmd := exec.Command(git, "-C", c.Workspace, "add", "value.go")
				if b, e := cmd.CombinedOutput(); e != nil {
					t.Fatalf("%s %v", b, e)
				}
				defer exec.Command(git, "-C", c.Workspace, "reset", "HEAD", "value.go").Run()
			case "toolchain":
				oldGo := e.GoBinary
				e.GoBinary = filepath.Join(t.TempDir(), "go")
				os.WriteFile(e.GoBinary, []byte("substitute"), 0700)
				defer func() { e.GoBinary = oldGo }()
			case "executable":
				b, _ := e.DB.Get(blobKey(v.Executable))
				e.DB.Put(blobKey(v.Executable), []byte("substitute"))
				defer e.DB.Put(blobKey(v.Executable), b)
			}
			if err := e.revalidate(ctx, c, policy, v); err == nil {
				t.Fatal("drift accepted")
			}
		})
	}
	if err = e.revalidate(ctx, c, policy, v); err != nil {
		t.Fatal(err)
	}
	t.Run("toolchain_substitution_during_readonly_observations", func(t *testing.T) {
		oldGo, oldAdmit := e.GoBinary, e.Admit
		defer func() { e.GoBinary, e.Admit = oldGo, oldAdmit }()
		reads := 0
		e.Admit = func(ctx context.Context, effect string) error {
			if err := oldAdmit(ctx, effect); err != nil {
				return err
			}
			if effect == "git-read-or-worktree" {
				reads++
				if reads == 3 {
					e.GoBinary = filepath.Join(t.TempDir(), "missing", "bin", "go")
				}
			}
			return nil
		}
		if err := e.revalidate(ctx, c, policy, v); err == nil || reads != 3 {
			t.Fatalf("late toolchain substitution accepted or final observation not reached: reads=%d err=%v", reads, err)
		}
	})
	e.DB.Put(blobKey(r.Passes[0].Check.OutputDigest), []byte("forged"))
	if _, err = e.Read("run"); err == nil {
		t.Fatal("corrupt prior failure accepted")
	}
}
