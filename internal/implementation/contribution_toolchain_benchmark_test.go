package implementation

import (
	"context"
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
	for _, entry := range m.Files {
		info, err := root.Lstat(entry.Path)
		if err != nil {
			t.Fatal(err)
		}
		b, err := readRepositoryContextFile(root, entry.Path, info.Size())
		if err != nil || digest(b) != entry.Digest || uint32(info.Mode().Perm()) != entry.Mode {
			t.Fatalf("scratch changed manifest for %s: %v", entry.Path, err)
		}
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
