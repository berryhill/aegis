package layout

import (
	"os"
	"path/filepath"
	"strings"
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
		"root": filepath.Join(home, ".aegis"), "config": filepath.Join(home, ".aegis", "aegis.yaml"),
		"state": filepath.Join(home, ".aegis", "state"), "checkpoints": filepath.Join(home, ".aegis", "state", "audit-checkpoints"), "authority": filepath.Join(home, ".aegis", "state", "persistence", "authority-v1"),
		"database": filepath.Join(home, ".aegis", "state", "credentials", "authority.db"), "kek": filepath.Join(home, ".aegis", "state", "credentials", "authority.kek"),
		"certifications": filepath.Join(home, ".aegis", "state", "manager", "certifications"), "models": filepath.Join(home, ".aegis", "state", "manager", "ollama-models"), "runtime": filepath.Join(home, ".aegis", "state", "runtime"),
	}
	actual := map[string]string{"root": got.Root, "config": got.Config, "state": got.State, "checkpoints": got.AuditCheckpoints, "authority": got.AuthorityPersistence, "database": got.CredentialDatabase, "kek": got.HostKEK, "certifications": got.ManagerCertifications, "models": got.ManagedModels, "runtime": got.Runtime}
	for name, want := range expected {
		if actual[name] != want {
			t.Fatalf("%s=%q want %q", name, actual[name], want)
		}
	}
}

func TestForRootDerivesCompleteProfileSpecificOperationalLayout(t *testing.T) {
	scope := filepath.Join(t.TempDir(), "scope")
	root := filepath.Join(scope, "profile")
	got := ForRoot(scope, root)
	want := map[string]string{
		"home": scope, "root": root, "config": filepath.Join(root, "aegis.yaml"),
		"state": filepath.Join(root, "state"), "audit checkpoints": filepath.Join(root, "state", "audit-checkpoints"),
		"authority persistence":  filepath.Join(root, "state", "persistence", "authority-v1"),
		"credential database":    filepath.Join(root, "state", "credentials", "authority.db"),
		"host KEK":               filepath.Join(root, "state", "credentials", "authority.kek"),
		"manager certifications": filepath.Join(root, "state", "manager", "certifications"),
		"managed models":         filepath.Join(root, "state", "manager", "ollama-models"),
		"runtime":                filepath.Join(root, "state", "runtime"),
		"legacy config":          filepath.Join(scope, ".argis", "aegis.yaml"),
		"legacy state":           filepath.Join(scope, ".argis", "state"),
		"legacy checkpoints":     filepath.Join(scope, ".argis", "state", "audit-checkpoints"),
	}
	actual := map[string]string{
		"home": got.Home, "root": got.Root, "config": got.Config, "state": got.State,
		"audit checkpoints": got.AuditCheckpoints, "authority persistence": got.AuthorityPersistence,
		"credential database": got.CredentialDatabase, "host KEK": got.HostKEK,
		"manager certifications": got.ManagerCertifications, "managed models": got.ManagedModels,
		"runtime": got.Runtime, "legacy config": got.LegacyConfig, "legacy state": got.LegacyState,
		"legacy checkpoints": got.LegacyCheckpoints,
	}
	for name, expected := range want {
		if actual[name] != expected {
			t.Errorf("%s=%q want %q", name, actual[name], expected)
		}
	}
}

func TestForStateRederivesEveryStateRootedPath(t *testing.T) {
	resolver, _ := temporaryResolver(t)
	resolved, err := resolver.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(t.TempDir(), "custom-state")
	got := resolved.ForState(state)
	for name, value := range map[string]string{
		"state":                  got.State,
		"audit":                  got.AuditCheckpoints,
		"authority":              got.AuthorityPersistence,
		"credential database":    got.CredentialDatabase,
		"host KEK":               got.HostKEK,
		"manager certifications": got.ManagerCertifications,
		"managed models":         got.ManagedModels,
		"runtime":                got.Runtime,
	} {
		if value != state && !strings.HasPrefix(value, state+string(filepath.Separator)) {
			t.Errorf("%s path %q was not rederived from %q", name, value, state)
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
			os.Symlink(target, filepath.Join(home, ".aegis"))
			return home
		}},
		{"writable-root", func(t *testing.T, root string) string {
			home := filepath.Join(root, "home")
			os.Mkdir(home, 0700)
			os.Mkdir(filepath.Join(home, ".aegis"), 0770)
			return home
		}},
		{"non-directory-root", func(t *testing.T, root string) string {
			home := filepath.Join(root, "home")
			os.Mkdir(home, 0700)
			os.WriteFile(filepath.Join(home, ".aegis"), []byte("x"), 0600)
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

func TestFormerProductionRootIsLegacyAndCoexistenceIsAmbiguous(t *testing.T) {
	resolver, _ := temporaryResolver(t)
	resolved, err := resolver.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(resolved.LegacyState, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(resolved.LegacyState, "audit.jsonl"), []byte("legacy"), 0600); err != nil {
		t.Fatal(err)
	}
	discovery, err := resolved.Discover()
	if err != nil || discovery.Presence != Legacy || discovery.LegacyConfig != resolved.LegacyConfig {
		t.Fatalf("former production discovery=%+v err=%v", discovery, err)
	}
	if err = os.MkdirAll(resolved.State, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(resolved.Config, []byte("canonical"), 0600); err != nil {
		t.Fatal(err)
	}
	discovery, err = resolved.Discover()
	if err != nil || discovery.Presence != Ambiguous {
		t.Fatalf("coexistence discovery=%+v err=%v", discovery, err)
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
