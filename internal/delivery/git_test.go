package delivery

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureGit(t *testing.T) (*Git, string) {
	t.Helper()
	dir := t.TempDir()
	g := &Git{Binary: "/usr/bin/git", Directory: dir}
	run := func(args ...string) string {
		t.Helper()
		s, e := g.run(context.Background(), nil, nil, args...)
		if e != nil {
			t.Fatalf("git %v: %v", args, e)
		}
		return strings.TrimSpace(string(s))
	}
	run("init", "--initial-branch=main")
	if e := os.WriteFile(filepath.Join(dir, "source"), []byte("before"), 0644); e != nil {
		t.Fatal(e)
	}
	run("add", "source")
	env := []string{"GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid"}
	if _, e := g.run(context.Background(), env, nil, "commit", "-m", "baseline"); e != nil {
		t.Fatal(e)
	}
	return g, run("rev-parse", "HEAD")
}
func commitFixture(t *testing.T) (*Git, CommitSpec) {
	g, parent := fixtureGit(t)
	_ = os.WriteFile(filepath.Join(g.Directory, "source"), []byte("after"), 0755)
	_ = os.Chmod(filepath.Join(g.Directory, "source"), 0755)
	_ = os.WriteFile(filepath.Join(g.Directory, "private"), []byte("unrelated private canary"), 0600)
	s := CommitSpec{Parent: parent, Branch: "contribution/run", Author: Identity{"Fixture", "fixture@example.invalid"}, Committer: Identity{"Fixture", "fixture@example.invalid"}, Timestamp: "2026-01-01T00:00:00Z", Message: "scoped\n", Files: []File{{Path: "source", Mode: "100755", Bytes: []byte("after")}}}
	tree, commit, e := g.prepare(context.Background(), s)
	if e != nil {
		t.Fatal(e)
	}
	s.Tree = tree
	s.Commit = commit
	return g, s
}
func TestScopedCommitPreservesIndexAndUnrelatedDirty(t *testing.T) {
	g, s := commitFixture(t)
	before, _ := g.run(context.Background(), nil, nil, "ls-files", "--stage")
	o := g.Commit(context.Background(), s, func(context.Context) error { return nil })
	if o.Outcome != Confirmed || o.Head != s.Commit {
		t.Fatalf("%+v", o)
	}
	after, _ := g.run(context.Background(), nil, nil, "ls-files", "--stage")
	if string(before) != string(after) {
		t.Fatal("shared index changed")
	}
	b, _ := os.ReadFile(filepath.Join(g.Directory, "private"))
	if string(b) != "unrelated private canary" {
		t.Fatal("unrelated file changed")
	}
	if got, _ := g.run(context.Background(), nil, nil, "rev-parse", "HEAD"); strings.TrimSpace(string(got)) != s.Parent {
		t.Fatal("HEAD moved")
	}
	o = g.Reconcile(context.Background(), s)
	if o.Outcome != Confirmed {
		t.Fatal(o)
	}
}
func TestScopedCommitRejectsDriftModesPathsAndIdentity(t *testing.T) {
	for _, kind := range []string{"bytes", "mode", "path", "identity", "tree", "commit", "symlink", "denied"} {
		t.Run(kind, func(t *testing.T) {
			g, s := commitFixture(t)
			guard := func(context.Context) error { return nil }
			switch kind {
			case "bytes":
				_ = os.WriteFile(filepath.Join(g.Directory, "source"), []byte("changed"), 0755)
			case "mode":
				_ = os.Chmod(filepath.Join(g.Directory, "source"), 0644)
			case "path":
				s.Files[0].Path = "../escape"
			case "identity":
				s.Author.Email = "bad\nidentity"
			case "tree":
				s.Tree = strings.Repeat("f", 40)
			case "commit":
				s.Commit = strings.Repeat("f", 40)
			case "symlink":
				_ = os.Remove(filepath.Join(g.Directory, "source"))
				_ = os.Symlink("private", filepath.Join(g.Directory, "source"))
			case "denied":
				guard = func(context.Context) error { return ErrDenied }
			}
			o := g.Commit(context.Background(), s, guard)
			if o.Outcome != Denied {
				t.Fatalf("%s %+v", kind, o)
			}
			if _, e := g.run(context.Background(), nil, nil, "show-ref", "--verify", "refs/heads/"+s.Branch); e == nil {
				t.Fatal("denied ref written")
			}
		})
	}
}
func TestGitEnvironmentDoesNotInheritCredentialOrHooks(t *testing.T) {
	t.Setenv("GH_TOKEN", "private-canary")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.hooksPath")
	t.Setenv("GIT_CONFIG_VALUE_0", "/bad")
	g, _ := fixtureGit(t)
	c := exec.Command(g.Binary, "--version")
	c.Env = g.environment(nil)
	for _, v := range c.Env {
		if strings.Contains(v, "private-canary") || strings.Contains(v, "/bad") {
			t.Fatal("ambient value inherited")
		}
	}
}
