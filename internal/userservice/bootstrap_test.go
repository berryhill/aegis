package userservice

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestServiceFixtureIsolatesUnitPath(t *testing.T) {
	inherited := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", inherited)
	exe, cfg := serviceFixture(t)
	privateConfig := filepath.Join(filepath.Dir(exe), "config")
	if got := os.Getenv("XDG_CONFIG_HOME"); got != privateConfig {
		t.Fatalf("fixture inherited external config home: got %q, want %q", got, privateConfig)
	}
	p, err := Preview(exe, cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(privateConfig, "systemd", "user", UnitName)
	if p.UnitPath != want {
		t.Fatalf("unit escaped fixture's private temp: got %q, want %q", p.UnitPath, want)
	}
}

func TestDiscoverInstalledExecutable(t *testing.T) {
	exe, cfg := serviceFixture(t)
	p, err := Preview(exe, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(p.UnitPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.UnitPath, p.unit, 0600); err != nil {
		t.Fatal(err)
	}
	r := &recordingRunner{fragmentPath: p.UnitPath, execStart: loadedExecStartFixture(exe, cfg)}
	got, err := DiscoverInstalledExecutable(context.Background(), r, cfg)
	if err != nil || got != exe {
		t.Fatalf("got %q: %v", got, err)
	}
	r.execStart = loadedExecStartFixture("/foreign", cfg)
	if _, err := DiscoverInstalledExecutable(context.Background(), r, cfg); err == nil {
		t.Fatal("accepted foreign loaded executable")
	}
	r.execStart = loadedExecStartFixture(exe, cfg)
	if err := os.WriteFile(p.UnitPath, append(p.unit, []byte("# foreign\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := DiscoverInstalledExecutable(context.Background(), r, cfg); err == nil {
		t.Fatal("accepted foreign unit")
	}
}
