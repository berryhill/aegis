package skillbundle

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchiveExtractionMemberBudget(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "many.tar.gz")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	for i := 0; i < 2049; i++ {
		if err := tw.WriteHeader(&tar.Header{Name: fmt.Sprintf("aegis-skills-v0.2.18/skills/file-%d.md", i), Mode: 0644, Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyArchive(archive, strings.Repeat("a", 40)); err == nil || !strings.Contains(err.Error(), "archive_extraction_limit") {
		t.Fatalf("member budget denial: %v", err)
	}
}
