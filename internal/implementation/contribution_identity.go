package implementation

import (
	"context"
	"errors"
	"fmt"
	"github.com/berryhill/aegis/internal/loop"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// freezeGit uses an isolated index and creates local, unreachable Git objects.
// It never stages the user's index, moves a branch, signs, invokes hooks or publishes.
func (e *ManagedContribution) freezeGit(ctx context.Context, p loop.ContributionExecutionPolicy, workspace, base string, source SourceInventory) (string, string, error) {
	if err := e.admit(ctx, "candidate-git-object"); err != nil {
		return "", "", err
	}
	index, err := os.CreateTemp(e.StateRoot, "index-")
	if err != nil {
		return "", "", err
	}
	name := index.Name()
	index.Close()
	os.Remove(name)
	defer os.Remove(name)
	run := func(input []byte, args ...string) (string, error) {
		if err := e.admit(ctx, "candidate-git-object"); err != nil {
			return "", err
		}
		cmd := exec.CommandContext(ctx, p.GitBinary, append([]string{"-C", workspace, "-c", "core.hooksPath=/dev/null"}, args...)...)
		cmd.Env = []string{"PATH=" + filepath.Dir(p.GitBinary), "HOME=" + e.StateRoot, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_INDEX_FILE=" + name, "GIT_AUTHOR_NAME=Aegis candidate controller", "GIT_AUTHOR_EMAIL=candidate@aegis.invalid", "GIT_COMMITTER_NAME=Aegis candidate controller", "GIT_COMMITTER_EMAIL=candidate@aegis.invalid", "GIT_AUTHOR_DATE=2000-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2000-01-01T00:00:00Z"}
		cmd.Stdin = strings.NewReader(string(input))
		b, err := cmd.Output()
		return strings.TrimSpace(string(b)), err
	}
	if _, err = run(nil, "read-tree", "--empty"); err != nil {
		return "", "", err
	}
	for _, f := range source.Files {
		if !filepath.IsLocal(f.Path) || strings.ContainsAny(f.Path, "\x00\n\r") {
			return "", "", errors.New("invalid Git source path")
		}
		blob, x := run(f.Content, "hash-object", "-w", "--stdin")
		if x != nil {
			return "", "", x
		}
		mode := "100644"
		if f.Mode&0111 != 0 {
			mode = "100755"
		}
		if _, x = run(nil, "update-index", "--add", "--cacheinfo", fmt.Sprintf("%s,%s,%s", mode, blob, f.Path)); x != nil {
			return "", "", x
		}
	}
	tree, err := run(nil, "write-tree")
	if err != nil {
		return "", "", err
	}
	commit, err := run([]byte("Frozen managed contribution candidate\n"), "commit-tree", tree, "-p", base)
	return commit, tree, err
}
