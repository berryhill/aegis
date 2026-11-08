package implementation

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/berryhill/aegis/internal/loop"
)

func TestEngineeringNewRegressionLifecycle(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	files := map[string]string{"go.mod": "module fixture\n\ngo 1.24\n", "value.go": "package fixture\nfunc Value() int{return 1}\n", "trusted_test.go": "package fixture\nimport \"testing\"\nfunc TestTrusted(t *testing.T){if Value()!=2{t.Fatal(Value())}}\n"}
	for p, b := range files {
		if err := os.WriteFile(filepath.Join(root, p), []byte(b), 0600); err != nil {
			t.Fatal(err)
		}
	}
	c := loop.ImplementationDraft("template", "trusted")
	c.Workspace = root
	c.WritableFiles = []string{"value.go"}
	c.Policy.RequiredTests = []loop.RequiredGoTest{{Package: "fixture", Name: "TestTrusted"}}
	c.RepositoryContext = &loop.RepositoryContext{TaskDigest: digest([]byte(c.Task)), MaxFiles: 8, MaxFileBytes: 4096, MaxTotalBytes: 16384}
	for _, p := range []string{"go.mod", "trusted_test.go"} {
		role := "trusted_test"
		if p == "go.mod" {
			role = "checker_input"
		}
		b := files[p]
		c.RepositoryContext.Files = append(c.RepositoryContext.Files, loop.RepositoryContextFile{Path: p, Role: role, Bytes: int64(len(b)), Digest: digest([]byte(b))})
	}
	c.RepositoryContext.SourceDigest = RepositorySourceDigest(c.RepositoryContext)
	v, err := ResolveEngineeringContext(ctx, c, "fix value", "Value returns 2", root, root, []string{"value.go", "new_regression_test.go"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	entry := v.RepositoryContext.Files[3]
	if !entry.Absent || entry.Bytes != 0 || entry.Digest != digest(nil) {
		t.Fatal("no negative preimage", entry)
	}
	initial, err := CollectRepositoryContext(ctx, v, v.RepositoryContext.SourceDigest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = contractSnapshot(v); err != nil {
		t.Fatal(err)
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	e := &Executor{GoBinary: goBin, Native: stdlibSeededNative(t, goBin), Admit: func(context.Context, string) error { return nil }}
	if _, passed, err := e.checkCandidate(ctx, v, initial); err != nil || passed {
		t.Fatalf("trusted red: %v %v", passed, err)
	}
	regression := []byte("package fixture\nimport \"testing\"\nfunc TestRegression(t *testing.T){if Value()!=2{t.Fatal(Value())}}\n")
	edits := []Edit{{Path: "value.go", Content: []byte("package fixture\nfunc Value() int{return 2}\n")}, {Path: "new_regression_test.go", Content: regression}}
	current := cloneContext(initial)
	if err = e.apply(ctx, v, edits, current); err != nil {
		t.Fatal(err)
	}
	if current.Files[3].Absent {
		t.Fatal("negative preimage did not advance")
	}
	if err = verifyContext(ctx, v, initial); err == nil {
		t.Fatal("new file was confused with absent preimage")
	}
	if _, passed, err := e.checkCandidate(ctx, v, current); err != nil || !passed {
		t.Fatalf("trusted green: %v %v", passed, err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "trusted_test.go")); string(b) != files["trusted_test.go"] {
		t.Fatal("trusted check modified")
	}
}

func TestEngineeringAbsentPathDenials(t *testing.T) {
	root := t.TempDir()
	r, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for _, p := range []string{"../escape_test.go", "missing/value_test.go", "value.go"} {
		if verifyAbsentRegression(r, p) == nil {
			t.Fatal(p)
		}
	}
	if err = os.WriteFile(filepath.Join(root, "empty_test.go"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if verifyAbsentRegression(r, "empty_test.go") == nil {
		t.Fatal("empty is absent")
	}
	if err = os.Symlink(t.TempDir(), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if verifyAbsentRegression(r, "link/new_test.go") == nil {
		t.Fatal("symlink parent")
	}
}
