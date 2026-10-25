package delivery

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Identity struct{ Name, Email string }

// File is the exact reviewed change inventory, not a whole candidate manifest.
// Mode is 100644/100755; mode 000000 denotes a reviewed deletion.
type File struct {
	Path, Mode string
	Bytes      []byte
}
type CommitSpec struct {
	Parent, Tree, Commit, Branch string
	Author, Committer            Identity
	Timestamp, Message           string
	Files                        []File
}
type Git struct{ Binary, Directory string }

func (g *Git) environment(extra []string) []string {
	return append([]string{"PATH=/usr/bin:/bin", "HOME=/nonexistent", "LANG=C", "LC_ALL=C", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_NO_REPLACE_OBJECTS=1"}, extra...)
}
func (g *Git) run(ctx context.Context, env []string, in []byte, args ...string) ([]byte, error) {
	if !filepath.IsAbs(g.Binary) || !filepath.IsAbs(g.Directory) {
		return nil, ErrDenied
	}
	cmd := exec.CommandContext(ctx, g.Binary, append([]string{"--no-optional-locks", "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-C", g.Directory}, args...)...)
	cmd.Env = g.environment(env)
	cmd.Stdin = bytes.NewReader(in)
	cmd.Stderr = io.Discard
	b, e := cmd.Output()
	if e != nil {
		return nil, ErrDenied
	}
	return b, nil
}
func safePath(p string) bool {
	return p != "" && !strings.ContainsAny(p, "\x00\r\n\\") && !filepath.IsAbs(p) && filepath.Clean(p) == p && p != ".." && !strings.HasPrefix(p, "../") && p != ".git" && !strings.HasPrefix(p, ".git/") && p != "."
}
func identityOK(i Identity) bool {
	return i.Name != "" && i.Email != "" && !strings.ContainsAny(i.Name+i.Email, "<>\x00\r\n")
}
func (g *Git) files(s CommitSpec) error {
	if len(s.Files) == 0 || len(s.Files) > 4096 {
		return ErrDenied
	}
	seen := map[string]bool{}
	var size int
	for _, f := range s.Files {
		if !safePath(f.Path) || seen[f.Path] {
			return ErrDenied
		}
		seen[f.Path] = true
		path := g.Directory
		parts := strings.Split(f.Path, "/")
		for n, part := range parts {
			path = filepath.Join(path, part)
			info, e := os.Lstat(path)
			if n == len(parts)-1 && f.Mode == "000000" {
				if !os.IsNotExist(e) || len(f.Bytes) != 0 {
					return ErrDenied
				}
				continue
			}
			if e != nil || info.Mode()&os.ModeSymlink != 0 {
				return ErrDenied
			}
			if n < len(parts)-1 {
				if !info.IsDir() {
					return ErrDenied
				}
				continue
			}
			if !info.Mode().IsRegular() {
				return ErrDenied
			}
			mode := "100644"
			if info.Mode().Perm()&0111 != 0 {
				mode = "100755"
			}
			if mode != f.Mode {
				return ErrDenied
			}
			b, e := os.ReadFile(path)
			if e != nil || !bytes.Equal(b, f.Bytes) {
				return ErrDenied
			}
			size += len(b)
			if size > 32<<20 {
				return ErrDenied
			}
		}
	}
	return nil
}

// prepare uses plumbing and a private index; it never runs hooks, filters, a
// shell, a credential helper, git add . or writes the caller's index/HEAD.
func (g *Git) prepare(ctx context.Context, s CommitSpec) (string, string, error) {
	if !sha.MatchString(s.Parent) || !branchName.MatchString(s.Branch) || !strings.Contains(s.Branch, "/") || !identityOK(s.Author) || !identityOK(s.Committer) || len(s.Message) == 0 || len(s.Message) > 16384 || strings.ContainsRune(s.Message, 0) {
		return "", "", ErrDenied
	}
	if _, e := time.Parse(time.RFC3339, s.Timestamp); e != nil {
		return "", "", ErrDenied
	}
	for _, f := range s.Files {
		if closing.Match(f.Bytes) {
			return "", "", ErrDenied
		}
	}
	if g.files(s) != nil {
		return "", "", ErrDenied
	}
	gd, e := g.run(ctx, nil, nil, "rev-parse", "--absolute-git-dir")
	if e != nil {
		return "", "", e
	}
	dir, e := os.MkdirTemp(strings.TrimSpace(string(gd)), "delivery-index-")
	if e != nil {
		return "", "", ErrDenied
	}
	defer os.RemoveAll(dir)
	env := []string{"GIT_INDEX_FILE=" + filepath.Join(dir, "index")}
	if _, e = g.run(ctx, env, nil, "read-tree", s.Parent); e != nil {
		return "", "", e
	}
	files := append([]File(nil), s.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	for _, f := range files {
		if f.Mode == "000000" {
			_, e = g.run(ctx, env, nil, "update-index", "--force-remove", "--", f.Path)
		} else {
			var blob []byte
			blob, e = g.run(ctx, env, f.Bytes, "hash-object", "-w", "--stdin")
			if e == nil {
				_, e = g.run(ctx, env, nil, "update-index", "--add", "--cacheinfo", f.Mode, strings.TrimSpace(string(blob)), f.Path)
			}
		}
		if e != nil {
			return "", "", e
		}
	}
	tree, e := g.run(ctx, env, nil, "write-tree")
	if e != nil {
		return "", "", e
	}
	env = append(env, "GIT_AUTHOR_NAME="+s.Author.Name, "GIT_AUTHOR_EMAIL="+s.Author.Email, "GIT_COMMITTER_NAME="+s.Committer.Name, "GIT_COMMITTER_EMAIL="+s.Committer.Email, "GIT_AUTHOR_DATE="+s.Timestamp, "GIT_COMMITTER_DATE="+s.Timestamp)
	commit, e := g.run(ctx, env, []byte(s.Message), "commit-tree", strings.TrimSpace(string(tree)), "-p", s.Parent)
	return strings.TrimSpace(string(tree)), strings.TrimSpace(string(commit)), e
}
func (g *Git) Commit(ctx context.Context, s CommitSpec, guard Guard) Observation {
	deny := Observation{Outcome: Denied, Code: "scoped_commit_denied"}
	if guard == nil || guard(ctx) != nil || g.detachedTarget(ctx, s.Branch) != nil || closing.MatchString(s.Message) {
		return deny
	}
	tree, commit, e := g.prepare(ctx, s)
	if e != nil || tree != s.Tree || commit != s.Commit || !sha.MatchString(s.Commit) {
		return deny
	}
	if g.files(s) != nil || guard(ctx) != nil || g.detachedTarget(ctx, s.Branch) != nil {
		return deny
	}
	old := strings.Repeat("0", 40)
	if ref, e := g.run(ctx, nil, nil, "rev-parse", "--verify", "refs/heads/"+s.Branch); e == nil {
		old = strings.TrimSpace(string(ref))
		if old == s.Commit {
			return g.Reconcile(ctx, s)
		}
		if old != s.Parent {
			return deny
		}
	}
	if _, e = g.run(ctx, nil, nil, "update-ref", "refs/heads/"+s.Branch, s.Commit, old); e != nil {
		return Observation{Outcome: Unknown, Code: "local_ref_unknown"}
	}
	return g.Reconcile(ctx, s)
}

// detachedTarget denies moving a branch checked out in ANY linked worktree.
// The owner's exclusive custody must also prevent concurrent checkout changes.
// Detached HEAD worktrees are allowed; no caller HEAD/index/worktree is written.
func (g *Git) detachedTarget(ctx context.Context, branch string) error {
	b, e := g.run(ctx, nil, nil, "worktree", "list", "--porcelain", "-z")
	if e != nil || len(b) == 0 {
		return ErrDenied
	}
	for _, field := range strings.Split(string(b), "\x00") {
		if field == "branch refs/heads/"+branch {
			return ErrDenied
		}
	}
	if _, e = g.run(ctx, nil, nil, "rev-parse", "--verify", "HEAD"); e != nil {
		return ErrDenied
	}
	return nil
}

func (g *Git) Reconcile(ctx context.Context, s CommitSpec) Observation {
	parents, pe := g.run(ctx, nil, nil, "rev-list", "--parents", "-n", "1", s.Commit)
	if pe != nil || strings.TrimSpace(string(parents)) != s.Commit+" "+s.Parent {
		return Observation{Outcome: Unknown, Code: "parent_mismatch"}
	}
	head, e := g.run(ctx, nil, nil, "rev-parse", "--verify", "refs/heads/"+s.Branch)
	if e != nil || strings.TrimSpace(string(head)) != s.Commit {
		return Observation{Outcome: Unknown, Code: "local_ref_unknown"}
	}
	tree, e := g.run(ctx, nil, nil, "rev-parse", s.Commit+"^{tree}")
	if e != nil || strings.TrimSpace(string(tree)) != s.Tree {
		return Observation{Outcome: Denied, Code: "tree_mismatch"}
	}
	return Observation{Outcome: Confirmed, Head: s.Commit, Tree: s.Tree}
}
