package implementation

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// The reader is shared by source snapshots and full-distribution toolchain
// verification. Exact allocation must preserve binary bytes and custody denial.
func TestRepositoryContextExactSizeRead(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, size := range []int{0, 1, 511, 512, 513, 32768, 1 << 20} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			original := make([]byte, size)
			for i := range original {
				original[i] = byte(i)
			}
			if err := os.WriteFile(filepath.Join(dir, "input"), original, 0600); err != nil {
				t.Fatal(err)
			}
			got, err := readRepositoryContextFile(root, "input", int64(size))
			if err != nil || !bytes.Equal(got, original) {
				t.Fatalf("bytes not preserved: %v", err)
			}
			for _, mismatch := range []int64{-1, int64(size) - 1, int64(size) + 1} {
				if got, err := readRepositoryContextFile(root, "input", mismatch); err == nil || got != nil {
					t.Fatalf("size %d admitted for %d bytes", mismatch, size)
				}
			}
		})
	}
	if err := os.Link(filepath.Join(dir, "input"), filepath.Join(dir, "alias")); err != nil {
		t.Fatal(err)
	}
	if got, err := readRepositoryContextFile(root, "input", 1<<20); err == nil || got != nil {
		t.Fatal("hardlink accepted")
	}
	if err := os.Symlink("input", filepath.Join(dir, "symlink")); err != nil {
		t.Fatal(err)
	}
	if got, err := readRepositoryContextFile(root, "symlink", 1<<20); err == nil || got != nil {
		t.Fatal("symlink accepted")
	}
}
