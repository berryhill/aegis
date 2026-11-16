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
	home, err := os.MkdirTemp(e.StateRoot, "git-freeze-")
	if err != nil {
		return "", "", err
	}
	name := filepath.Join(home, "index")
	joined := true
	defer func() {
		if joined {
			_ = os.RemoveAll(home)
		}
	}()
	run := func(input []byte, args ...string) (string, error) {
		if err := e.admit(ctx, "candidate-git-object"); err != nil {
			return "", err
		}
		cmd := exec.CommandContext(ctx, p.GitBinary, append([]string{"-C", workspace, "-c", "core.hooksPath=/dev/null"}, args...)...)
		cmd.Env = []string{"PATH=" + filepath.Dir(p.GitBinary), "HOME=" + e.StateRoot, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_INDEX_FILE=" + name, "GIT_AUTHOR_NAME=Aegis candidate controller", "GIT_AUTHOR_EMAIL=candidate@aegis.invalid", "GIT_COMMITTER_NAME=Aegis candidate controller", "GIT_COMMITTER_EMAIL=candidate@aegis.invalid", "GIT_AUTHOR_DATE=2000-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2000-01-01T00:00:00Z"}
		cmd.Stdin = strings.NewReader(string(input))
		b, err := e.commandOutput(ctx, cmd)
		if err != nil && e.Native != nil {
			joined = false // An unknown native writer may still own the private inputs.
		}
		return strings.TrimSpace(string(b)), err
	}
	if _, err = run(nil, "read-tree", "--empty"); err != nil {
		return "", "", err
	}
	var entries strings.Builder
	// Bound argv independently of manifest size. Each selected file retains its
	// authority check, and every batch repeats admission immediately before the
	// only effect. Hash only private copies of frozen bytes, never workspace paths
	// or filters supplied by a candidate repository.
	for start := 0; start < len(source.Files); start += 64 {
		files := source.Files[start:min(start+64, len(source.Files))]
		args := []string{"hash-object", "-w", "--no-filters", "--"}
		for i, f := range files {
			if !filepath.IsLocal(f.Path) || strings.ContainsAny(f.Path, "\x00\n\r") {
				return "", "", errors.New("invalid Git source path")
			}
			if err := e.admit(ctx, "candidate-git-object"); err != nil {
				return "", "", err
			}
			path := filepath.Join(home, fmt.Sprintf("blob-%d", start+i))
			if err := os.WriteFile(path, f.Content, 0600); err != nil {
				return "", "", err
			}
			args = append(args, path)
		}
		output, err := run(nil, args...)
		if err != nil {
			return "", "", err
		}
		blobs := strings.Split(output, "\n")
		if len(blobs) != len(files) {
			return "", "", errors.New("invalid Git blob inventory")
		}
		for i, f := range files {
			blob := blobs[i]
			if (len(blob) != 40 && len(blob) != 64) || strings.Trim(blob, "0123456789abcdef") != "" {
				return "", "", errors.New("invalid Git blob identity")
			}
			mode := "100644"
			if f.Mode&0111 != 0 {
				mode = "100755"
			}
			fmt.Fprintf(&entries, "%s %s	%s\x00", mode, blob, f.Path)
		}
	}
	// One freshly admitted index transaction for the complete frozen manifest;
	// no user index or branch is ever changed.
	if _, err = run([]byte(entries.String()), "update-index", "-z", "--index-info"); err != nil {
		return "", "", err
	}
	tree, err := run(nil, "write-tree")
	if err != nil {
		return "", "", err
	}
	commit, err := run([]byte("Frozen managed contribution candidate\n"), "commit-tree", tree, "-p", base)
	return commit, tree, err
}
