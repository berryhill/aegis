package implementation

import (
	"archive/tar"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/berryhill/aegis/internal/loop"
)

func TestEngineeringResolvedContextBoundaries(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	files := map[string]string{"go.mod": "module fixture\n\ngo 1.24\n", "trusted_test.go": "package fixture\nimport \"testing\"\nfunc TestTrusted(t *testing.T){}\n", "value.go": "package fixture\n", "regression_test.go": "package fixture\n"}
	c := loop.ImplementationDraft("template", "trusted")
	c.Workspace = root
	c.Policy.RequiredTests = []loop.RequiredGoTest{{Package: "fixture", Name: "TestTrusted"}}
	c.WritableFiles = []string{"unused.go"}
	c.RepositoryContext = &loop.RepositoryContext{TaskDigest: digest([]byte(c.Task)), MaxFiles: 8, MaxFileBytes: 4096, MaxTotalBytes: 16384}
	for p, b := range files {
		if err := os.WriteFile(filepath.Join(root, p), []byte(b), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{"go.mod", "trusted_test.go"} {
		role := "trusted_test"
		if p == "go.mod" {
			role = "checker_input"
		}
		b := files[p]
		c.RepositoryContext.Files = append(c.RepositoryContext.Files, loop.RepositoryContextFile{Path: p, Role: role, Bytes: int64(len(b)), Digest: digest([]byte(b))})
	}
	c.RepositoryContext.SourceDigest = RepositorySourceDigest(c.RepositoryContext)
	v, err := ResolveEngineeringContext(ctx, c, "issue task", "issue acceptance", filepath.Join(root, "owned"), root, []string{"value.go", "regression_test.go"}, nil)
	if err != nil || v.SchemaVersion != loop.RepositoryEngineeringSchema || len(c.RepositoryContext.Files) != 2 {
		t.Fatalf("resolution mutated template or failed: %v", err)
	}
	if v.RepositoryContext.Files[3].Role != "regression_test" {
		t.Fatal("regression promoted to trusted")
	}
	for _, paths := range [][]string{{"value.go", "trusted_test.go"}, {"value.go", "missing/value_test.go"}, {"../escape.go", "regression_test.go"}, {"value.go", "value.go"}} {
		if _, err = ResolveEngineeringContext(ctx, c, "issue", "acceptance", root, root, paths, nil); err == nil {
			t.Fatalf("unsafe/overlapping context accepted: %v", paths)
		}
	}
	if err = os.WriteFile(filepath.Join(root, "trusted_test.go"), []byte("package altered\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = ResolveEngineeringContext(ctx, c, "issue", "acceptance", root, root, []string{"value.go", "regression_test.go"}, nil); err == nil {
		t.Fatal("changed trusted input admitted")
	}
}

func TestEngineeringDependencyArchiveIntegrity(t *testing.T) {
	archive := func(name string, kind byte) []byte {
		var b bytes.Buffer
		w := tar.NewWriter(&b)
		h := &tar.Header{Name: name, Mode: 0600, Typeflag: kind}
		if kind == tar.TypeReg {
			h.Size = 3
		}
		if err := w.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if kind == tar.TypeReg {
			w.Write([]byte("abc"))
		}
		w.Close()
		return b.Bytes()
	}
	ctx := context.Background()
	valid := archive("example.test/module/@v/v1.0.0.mod", tar.TypeReg)
	if err := materializeDependencyBundle(ctx, valid, filepath.Join(t.TempDir(), "proxy")); err != nil {
		t.Fatal(err)
	}
	for _, wire := range [][]byte{archive("../escape.mod", tar.TypeReg), archive("example.test/module/@v/v1.0.0.mod", tar.TypeSymlink), archive("unexpected.txt", tar.TypeReg)} {
		if err := materializeDependencyBundle(ctx, wire, filepath.Join(t.TempDir(), "proxy")); err == nil {
			t.Fatal("unsafe archive admitted")
		}
	}
	p := filepath.Join(t.TempDir(), "bundle.tar")
	if err := os.WriteFile(p, valid, 0600); err != nil {
		t.Fatal(err)
	}
	spec := loop.DependencyBundle{Path: p, Digest: digest(valid)}
	if _, err := dependencyBundleBytes(spec); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := dependencyBundleBytes(spec); err == nil {
		t.Fatal("dependency substitution admitted")
	}
}
