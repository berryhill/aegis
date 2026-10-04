package app

import (
	"path/filepath"
	"testing"

	"github.com/berryhill/aegis/internal/loop"
)

func TestDoerProtectionIncludesSystemdCustody(t *testing.T) {
	s, _, _, _, _ := hostApprovalFixture(t)
	directory := t.TempDir()
	t.Setenv("CREDENTIALS_DIRECTORY", directory)
	s.Config.Credentials.Authority.Custody = "systemd"
	s.Config.Credentials.Authority.KEKFile = ""
	s.Config.Credentials.Authority.KEKCredential = "controller-kek"
	safe := loop.DoerContract{Task: "Create result", Workspace: t.TempDir(), WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", MaxAttempts: 2}
	if err := s.validateDoerControllerWorkspace(safe); err != nil {
		t.Fatal(err)
	}
	unsafe := safe
	unsafe.Workspace = directory
	unsafe.WritableFiles = []string{"controller-kek"}
	unsafe.VerifyFile = "controller-kek"
	if err := s.validateDoerControllerWorkspace(unsafe); err == nil {
		t.Fatal("future systemd custody target permitted")
	}
	s.Config.Credentials.Authority.KEKCredential = filepath.Join("..", "outside")
	if err := s.validateDoerControllerWorkspace(safe); err == nil {
		t.Fatal("invalid custody name admitted")
	}
	s.Config.Credentials.Authority.KEKCredential = "controller-kek"
	t.Setenv("CREDENTIALS_DIRECTORY", "")
	if err := s.validateDoerControllerWorkspace(safe); err == nil {
		t.Fatal("missing custody directory admitted")
	}
}
