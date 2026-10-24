package implementation

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestContextSnapshotDoesNotCrawlPrivateFiles(t *testing.T) {
	e, c := addContextFixture(t)
	// A legacy whole-workspace crawl would reject this undeclared symlink.
	// Declared custody must neither follow it nor inspect unrelated private bytes.
	if err := os.WriteFile(filepath.Join(c.Workspace, "private.txt"), []byte("UNDECLARED_PRIVATE_SENTINEL"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("private.txt", filepath.Join(c.Workspace, "private-link")); err != nil {
		t.Fatal(err)
	}
	if err := Preflight(c, e.GoBinary); err != nil {
		t.Fatal(err)
	}
	wire, err := contractSnapshot(c)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(wire, []byte("private")) || bytes.Contains(wire, []byte("UNDECLARED_PRIVATE_SENTINEL")) {
		t.Fatal("private data entered custody")
	}
	if !bytes.Contains(wire, []byte("go.mod")) {
		t.Fatal("checker module input absent")
	}
}
