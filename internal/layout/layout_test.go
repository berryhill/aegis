package layout

import (
	"os"
	"path/filepath"
	"testing"
)

func temporaryResolver(t *testing.T) (Resolver, string) {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	return Resolver{Home: func() (string, error) { return home, nil }, EUID: os.Geteuid}, home
}

func TestLiteralCanonicalLayoutAndXDGIndependence(t *testing.T) {
	resolver, home := temporaryResolver(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg-config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "xdg-state"))
	got, err := resolver.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{
		"root": filepath.Join(home, ".argis"), "config": filepath.Join(home, ".argis", "aegis.yaml"),
		"state": filepath.Join(home, ".argis", "state"), "checkpoints": filepath.Join(home, ".argis", "state", "audit-checkpoints"),
		"database": filepath.Join(home, ".argis", "state", "credentials", "authority.db"), "kek": filepath.Join(home, ".argis", "state", "credentials", "authority.kek"),
		"certifications": filepath.Join(home, ".argis", "state", "manager", "certifications"), "models": filepath.Join(home, ".argis", "state", "manager", "ollama-models"), "runtime": filepath.Join(home, ".argis", "state", "runtime"),
	}
	actual := map[string]string{"root": got.Root, "config": got.Config, "state": got.State, "checkpoints": got.AuditCheckpoints, "database": got.CredentialDatabase, "kek": got.HostKEK, "certifications": got.ManagerCertifications, "models": got.ManagedModels, "runtime": got.Runtime}
	for name, want := range expected {
		if actual[name] != want {
			t.Fatalf("%s=%q want %q", name, actual[name], want)
		}
	}
}

func TestUnsafeHomesAndCanonicalRootsFailClosed(t *testing.T) {
	for _, test := range []struct {
		name    string
		prepare func(t *testing.T, root string) string
	}{
		{"relative", func(*testing.T, string) string { return "relative" }},
		{"root", func(*testing.T, string) string { return string(filepath.Separator) }},
		{"symlink-home", func(t *testing.T, root string) string {
			real := filepath.Join(root, "real")
			os.Mkdir(real, 0700)
			link := filepath.Join(root, "link")
			os.Symlink(real, link)
			return link
		}},
		{"symlink-root", func(t *testing.T, root string) string {
			home := filepath.Join(root, "home")
			os.Mkdir(home, 0700)
			target := filepath.Join(root, "target")
			os.Mkdir(target, 0700)
			os.Symlink(target, filepath.Join(home, ".argis"))
			return home
		}},
		{"writable-root", func(t *testing.T, root string) string {
			home := filepath.Join(root, "home")
			os.Mkdir(home, 0700)
			os.Mkdir(filepath.Join(home, ".argis"), 0770)
			return home
		}},
		{"non-directory-root", func(t *testing.T, root string) string {
			home := filepath.Join(root, "home")
			os.Mkdir(home, 0700)
			os.WriteFile(filepath.Join(home, ".argis"), []byte("x"), 0600)
			return home
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			base := t.TempDir()
			home := test.prepare(t, base)
			if _, err := (Resolver{Home: func() (string, error) { return home, nil }, EUID: os.Geteuid}).Resolve(); err == nil {
				t.Fatal("unsafe layout accepted")
			}
		})
	}
}

func TestWrongOwnerIdentityFailsClosed(t *testing.T) {
	home := filepath.Join(t.TempDir(), "wrong-owner-home")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := (Resolver{Home: func() (string, error) { return home, nil }, EUID: func() int { return os.Geteuid() + 1 }}).Resolve(); err == nil {
		t.Fatal("ambiguously owned home accepted")
	}
}

func TestDiscoveryStatesAndEmptyRetainedLegacyRoots(t *testing.T) {
	resolver, _ := temporaryResolver(t)
	resolved, err := resolver.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if d, _ := resolved.Discover(); d.Presence != None {
		t.Fatalf("fresh=%+v", d)
	}
	if err = os.MkdirAll(resolved.LegacyState, 0700); err != nil {
		t.Fatal(err)
	}
	if d, _ := resolved.Discover(); d.Presence != None {
		t.Fatalf("empty retained legacy root counted: %+v", d)
	}
	if err = os.WriteFile(filepath.Join(resolved.LegacyState, "audit.jsonl"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if d, _ := resolved.Discover(); d.Presence != Legacy {
		t.Fatalf("legacy=%+v", d)
	}
	if err = os.Mkdir(resolved.Root, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(resolved.Root, "unknown"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if d, _ := resolved.Discover(); d.Presence != Ambiguous {
		t.Fatalf("ambiguous=%+v", d)
	}
}

func TestEmptyCanonicalRootAndPreservedModelsAreNotInstallations(t *testing.T) {
	resolver, _ := temporaryResolver(t)
	resolved, err := resolver.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(resolved.State, 0700); err != nil {
		t.Fatal(err)
	}
	discovery, err := resolved.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if discovery.Presence != None {
		t.Fatalf("presence=%s", discovery.Presence)
	}
	if err = os.MkdirAll(resolved.ManagedModels, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(resolved.ManagedModels, "blob"), []byte("model"), 0600); err != nil {
		t.Fatal(err)
	}
	discovery, err = resolved.Discover()
	if err != nil || discovery.Presence != None {
		t.Fatalf("preserved model discovery=%+v err=%v", discovery, err)
	}
}
