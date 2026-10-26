package orchestration

import (
	"github.com/berryhill/aegis/internal/loop"
	"os"
	"path/filepath"
	"testing"
)

func TestManagedGraphOutputPreconditions(t *testing.T) {
	root := t.TempDir()
	a := loop.DoerContract{Workspace: root, VerifyFile: "result.txt"}
	if err := managedOutputAbsent(a); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "result.txt"), []byte("unrelated existing content"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := managedOutputAbsent(a); err == nil {
		t.Fatal("existing wrong output accepted")
	}
	data, err := os.ReadFile(filepath.Join(root, "result.txt"))
	if err != nil || string(data) != "unrelated existing content" {
		t.Fatal("existing output changed")
	}
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	for _, workspace := range []string{root, nested} {
		b := loop.DoerContract{Workspace: workspace}
		if managedWorkspacesSeparate(a, b) == nil || managedWorkspacesSeparate(b, a) == nil {
			t.Fatal("overlapping workspace accepted")
		}
	}
	b := loop.DoerContract{Workspace: t.TempDir()}
	if err := managedWorkspacesSeparate(a, b); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	a.Workspace = link
	if managedOutputAbsent(a) == nil {
		t.Fatal("workspace symlink accepted")
	}
}
