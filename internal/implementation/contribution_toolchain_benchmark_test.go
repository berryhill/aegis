package implementation

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Compare every manifest hash against the non-reusing source reader, including
// differently sized adjacent files in the real distribution.
func TestToolchainScratchManifestBytes(t *testing.T) {
	driver, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	driver, err = filepath.EvalSymlinks(driver)
	if err != nil {
		t.Fatal(err)
	}
	m, _, err := freezeToolchain(context.Background(), driver)
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(m.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	// Independently enumerate the old WalkDir inventory as well as rereading
	// every file through the original full-path reader. Missing/extra entries,
	// traversal ordering, modes and bytes must preserve the exact manifest.
	expected := m
	expected.Files = nil
	err = filepath.WalkDir(m.Root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(m.Root, p)
		if err != nil {
			return err
		}
		if d.IsDir() && (rel == "pkg/mod" || rel == "pkg/sumdb" || rel == "doc" || rel == "test" || rel == "api" || rel == "misc") {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		b, err := readRepositoryContextFile(root, rel, info.Size())
		if err != nil {
			return err
		}
		expected.Files = append(expected.Files, toolchainFile{rel, uint32(info.Mode().Perm()), digest(b)})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(expected)
	got, _ := json.Marshal(m)
	if !bytes.Equal(want, got) {
		t.Fatal("fresh traversal changed full manifest bytes")
	}
}

// Measures a complete fresh distribution read; no manifest or digest is reused.
func BenchmarkFreezeNativeToolchain(b *testing.B) {
	driver, err := exec.LookPath("go")
	if err != nil {
		b.Fatal(err)
	}
	driver, err = filepath.EvalSymlinks(driver)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := freezeToolchain(context.Background(), driver); err != nil {
			b.Fatal(err)
		}
	}
}
