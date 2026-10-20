//go:build linux

package config

import (
	"bytes"
	"os"
	"os/user"
	"path/filepath"
	"testing"
)

func TestControllerCodexConfigPreservationAndDenials(t *testing.T) {
	u, e := user.Current()
	if e != nil {
		t.Fatal(e)
	}
	root, e := os.MkdirTemp(u.HomeDir, ".aegis-config-connection-test-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(root)
	original := []byte("# keep exactly\nprincipal:\n  id: operator\n  uid: '" + u.Uid + "'\n  user: '" + u.Username + "'\ncredentials:\n  provider_auth: {}\n# trailing comment\nretention:\n  design_homes: false\n")
	binding := ControllerCodexLogin{ProfileHome: filepath.Join(u.HomeDir, ".hermes", "profiles", "synthetic"), AccountID: "synthetic-account", ApprovedBy: "operator"}
	for _, kind := range []string{"valid", "changed", "unsafe", "symlink", "unknown", "competing"} {
		t.Run(kind, func(t *testing.T) {
			dir := filepath.Join(root, kind)
			os.Mkdir(dir, 0700)
			path := filepath.Join(dir, "aegis.yaml")
			os.WriteFile(path, original, 0600)
			expected, e := ConnectionSnapshot(path)
			if e != nil {
				t.Fatal(e)
			}
			switch kind {
			case "changed":
				os.WriteFile(path, append(original, []byte("# changed\n")...), 0600)
			case "unsafe":
				os.Chmod(path, 0644)
			case "symlink":
				os.Rename(path, path+".real")
				os.Symlink(path+".real", path)
			case "unknown":
				os.WriteFile(path, append(original, []byte("unknown: true\n")...), 0600)
				expected, _ = os.ReadFile(path)
			case "competing":
				os.WriteFile(path, bytes.Replace(original, []byte("provider_auth: {}"), []byte("provider_auth:\n    codex:\n      type: environment\n      source_env: SYNTHETIC\n      target_env: AEGIS_CODEX_ACCESS_TOKEN"), 1), 0600)
				expected, _ = os.ReadFile(path)
			}
			e = ConnectControllerCodex(path, expected, binding)
			if kind != "valid" {
				if e == nil {
					t.Fatal("unsafe update admitted")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(after, original) {
				t.Fatal("owning config changed")
			}
			actual, e := ReadControllerCodexBinding(path)
			if e != nil || actual == nil || *actual != binding {
				t.Fatal("typed readback failed", e)
			}
			beforeRecord, _ := os.ReadFile(path + connectionSuffix)
			if ConnectControllerCodex(path, expected, binding) == nil {
				t.Fatal("existing record replaced")
			}
			afterRecord, _ := os.ReadFile(path + connectionSuffix)
			if !bytes.Equal(beforeRecord, afterRecord) {
				t.Fatal("record overwritten")
			}
			os.WriteFile(path, append(append([]byte{}, original...), []byte("# operator edit\n")...), 0600)
			if _, e := ReadControllerCodexBinding(path); e == nil {
				t.Fatal("config drift ignored")
			}

		})
	}
}

// Inject an uncooperative owner write at the actual publication boundary, AFTER
// the final reviewed-byte check. Exercise real no-replace publication/readback;
// this reproduces the formerly unsafe schedule, not only pre-check drift.
func TestControllerCodexConcurrentPublicationPreservesOwnerEdit(t *testing.T) {
	u, e := user.Current()
	if e != nil {
		t.Fatal(e)
	}
	root, e := os.MkdirTemp(u.HomeDir, ".aegis-connection-boundary-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(root)
	path := filepath.Join(root, "aegis.yaml")
	original := []byte("principal:\n  id: operator\n  uid: '" + u.Uid + "'\n  user: '" + u.Username + "'\n")
	if e := os.WriteFile(path, original, 0600); e != nil {
		t.Fatal(e)
	}
	edited := append(append([]byte{}, original...), []byte("# CONCURRENT_OPERATOR_EDIT\n")...)
	binding := ControllerCodexLogin{ProfileHome: filepath.Join(u.HomeDir, ".hermes", "profiles", "synthetic"), AccountID: "synthetic-account", ApprovedBy: "operator"}
	called := false
	err := connectControllerCodex(path, original, binding, func(fd int, staged, target string) error {
		called = true
		if e := os.WriteFile(path, edited, 0600); e != nil {
			t.Fatal(e)
		}
		return publishControllerConnection(fd, staged, target)
	})
	if !called || err == nil {
		t.Fatal("publication boundary drift not denied", err)
	}
	after, e := os.ReadFile(path)
	if e != nil || !bytes.Equal(after, edited) {
		t.Fatal("concurrent owning config data lost", e)
	}
	if _, e := ReadControllerCodexBinding(path); e == nil {
		t.Fatal("stale operational binding resolved")
	}
}

func TestControllerCodexLegacyConfigWithoutRecord(t *testing.T) {
	u, e := user.Current()
	if e != nil {
		t.Fatal(e)
	}
	root, e := os.MkdirTemp(u.HomeDir, ".aegis-connection-legacy-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(root)
	path := filepath.Join(root, "config.yaml")
	original := "principal:\n  id: operator\n  uid: '" + u.Uid + "'\n  user: '" + u.Username + "'\n"
	for _, kind := range []string{"none", "environment", "native"} {
		t.Run(kind, func(t *testing.T) {
			text := original
			if kind == "environment" {
				text += "credentials:\n  provider_auth:\n    codex:\n      type: environment\n      source_env: SYNTHETIC\n      target_env: AEGIS_CODEX_ACCESS_TOKEN\n"
			}
			if kind == "native" {
				text += "credentials:\n  controller_codex_login:\n    profile_home: '" + filepath.Join(u.HomeDir, ".hermes", "profiles", "synthetic") + "'\n    account_id: synthetic-account\n    approved_by: operator\n"
			}
			if e := os.WriteFile(path, []byte(text), 0600); e != nil {
				t.Fatal(e)
			}
			binding, e := ReadControllerCodexBinding(path)
			if e != nil {
				t.Fatal(e)
			}
			if kind == "native" {
				if binding == nil || binding.AccountID != "synthetic-account" {
					t.Fatal("legacy native binding changed")
				}
			} else if binding != nil {
				t.Fatal("unexpected native binding")
			}
		})
	}
}

func TestControllerCodexRecordSafety(t *testing.T) {
	u, e := user.Current()
	if e != nil {
		t.Fatal(e)
	}
	root, e := os.MkdirTemp(u.HomeDir, ".aegis-connection-safety-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(root)
	for _, kind := range []string{"mode", "symlink", "hardlink", "malformed", "unknown", "parent", "public-parent", "legacy-conflict"} {
		t.Run(kind, func(t *testing.T) {
			dir := filepath.Join(root, kind)
			if e := os.Mkdir(dir, 0700); e != nil {
				t.Fatal(e)
			}
			path := filepath.Join(dir, "config.yaml")
			original := []byte("principal:\n  id: operator\n  uid: '" + u.Uid + "'\n  user: '" + u.Username + "'\n")
			if e := os.WriteFile(path, original, 0600); e != nil {
				t.Fatal(e)
			}
			binding := ControllerCodexLogin{ProfileHome: filepath.Join(u.HomeDir, ".hermes", "profiles", "synthetic"), AccountID: "synthetic-account", ApprovedBy: "operator"}
			if e := ConnectControllerCodex(path, original, binding); e != nil {
				t.Fatal(e)
			}
			record := path + connectionSuffix
			switch kind {
			case "mode":
				os.Chmod(record, 0644)
			case "symlink":
				os.Rename(record, record+".real")
				os.Symlink(record+".real", record)
			case "hardlink":
				os.Link(record, record+".link")
			case "malformed":
				os.WriteFile(record, []byte("{}"), 0600)
			case "unknown":
				b, _ := os.ReadFile(record)
				os.WriteFile(record, append(b, []byte("{}")...), 0600)
			case "parent":
				os.Chmod(dir, 0777)
			case "public-parent":
				os.Chmod(dir, 0755)
			case "legacy-conflict":
				os.WriteFile(path, append(original, []byte("credentials:\n  controller_codex_login:\n    profile_home: '"+binding.ProfileHome+"'\n    account_id: synthetic-account\n    approved_by: operator\n")...), 0600)
			}
			if _, e := ReadControllerCodexBinding(path); e == nil {
				t.Fatal("unsafe/conflicting record admitted")
			}
		})
	}
}
