package authority

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestClassifyLegacyAuthority(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	if got, err := ClassifyLegacyAuthority(state); err != nil || got.State != CollisionAbsent {
		t.Fatalf("absent=%+v err=%v", got, err)
	}
	if err := os.MkdirAll(filepath.Join(state, "mandates", "retained-empty"), 0700); err != nil {
		t.Fatal(err)
	}
	if got, err := ClassifyLegacyAuthority(state); err != nil || got.State != CollisionEmpty {
		t.Fatalf("empty=%+v err=%v", got, err)
	}
	if err := os.WriteFile(filepath.Join(state, "mandates", "record.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := ClassifyLegacyAuthority(state); err == nil || got.State != CollisionUnsafe {
		t.Fatalf("populated=%+v err=%v", got, err)
	}
}

func TestClassifyLegacyAuthorityAllowsCanonicalOperationalSessions(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(filepath.Join(state, "sessions"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "sessions", "session.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := ClassifyLegacyAuthority(state); err != nil || got.State != CollisionAbsent {
		t.Fatalf("canonical operational sessions collided with Badger authority: classification=%+v err=%v", got, err)
	}
}

func TestClassifyLegacyAuthorityRejectsSymlinkAndInsecureMode(t *testing.T) {
	for _, test := range []struct {
		name    string
		prepare func(string) error
	}{
		{"symlink", func(state string) error {
			target := filepath.Join(filepath.Dir(state), "target")
			if err := os.Mkdir(target, 0700); err != nil {
				return err
			}
			return os.Symlink(target, filepath.Join(state, "authority-contexts"))
		}},
		{"mode", func(state string) error { return os.Mkdir(filepath.Join(state, "authority-contexts"), 0750) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := filepath.Join(t.TempDir(), "state")
			if err := os.Mkdir(state, 0700); err != nil {
				t.Fatal(err)
			}
			if err := test.prepare(state); err != nil {
				t.Fatal(err)
			}
			got, err := ClassifyLegacyAuthority(state)
			if err == nil || got.State != CollisionUnsafe || errors.Is(err, os.ErrNotExist) {
				t.Fatalf("classification=%+v err=%v", got, err)
			}
		})
	}
}

func TestClassifyLegacyAuthorityChecksEveryLegacySurface(t *testing.T) {
	for _, name := range LegacyDirectories {
		t.Run(name, func(t *testing.T) {
			state := filepath.Join(t.TempDir(), "state")
			if err := os.MkdirAll(filepath.Join(state, name), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(state, name, "retained.json"), []byte("{}"), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := ClassifyLegacyAuthority(state)
			if err == nil || got.State != CollisionUnsafe {
				t.Fatalf("surface %s classification=%+v err=%v", name, got, err)
			}
		})
	}
}
