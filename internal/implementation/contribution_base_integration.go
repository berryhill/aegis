package implementation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/berryhill/aegis/internal/loop"
)

// ContributionBaseIntegration is source evidence, NOT candidate acceptance or
// permission to change the sealed base. Its source requires a new consumed pass,
// build, freeze and independently admitted tester before it can be published.
type ContributionBaseIntegration struct {
	OriginalBase string          `json:"original_base"`
	CurrentBase  string          `json:"current_base"`
	Predecessor  string          `json:"predecessor"`
	Tree         string          `json:"tree"`
	Source       SourceInventory `json:"source"`
}

// integrateAdvancingBase performs a conservative native three-tree integration
// in a private index. The caller must separately admit the canonical current-base
// readback under source custody. No branch, real index, worktree or old receipt is
// changed. Same-path conflicts are deliberately returned to the source owner;
// external merge drivers, hooks, filters and arbitrary upstream commands never run.
func (e *ManagedContribution) integrateAdvancingBase(ctx context.Context, c loop.VerifiedImplementation, p loop.ContributionExecutionPolicy, prior ContributionCandidate, current string) (ContributionBaseIntegration, error) {
	var out ContributionBaseIntegration
	denied := errors.New("advancing base integration denied")
	oid := func(s string) bool {
		if len(s) != 40 {
			return false
		}
		for _, r := range s {
			if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
				return false
			}
		}
		return true
	}
	if e == nil || ctx == nil || ctx.Err() != nil || e.Admit == nil || c.RepositoryContext == nil || !filepath.IsAbs(e.StateRoot) || !filepath.IsAbs(c.Workspace) || !filepath.IsAbs(p.GitBinary) || !filepath.IsAbs(p.RepositoryPath) || !oid(prior.Base) || !oid(current) || current == prior.Base || !oid(prior.CandidateCommit) || !oid(prior.CandidateTree) || !sha256Digest(prior.Digest) {
		return out, denied
	}
	origin, err := e.git(ctx, p, p.RepositoryPath, "remote", "get-url", "origin")
	if err != nil || e.Repository == "" || strings.TrimSpace(string(origin)) != "https://github.com/"+e.Repository+".git" {
		return out, denied
	}
	if _, err = e.git(ctx, p, c.Workspace, "merge-base", "--is-ancestor", prior.Base, current); err != nil {
		return out, denied
	}
	tree, err := e.git(ctx, p, c.Workspace, "rev-parse", prior.CandidateCommit+"^{tree}")
	if err != nil || strings.TrimSpace(string(tree)) != prior.CandidateTree {
		return out, denied
	}
	// Refuse a forged predecessor source/tree pair before merging any objects.
	_, frozenTree, err := e.freezeGit(ctx, p, c.Workspace, prior.Base, prior.Source)
	if err != nil || frozenTree != prior.CandidateTree {
		return out, denied
	}
	index, err := os.CreateTemp(e.StateRoot, "integration-index-")
	if err != nil {
		return out, err
	}
	name := index.Name()
	index.Close()
	os.Remove(name)
	defer os.Remove(name)
	run := func(args ...string) ([]byte, error) {
		if err := e.admit(ctx, "base-integration"); err != nil {
			return nil, err
		}
		cmd := exec.CommandContext(ctx, p.GitBinary, append([]string{"-C", c.Workspace, "-c", "core.hooksPath=/dev/null"}, args...)...)
		cmd.Env = []string{"PATH=" + filepath.Dir(p.GitBinary), "HOME=" + e.StateRoot, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_INDEX_FILE=" + name}
		return e.commandOutput(ctx, cmd)
	}
	if _, err = run("read-tree", "-m", prior.Base, prior.CandidateCommit, current); err != nil {
		return out, denied
	}
	entries, err := run("ls-files", "--stage", "-z")
	if err != nil {
		return out, denied
	}
	source := SourceInventory{Schema: "aegis.contribution.source.v1"}
	total := 0
	for _, entry := range strings.Split(string(entries), "\x00") {
		if entry == "" {
			continue
		}
		meta, path, ok := strings.Cut(entry, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 || fields[2] != "0" || !oid(fields[1]) || !filepath.IsLocal(path) || filepath.Clean(path) != path || strings.ContainsAny(path, "\r\n") || (fields[0] != "100644" && fields[0] != "100755") {
			return out, denied
		}
		size, err := run("cat-file", "-s", fields[1])
		if err != nil {
			return out, denied
		}
		n, err := strconv.Atoi(strings.TrimSpace(string(size)))
		if err != nil || n < 0 || n > maxBytes || total+n > maxBytes || len(source.Files) >= 4096 {
			return out, denied
		}
		content, err := run("cat-file", "blob", fields[1])
		if err != nil || len(content) != n {
			return out, denied
		}
		mode := uint32(0644)
		if fields[0] == "100755" {
			mode = 0755
		}
		total += n
		source.Files = append(source.Files, SourceFile{Path: path, Mode: mode, Content: content})
	}
	// An upstream edit is not authority to replace trusted tests, module/checker
	// inputs or reviewed security/launch assets. Those need a separately admitted
	// policy amendment, not an automatic rebaseline.
	for _, f := range c.RepositoryContext.Files {
		writable := false
		for _, path := range c.WritableFiles {
			if path == f.Path {
				writable = true
			}
		}
		if writable && f.Role == "source" {
			continue
		}
		found := false
		for _, next := range source.Files {
			if next.Path == f.Path && digest(next.Content) == f.Digest {
				found = true
			}
		}
		if !found {
			return out, denied
		}
	}
	for _, asset := range p.ReviewAssets {
		found := false
		for _, next := range source.Files {
			if next.Path == asset.Path && digest(next.Content) == asset.ApprovedDigest {
				found = true
			}
		}
		if !found {
			return out, denied
		}
	}
	// Materialization plus the normal inventory reader applies sensitive-file,
	// mode/type/size and complete-inventory policy to the actual integrated bytes.
	home, err := os.MkdirTemp(e.StateRoot, "integration-source-")
	if err != nil {
		return out, err
	}
	defer os.RemoveAll(home)
	dest := filepath.Join(home, "source")
	if err = source.Materialize(dest); err != nil {
		return out, err
	}
	inventory, err := Inventory(ctx, dest)
	if err != nil {
		return out, err
	}
	tree, err = run("write-tree")
	if err != nil {
		return out, denied
	}
	merged := strings.TrimSpace(string(tree))
	if !oid(merged) {
		return out, fmt.Errorf("invalid integrated tree")
	}
	return ContributionBaseIntegration{OriginalBase: prior.Base, CurrentBase: current, Predecessor: prior.Digest, Tree: merged, Source: inventory}, nil
}

// Called only after the next candidate's consumption is durable, under the
// same source lock. Integration is immutable controller input, not model edits.
func (e *ManagedContribution) applyBaseIntegration(ctx context.Context, c loop.VerifiedImplementation, v *ContributionBaseIntegration) error {
	if err := e.admit(ctx, "base-integration-apply"); err != nil {
		return err
	}
	current, err := Inventory(ctx, c.Workspace)
	if err != nil {
		return err
	}
	wanted := map[string]bool{}
	for _, f := range v.Source.Files {
		wanted[f.Path] = true
		path := filepath.Join(c.Workspace, f.Path)
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		if err = os.WriteFile(path, f.Content, os.FileMode(f.Mode)); err != nil {
			return err
		}
		if err = os.Chmod(path, os.FileMode(f.Mode)); err != nil {
			return err
		}
	}
	for _, f := range current.Files {
		if !wanted[f.Path] {
			if err = os.Remove(filepath.Join(c.Workspace, f.Path)); err != nil {
				return err
			}
		}
	}
	actual, err := Inventory(ctx, c.Workspace)
	if err != nil {
		return err
	}
	if actual.Digest != v.Source.Digest {
		return errors.New("integrated source inventory mismatch")
	}
	return nil
}
