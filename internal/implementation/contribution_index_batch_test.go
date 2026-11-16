package implementation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/loop"
)

func TestContributionFrozenIndexBatchIdentityAndAdmission(t *testing.T) {
	driver, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	driver, err = filepath.EvalSymlinks(driver)
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	home := t.TempDir()
	env := []string{"PATH=" + filepath.Dir(driver), "HOME=" + home, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"}
	run := func(index string, input []byte, args ...string) string {
		t.Helper()
		cmd := exec.Command(driver, append([]string{"-C", repo}, args...)...)
		cmd.Env = append([]string(nil), env...)
		if index != "" {
			cmd.Env = append(cmd.Env, "GIT_INDEX_FILE="+index)
		}
		cmd.Stdin = bytes.NewReader(input)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git fixture: %v %s", err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("", nil, "init", "-b", "main")
	run("", nil, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--allow-empty", "-m", "base")
	base := run("", nil, "rev-parse", "HEAD")
	if err = os.WriteFile(filepath.Join(repo, "user.txt"), []byte("unrelated staged input"), 0600); err != nil {
		t.Fatal(err)
	}
	run("", nil, "add", "user.txt")
	original, err := os.ReadFile(filepath.Join(repo, ".git/index"))
	if err != nil {
		t.Fatal(err)
	}
	source := SourceInventory{Files: []SourceFile{{Path: "a file,with	tab.go", Mode: 0600, Content: []byte("first")}, {Path: "nested/executable", Mode: 0700, Content: []byte("second")}, {Path: "-literal", Mode: 0600, Content: []byte("third")}}}
	for i := 0; i < 65; i++ {
		source.Files = append(source.Files, SourceFile{Path: fmt.Sprintf("many/file-%d", i), Mode: 0600, Content: []byte(fmt.Sprint(i))})
	}
	batches := (len(source.Files) + 63) / 64
	// Independent original per-entry operation must yield the identical tree.
	index := filepath.Join(t.TempDir(), "expected-index")
	run(index, nil, "read-tree", "--empty")
	for _, f := range source.Files {
		blob := run(index, f.Content, "hash-object", "-w", "--stdin")
		mode := "100644"
		if f.Mode&0111 != 0 {
			mode = "100755"
		}
		run(index, nil, "update-index", "--add", "--cacheinfo", fmt.Sprintf("%s,%s,%s", mode, blob, f.Path))
	}
	expected := run(index, nil, "write-tree")
	policy := loop.ContributionExecutionPolicy{GitBinary: driver}
	for _, scenario := range []string{"success", "batch-denied", "blob-injection", "native-unjoined"} {
		t.Run(scenario, func(t *testing.T) {
			e := &ManagedContribution{StateRoot: t.TempDir()}
			hashes, updates := 0, 0
			denial := errors.New("fresh index admission denied")
			e.Admit = func(_ context.Context, effect string) error {
				if effect != "candidate-git-object" {
					t.Fatal(effect)
				}
				if scenario == "batch-denied" && hashes == batches {
					return denial
				}
				return nil
			}
			e.Native = func(ctx context.Context, cmd *exec.Cmd) error {
				for _, arg := range cmd.Args {
					if arg == "hash-object" {
						hashes++
						if !strings.Contains(strings.Join(cmd.Args, " "), "--no-filters --") {
							t.Fatal("candidate filters reachable")
						}
						if scenario == "native-unjoined" {
							return errors.New("unknown writer")
						}
						if scenario == "blob-injection" {
							_, _ = cmd.Stdout.Write([]byte(strings.Repeat("a", 40) + "\n100644 " + strings.Repeat("b", 40) + "\tinjected"))
							return nil
						}
					}
					if arg == "update-index" {
						updates++
					}
				}
				return runCheckerNative(ctx, cmd)
			}
			commit, tree, err := e.freezeGit(context.Background(), policy, repo, base, source)
			switch scenario {
			case "success":
				if err != nil || tree != expected || commit == "" || updates != 1 || hashes != batches {
					t.Fatalf("tree=%s expected=%s commit=%s updates=%d hashes=%d err=%v", tree, expected, commit, updates, hashes, err)
				}
				if run("", nil, "rev-parse", commit+"^{tree}") != expected {
					t.Fatal("commit tree mismatch")
				}
			case "batch-denied":
				if !errors.Is(err, denial) || updates != 0 || commit != "" || tree != "" {
					t.Fatal("denied batch executed", err)
				}
			case "blob-injection", "native-unjoined":
				if err == nil || updates != 0 || commit != "" || tree != "" {
					t.Fatal("native output injected index input", err)
				}
			}
			homes, scanErr := filepath.Glob(filepath.Join(e.StateRoot, "git-freeze-*"))
			if scanErr != nil || (scenario == "native-unjoined" && len(homes) != 1) || (scenario != "native-unjoined" && len(homes) != 0) {
				t.Fatal("private input custody", homes, scanErr)
			}
			after, err := os.ReadFile(filepath.Join(repo, ".git/index"))
			if err != nil || !bytes.Equal(original, after) || run("", nil, "rev-parse", "HEAD") != base {
				t.Fatal("user index or branch changed", err)
			}
		})
	}
}
