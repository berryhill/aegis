package implementation

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestEngineeringTemplGeneratedDeltaRejectsExtraWritesAndModeDrift(t *testing.T) {
	before := SourceInventory{Files: []SourceFile{{Path: "web/view.templ", Mode: 0640, Content: []byte("source")}, {Path: "web/view_templ.go", Mode: 0640, Content: []byte("old")}, {Path: "go.sum", Mode: 0600, Content: []byte("locked")}}}
	for _, kind := range []string{"exact", "extra", "missing", "mode", "source", "lock"} {
		t.Run(kind, func(t *testing.T) {
			after := SourceInventory{Files: append([]SourceFile{}, before.Files...)}
			after.Files[1].Content = []byte("real compiler output")
			switch kind {
			case "extra":
				after.Files = append(after.Files, SourceFile{Path: "other.go"})
			case "missing":
				after.Files = after.Files[:2]
			case "mode":
				after.Files[1].Mode = 0600
			case "source":
				after.Files[0].Content = []byte("drift")
			case "lock":
				after.Files[2].Content = []byte("drift")
			}
			edits, e := generatedTemplEdits(before, after, []string{"web/view.templ"})
			if kind == "exact" {
				if e != nil || len(edits) != 1 || edits[0].Path != "web/view_templ.go" || edits[0].preserveMode != 0640 {
					t.Fatalf("%+v %v", edits, e)
				}
			} else if e == nil {
				t.Fatal("unallowlisted generation delta accepted")
			}
		})
	}
}
func TestEngineeringTemplGeneratorFreshExactEffectDenial(t *testing.T) {
	root := t.TempDir()
	source := SourceInventory{Files: []SourceFile{{Path: "go.mod", Content: []byte("module fixture\ngo 1.24\nrequire github.com/a-h/templ v0.3.960\ntool github.com/a-h/templ/cmd/templ\n")}}}
	denied := errors.New("fresh authority denied")
	calls := 0
	err := generateTemplPairs(context.Background(), "/unexecuted/go", root, nil, source, []string{"web/view.templ"}, func(context.Context, *exec.Cmd) error { calls++; return nil }, func(_ context.Context, effect string) error {
		if effect != "generate:web/view.templ->web/view_templ.go" {
			t.Fatal(effect)
		}
		return denied
	})
	if !errors.Is(err, denied) || calls != 0 {
		t.Fatalf("generation ran before exact admission: %v %d", err, calls)
	}
	if _, e := os.Stat(filepath.Join(root, "web/view_templ.go")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("denial mutated source")
	}
}
