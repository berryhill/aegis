package implementation

import (
	"context"
	"github.com/berryhill/aegis/internal/loop"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Controlled real Git integration, not canonical delivery or acceptance proof.
func TestContributionNativeAdvancingBaseIntegration(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(git, append([]string{"-C", repo, "-c", "core.hooksPath=/dev/null", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid"}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		b, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("git %v: %s %v", args, b, e)
		}
		return strings.TrimSpace(string(b))
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, path), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-b", "main")
	write("value.go", "package fixture\nconst Value = 0\n")
	write("trusted_test.go", "package fixture\n")
	run("add", ".")
	run("commit", "-m", "original")
	base := run("rev-parse", "HEAD")
	run("remote", "add", "origin", "https://github.com/fixture/controlled.git")
	c := loop.VerifiedImplementation{Workspace: repo, WritableFiles: []string{"value.go"}, RepositoryContext: &loop.RepositoryContext{Files: []loop.RepositoryContextFile{{Path: "trusted_test.go", Role: "trusted_test", Digest: digest([]byte("package fixture\n"))}}}}
	p := loop.ContributionExecutionPolicy{RepositoryPath: repo, GitBinary: git}
	e := &ManagedContribution{StateRoot: t.TempDir(), Repository: "fixture/controlled", Admit: func(context.Context, string) error { return nil }}
	write("value.go", "package fixture\nconst Value = 42\n")
	source, err := Inventory(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	commit, tree, err := e.freezeGit(ctx, p, repo, base, source)
	if err != nil {
		t.Fatal(err)
	}
	prior := ContributionCandidate{Base: base, CandidateCommit: commit, CandidateTree: tree, Source: source, Digest: digest([]byte("previous accepted candidate"))}
	write("value.go", "package fixture\nconst Value = 0\n")
	write("upstream.go", "package fixture\nconst Upstream = 7\n")
	run("add", ".")
	run("commit", "-m", "upstream")
	current := run("rev-parse", "HEAD")
	before := run("status", "--porcelain=v1")
	integrated, err := e.integrateAdvancingBase(ctx, c, p, prior, current)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range integrated.Source.Files {
		got[f.Path] = string(f.Content)
	}
	if got["value.go"] != "package fixture\nconst Value = 42\n" || got["upstream.go"] != "package fixture\nconst Upstream = 7\n" || integrated.OriginalBase != base || integrated.CurrentBase != current || integrated.Predecessor != prior.Digest || integrated.Tree == tree {
		t.Fatal("integration lost original or upstream scope")
	}
	if run("rev-parse", "HEAD") != current || run("status", "--porcelain=v1") != before || prior.Base != base {
		t.Fatal("integration changed source/ref/original intent")
	}
	t.Run("same-file-conflict", func(t *testing.T) {
		write("value.go", "package fixture\nconst Value = 99\n")
		run("add", "value.go")
		run("commit", "-m", "conflict")
		if _, err := e.integrateAdvancingBase(ctx, c, p, prior, run("rev-parse", "HEAD")); err == nil {
			t.Fatal("conflict accepted")
		}
	})
	t.Run("trusted-upstream-edit", func(t *testing.T) {
		run("checkout", "-b", "trusted-change", current)
		write("trusted_test.go", "package fixture\n// changed trusted acceptance\n")
		run("add", "trusted_test.go")
		run("commit", "-m", "trusted change")
		if _, err := e.integrateAdvancingBase(ctx, c, p, prior, run("rev-parse", "HEAD")); err == nil {
			t.Fatal("trusted rebaseline accepted")
		}
	})
	t.Run("forged-source", func(t *testing.T) {
		bad := prior
		bad.Source = integrated.Source
		if _, err := e.integrateAdvancingBase(ctx, c, p, bad, current); err == nil {
			t.Fatal("forged source accepted")
		}
	})
	t.Run("not-an-advance", func(t *testing.T) {
		if _, err := e.integrateAdvancingBase(ctx, c, p, prior, base); err == nil {
			t.Fatal("unchanged base accepted")
		}
	})
}
