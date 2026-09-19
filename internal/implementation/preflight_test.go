package implementation

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPreflightFailsClosedWithoutExecution(t *testing.T) {
	e, c := fixture(t)
	if err := Preflight(c, e.GoBinary); err != nil {
		t.Fatal(err)
	}
	if err := Preflight(c, filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("missing checker admitted")
	}
	checker := filepath.Join(t.TempDir(), "checker")
	if err := os.WriteFile(checker, []byte("not executed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Preflight(c, checker); err == nil {
		t.Fatal("non executable checker admitted")
	}
	original := c.Workspace
	c.Workspace = filepath.Join(t.TempDir(), "missing")
	if err := Preflight(c, e.GoBinary); err == nil {
		t.Fatal("missing workspace admitted")
	}
	c.Workspace = original
	c.Policy.RequiredTests = nil
	if err := Preflight(c, e.GoBinary); err == nil {
		t.Fatal("missing required tests admitted")
	}
}
