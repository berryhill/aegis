package implementation

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/loop"
)

func TestContributionRetainedBoundary(t *testing.T) {
	ctx := context.Background()
	kernel, c := fixture(t)
	repo := c.Workspace
	git, _ := exec.LookPath("git")
	git, _ = filepath.EvalSymlinks(git)
	command := func(dir string, args ...string) string {
		t.Helper()
		b, e := exec.Command(git, append([]string{"-C", dir}, args...)...).CombinedOutput()
		if e != nil {
			t.Fatalf("git: %s %v", b, e)
		}
		return strings.TrimSpace(string(b))
	}
	command(repo, "init", "-b", "main")
	command(repo, "add", ".")
	command(repo, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "base")
	base := command(repo, "rev-parse", "HEAD")
	c.Workspace = filepath.Join(t.TempDir(), "owned")
	branch := "contribution-" + strings.TrimPrefix(digest([]byte("run")), "sha256:")
	command(repo, "worktree", "add", "-b", branch, c.Workspace, base)
	p := loop.ContributionExecutionPolicy{RepositoryPath: repo, GitBinary: git}
	command(repo, "remote", "add", "origin", "https://github.com/fixture/controlled.git")
	e := &ManagedContribution{DB: kernel.DB, Admit: kernel.Admit, StateRoot: t.TempDir(), Repository: "fixture/controlled"}
	source, err := Inventory(ctx, c.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	identity, _ := json.Marshal(c)
	r := ContributionRecord{RunID: "run", Contract: "contract", QueueAttempt: "attempt", Base: base, Workspace: c.Workspace, Implementation: digest(identity), State: "phase_boundary", Policy: p, Expected: &CollectedRepositoryContext{}, BoundarySource: source.Digest, Baseline: ContributionCheck{Outcome: "failed"}}
	r.BoundaryIndex = digest([]byte(command(c.Workspace, "ls-files", "--stage", "-z")))
	// No selected repository context in this fixture: verifyContext is a no-op.
	if err = e.DB.Create([]byte("contribution/workspace/"+digest([]byte(c.Workspace))), []byte("run")); err != nil {
		t.Fatal(err)
	}
	check := func(r ContributionRecord, attempt string) error {
		return e.resumeBoundary(ctx, r, "run", "contract", attempt, base, c, p)
	}
	if err = check(r, "attempt"); err != nil {
		t.Fatalf("valid retained boundary: %v", err)
	}
	t.Run("stale-attempt", func(t *testing.T) {
		if check(r, "stale") == nil {
			t.Fatal("stale attempt accepted")
		}
	})
	t.Run("interrupted-held", func(t *testing.T) {
		v := r
		v.State = "interrupted"
		if check(v, "attempt") == nil {
			t.Fatal("unknown effect accepted")
		}
	})
	t.Run("running-held", func(t *testing.T) {
		v := r
		v.State = "running"
		if check(v, "attempt") == nil {
			t.Fatal("in-flight effect accepted")
		}
	})
	t.Run("consumed-budget", func(t *testing.T) {
		v := r
		v.Consumed = c.MaxPasses
		v.Passes = make([]ContributionPass, int(c.MaxPasses))
		if check(v, "attempt") == nil {
			t.Fatal("budget reset")
		}
	})
	t.Run("foreign-dirty-preserved", func(t *testing.T) {
		path := filepath.Join(c.Workspace, "foreign")
		os.WriteFile(path, []byte("keep"), 0600)
		defer os.Remove(path)
		if check(r, "attempt") == nil {
			t.Fatal("dirty work accepted")
		}
		b, _ := os.ReadFile(path)
		if string(b) != "keep" {
			t.Fatal("foreign data changed")
		}
	})
	t.Run("foreign-owner-preserved", func(t *testing.T) {
		key := []byte("contribution/workspace/" + digest([]byte(c.Workspace)))
		e.DB.Put(key, []byte("foreign"))
		defer e.DB.Put(key, []byte("run"))
		if check(r, "attempt") == nil {
			t.Fatal("foreign owner accepted")
		}
		if command(c.Workspace, "branch", "--show-current") != branch {
			t.Fatal("branch changed")
		}
	})
	t.Run("concurrent-source-writer", func(t *testing.T) {
		unlock, err := lockWorkspace(c.Workspace)
		if err != nil {
			t.Fatal(err)
		}
		defer unlock()
		second, err := lockWorkspace(c.Workspace)
		if err == nil {
			second()
			t.Fatal("parallel custody accepted")
		}
	})
}

func TestContributionTesterEvidencePrivacy(t *testing.T) {
	v := ContributionCandidate{Source: SourceInventory{Schema: "source", Digest: "digest", Files: []SourceFile{{Path: "private.go", Content: []byte("credential-sentinel")}}}}
	b, err := json.Marshal(v.TesterEvidence())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "private.go") || strings.Contains(string(b), "credential-sentinel") || len(v.Source.Files) != 1 {
		t.Fatal("source evidence exposed or original mutated")
	}
}
