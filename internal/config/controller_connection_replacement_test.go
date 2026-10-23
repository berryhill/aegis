//go:build linux

package config

import (
	"bytes"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

func TestControllerCodexReplacementPreservesHistoryAndConfig(t *testing.T) {
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(u.HomeDir, ".aegis-connection-replace-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	path := filepath.Join(root, "owner.yaml")
	original := []byte("# unchanged configuration\nprincipal:\n  id: operator\n  uid: '" + u.Uid + "'\n  user: '" + u.Username + "'\n")
	if err = os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	old := ControllerCodexLogin{ProfileHome: filepath.Join(u.HomeDir, ".hermes"), CredentialID: "nc-" + strings.Repeat("a", 64), AccountID: "original-account", ApprovedBy: "operator"}
	next := old
	next.CredentialID = "nc-" + strings.Repeat("b", 64)
	next.AccountID = "replacement-account"
	if err = ConnectControllerCodex(path, original, old); err != nil {
		t.Fatal(err)
	}
	originalSidecar, err := os.ReadFile(path + connectionSuffix)
	if err != nil {
		t.Fatal(err)
	}
	if err = ReplaceControllerCodex(path, original, old, next); err != nil {
		t.Fatal(err)
	}
	actual, err := ReadControllerCodexBinding(path)
	if err != nil || actual == nil || *actual != next {
		t.Fatal("replacement readback", err)
	}
	configBytes, _ := os.ReadFile(path)
	retained, _ := os.ReadFile(path + connectionSuffix)
	if !bytes.Equal(configBytes, original) || !bytes.Equal(retained, originalSidecar) {
		t.Fatal("configuration or original connection overwritten")
	}
	if ReplaceControllerCodex(path, original, old, next) == nil {
		t.Fatal("stale reviewed predecessor accepted")
	}
	third := next
	third.CredentialID = "nc-" + strings.Repeat("c", 64)
	if err = ReplaceControllerCodex(path, original, next, third); err != nil {
		t.Fatal(err)
	}
	actual, err = ReadControllerCodexBinding(path)
	if err != nil || actual == nil || *actual != third {
		t.Fatal("second replacement readback", err)
	}
	// Unknown, gapped or tampered history never falls back to an old login.
	bad := path + connectionSuffix + ".replacement-000004"
	if err = os.WriteFile(bad, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = ReadControllerCodexBinding(path); err == nil {
		t.Fatal("gapped history ignored")
	}
}
