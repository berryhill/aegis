package implementation

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// Original-tree regression of the independent candidate1 virtual test.
func TestIndependent349SnapshotRejectsUnlistedModule(t *testing.T) {
	e, c := addContextFixture(t)
	c.RepositoryContext.Files = c.RepositoryContext.Files[:2]
	c.RepositoryContext.MaxFiles = 2
	c.RepositoryContext.SourceDigest = RepositorySourceDigest(c.RepositoryContext)
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := Preflight(c, e.GoBinary); err == nil {
		t.Error("Preflight accepted undeclared module")
	}
	wire, err := contractSnapshot(c)
	if err == nil || bytes.Contains(wire, []byte("go.mod")) {
		t.Fatal("snapshot accepted undeclared module")
	}
}

func TestIndependent349SnapshotRejectsAggregateGrowth(t *testing.T) {
	_, c := addContextFixture(t)
	var total int64
	for _, f := range c.RepositoryContext.Files {
		total += f.Bytes
	}
	c.RepositoryContext.MaxTotalBytes = total
	c.RepositoryContext.MaxFileBytes = total
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.Workspace, "add.go"), []byte(addWrong+"// growth\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := contractSnapshot(c); err == nil {
		t.Fatal("snapshot accepted aggregate growth beyond declared budget")
	}
}
