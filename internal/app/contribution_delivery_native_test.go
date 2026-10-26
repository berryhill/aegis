package app

import (
	"context"
	"fmt"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/loop"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func deliveryRoleIdentity(s string) implementation.ContributionRoleIdentity {
	return implementation.ContributionRoleIdentity{AgentDigest: s, CharterDigest: s, AuthorityID: s, AuthorityDigest: s, SessionID: s, MandateID: s, RuntimeSessionID: s, TurnID: s}
}
func deliveryNativeFixture(t *testing.T) (*implementation.ManagedContribution, loop.VerifiedImplementation) {
	db, e := fleetbadger.Open(context.Background(), filepath.Join(t.TempDir(), "fleet-v1"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	goBin, e := exec.LookPath("go")
	if e != nil {
		t.Fatal(e)
	}
	goBin, e = filepath.EvalSymlinks(goBin)
	if e != nil {
		t.Fatal(e)
	}
	c := loop.ImplementationDraft("Return 42", "Real native journeys pass")
	c.Workspace = t.TempDir()
	c.Policy.TimeoutSeconds = 120
	return &implementation.ManagedContribution{DB: db.ImplementationStore(), GoBinary: goBin, Admit: func(context.Context, string) error { return nil }}, c
}

// Actual #376 native accepted-candidate custody and projection. Role transport is
// synthetic; no real provider/native #377 authority acceptance is claimed.
func TestContributionDeliveryNativeAcceptedProjection(t *testing.T) {
	ctx := context.Background()
	kernel, c := deliveryNativeFixture(t)
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
	c.RepositoryContext = &loop.RepositoryContext{TaskDigest: contribution.Digest([]byte(c.Task)), MaxFiles: 8, MaxFileBytes: 4096, MaxTotalBytes: 16384}
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
		c.RepositoryContext.Files = append(c.RepositoryContext.Files, loop.RepositoryContextFile{Path: p, Role: r, Bytes: int64(len(files[p])), Digest: contribution.Digest([]byte(files[p]))})
	}
	c.RepositoryContext.SourceDigest = implementation.RepositorySourceDigest(c.RepositoryContext)
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	policy := loop.ContributionExecutionPolicy{Kind: "go-stdlib-behavior.v1", RepositoryPath: repo, GitBinary: git, BaseBranch: "main", BuildPackage: ".", Criteria: []loop.ContributionCriterion{{ID: "behavior", Kind: "behavior", Test: c.Policy.RequiredTests[0]}, {ID: "security", Kind: "security", Test: c.Policy.RequiredTests[1]}, {ID: "launch", Kind: "launch", Test: c.Policy.RequiredTests[2]}, {ID: "denial", Kind: "denial", Test: c.Policy.RequiredTests[3]}}}
	for _, asset := range []struct{ path, scope string }{{"README.md", "launch"}, {"SECURITY.md", "security"}, {"generated.txt", "launch"}, {"main.go", "security"}, {"main_test.go", "security"}} {
		d := contribution.Digest([]byte(files[asset.path]))
		policy.ReviewAssets = append(policy.ReviewAssets, loop.ContributionReviewAsset{ID: asset.path, Path: asset.path, Scope: asset.scope, BaselineDigest: d, ApprovedDigest: d})
	}
	e := &implementation.ManagedContribution{DB: kernel.DB, GoBinary: kernel.GoBinary, StateRoot: t.TempDir(), Repository: "fixture/controlled", Admit: kernel.Admit}
	e.Fetch = func(context.Context, loop.ContributionExecutionPolicy) error {
		runGit("fetch", "--no-tags", repo, "main")
		return nil
	}
	calls, testers := 0, 0
	e.Build = func(_ context.Context, r implementation.Request) ([]implementation.Edit, implementation.ContributionRoleIdentity, error) {
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
		return []implementation.Edit{{Path: "value.go", Content: []byte("package main\nfunc Value() int{return 40}\n")}, {Path: "offset.go", Content: []byte(fmt.Sprintf("package main\nfunc Offset() int{return %d}\n", n))}}, deliveryRoleIdentity("builder"), nil
	}
	e.Test = func(checkCtx context.Context, v implementation.ContributionCandidate, execute func(context.Context, implementation.ContributionRoleIdentity) error) (implementation.ContributionRoleIdentity, error) {
		testers++
		if v.Executable == "" || v.Source.Digest == "" {
			t.Fatal("unfrozen tester")
		}
		identity := deliveryRoleIdentity(fmt.Sprintf("tester-%d", testers))
		return identity, execute(checkCtx, identity)
	}
	r, err := e.Run(ctx, "run", "contract", "attempt", base, c, policy)
	if err != nil || r.State != "candidate_accepted" || calls != 2 || testers != 2 {
		t.Fatalf("state=%s calls=%d testers=%d err=%v", r.State, calls, testers, err)
	}

	v := r.Passes[len(r.Passes)-1].Candidate
	g := &delivery.Git{Binary: git, Directory: c.Workspace}
	spec := delivery.CommitSpec{Parent: base, Tree: v.CandidateTree, Branch: "delivery/reviewed", Author: delivery.Identity{Name: "Reviewed Author", Email: "author@example.invalid"}, Committer: delivery.Identity{Name: "Reviewed Controller", Email: "controller@example.invalid"}, Timestamp: "2026-10-25T00:00:00Z", Message: "Implement reviewed behavior\n"}
	gate := func(context.Context, implementation.ContributionCandidate) error { return nil } // fixture only
	called := 0
	err = e.WithAcceptedCandidate(ctx, "run", "contract", policy, gate, func(ctx context.Context, actual implementation.ContributionCandidate) error {
		called++
		got, objects, x := g.ProjectAccepted(ctx, actual, spec, []string{"value.go", "offset.go"})
		if x != nil {
			t.Fatal(x)
		}
		if got.Tree != actual.CandidateTree || got.Parent != actual.Base || got.Commit == actual.CandidateCommit || len(objects) == 0 {
			t.Fatal("publication identity not exact")
		}
		for _, kind := range []string{"tree", "inventory", "scope", "parent"} {
			bad := actual
			bad.Source.Files = append([]implementation.SourceFile(nil), actual.Source.Files...)
			input := spec
			scope := []string{"value.go", "offset.go"}
			switch kind {
			case "tree":
				bad.CandidateTree = strings.Repeat("a", 40)
			case "inventory":
				bad.Source.Files = bad.Source.Files[1:]
			case "scope":
				scope = []string{"value.go"}
			case "parent":
				input.Parent = strings.Repeat("b", 40)
			}
			if _, _, x := g.ProjectAccepted(ctx, bad, input, scope); x == nil {
				t.Fatalf("%s accepted", kind)
			}
		}
		return nil
	})
	if err != nil || called != 1 {
		t.Fatalf("custody callback %d %v", called, err)
	}
	if err = e.WithAcceptedCandidate(ctx, "run", "contract", policy, nil, func(context.Context, implementation.ContributionCandidate) error { called++; return nil }); err == nil || called != 1 {
		t.Fatal("nil gate accepted")
	}
	old, err := os.ReadFile(filepath.Join(c.Workspace, "value.go"))
	if err != nil {
		t.Fatal(err)
	}
	err = e.WithAcceptedCandidate(ctx, "run", "contract", policy, gate, func(context.Context, implementation.ContributionCandidate) error {
		return os.WriteFile(filepath.Join(c.Workspace, "value.go"), []byte("drift"), 0600)
	})
	if err == nil {
		t.Fatal("post-callback kernel revalidation missed drift")
	}
	if err = os.WriteFile(filepath.Join(c.Workspace, "value.go"), old, 0600); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatal("callback retained after return")
	}
}
