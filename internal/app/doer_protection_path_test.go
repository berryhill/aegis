package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/berryhill/aegis/internal/loop"
)

func TestDoerProtectionResolvesPATHExecutable(t *testing.T) {
	s, _, _, _, _ := hostApprovalFixture(t)
	bin := t.TempDir()
	executable := filepath.Join(bin, "hermes")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	s.Config.HermesExecutable = "hermes"
	workspace := t.TempDir()
	safe := loop.DoerContract{Task: "Create result.txt", Workspace: workspace, WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", MaxAttempts: 2}
	if err := s.validateDoerControllerWorkspace(safe); err != nil {
		t.Fatalf("ordinary PATH executable blocked safe workspace: %v", err)
	}
	unsafe := loop.DoerContract{Task: "Replace executable", Workspace: bin, WritableFiles: []string{"hermes"}, VerifyFile: "hermes", MaxAttempts: 2}
	if err := s.validateDoerControllerWorkspace(unsafe); err == nil {
		t.Fatal("PATH-selected executable writable")
	}
	s.Config.HermesExecutable = "missing-controller-helper"
	if err := s.validateDoerControllerWorkspace(safe); err == nil {
		t.Fatal("unresolved executable protection accepted")
	}
}
