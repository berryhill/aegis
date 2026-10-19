//go:build linux

package command

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	selfupdate "github.com/berryhill/aegis/internal/update"
)

// Exercise the public constructor and Execute, not the bootstrap helper. Missing
// installed unit is an intentional coordinator boundary: never run a service or
// fetch a release, and never inspect the real operator's profile.
func TestInstalledUpdatePublicProfileRouting(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home")
	repo := filepath.Join(home, "repository")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module github.com/berryhill/aegis\n\ngo 1.26\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	canonical := filepath.Join(home, ".aegis", "aegis.yaml")
	for _, tc := range []struct {
		name string
		args []string
		deny bool
	}{
		{"explicit", []string{"update", "--installed", "--config", canonical}, false},
		{"default", []string{"update", "--installed"}, false},
		{"ordinary", []string{"update", "--config", canonical}, true},
		{"check", []string{"update", "--installed", "--check"}, true},
		{"target", []string{"update", "--installed", "--target", "http://localhost"}, true},
		{"state", []string{"update", "--installed", "--state-dir", filepath.Join(home, "state")}, true},
		{"runtime", []string{"update", "--installed", "--runtime", "hermes"}, true},
		{"hermes", []string{"update", "--installed", "--hermes-executable", "/untrusted/hermes"}, true},
		{"root-action", []string{"update", "--installed", "--update"}, true},
		{"other-config", []string{"update", "--installed", "--config", filepath.Join(repo, ".aegis", "aegis.yaml")}, true},
		{"false-is-ordinary", []string{"update", "--installed=false", "--config", canonical}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := NewRoot(Dependencies{In: strings.NewReader(""), Out: io.Discard, Err: io.Discard, Version: "dev", Profile: DevelopmentProfile, DevelopmentRoot: repo, Updater: selfupdate.New("dev")})
			root.SetArgs(tc.args)
			err := root.Execute()
			if err == nil {
				t.Fatal("expected hermetic boundary denial")
			}
			if tc.deny {
				if ExitCode(err) != 2 {
					t.Fatalf("must deny before coordinator: %v", err)
				}
			} else {
				var pathErr *os.PathError
				if !errors.As(err, &pathErr) || !strings.HasSuffix(pathErr.Path, "aegis.service") {
					t.Fatalf("did not reach exact installed-unit discovery: %v", err)
				}
				got, _ := root.PersistentFlags().GetString("config")
				if got != canonical {
					t.Fatalf("config=%q want canonical %q", got, canonical)
				}
			}
		})
	}
}
