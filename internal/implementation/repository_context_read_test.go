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
func TestRepositoryContextReusedBufferFreshRead(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	var scratch []byte
	for _, size := range []int{1 << 20, 0, 17, 1 << 21, 1} {
		want := bytes.Repeat([]byte{byte(size + 31)}, size)
		if err := os.WriteFile(filepath.Join(dir, "input"), want, 0600); err != nil {
			t.Fatal(err)
		}
		got, err := readRepositoryContextFileBuffer(root, "input", int64(size), scratch)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("fresh %d-byte read: %v", size, err)
		}
		if cap(scratch) >= size && size > 0 && &got[0] != &scratch[:cap(scratch)][0] {
			t.Fatal("scratch not reused")
		}
		scratch = got
		// A same-size replacement must be reread even with the previous buffer.
		for i := range want {
			want[i] ^= 0xff
		}
		if err := os.WriteFile(filepath.Join(dir, "input"), want, 0600); err != nil {
			t.Fatal(err)
		}
		got, err = readRepositoryContextFileBuffer(root, "input", int64(size), scratch)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("replacement %d-byte read: %v", size, err)
		}
		if got, err := readRepositoryContextFileBuffer(root, "input", int64(size)+1, scratch); err == nil || got != nil {
			t.Fatal("size mismatch accepted")
		}
	}
	if err := os.Link(filepath.Join(dir, "input"), filepath.Join(dir, "alias")); err != nil {
		t.Fatal(err)
	}
	if got, err := readRepositoryContextFileBuffer(root, "input", 1, scratch); err == nil || got != nil {
		t.Fatal("reused buffer admitted hardlink")
	}
	if err := os.Symlink("input", filepath.Join(dir, "symlink")); err != nil {
		t.Fatal(err)
	}
	if got, err := readRepositoryContextFileBuffer(root, "symlink", 1, scratch); err == nil || got != nil {
		t.Fatal("reused buffer admitted symlink")
	}
}

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
