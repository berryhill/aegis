//go:build linux

package command

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/migration"
	resetdomain "github.com/berryhill/aegis/internal/reset"
)

type migrateCommandFixture struct {
	home, config, state string
	service             *migration.Service
}

func newMigrateCommandFixture(t *testing.T) migrateCommandFixture {
	t.Helper()
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(t.TempDir(), "home")
	if err = os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	state := filepath.Join(home, ".local", "state", "aegis")
	checkpoints := filepath.Join(home, ".local", "state", "aegis-checkpoints")
	configPath := filepath.Join(home, ".config", "aegis", "aegis.yaml")
	for _, directory := range []string{filepath.Dir(configPath), filepath.Join(state, "plans"), checkpoints} {
		if err = os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	document := fmt.Sprintf("state_dir: %q\nprincipal:\n  id: principal\n  name: Principal\n  uid: %q\n  user: %q\n  auth_ttl: 5m\naudit:\n  checkpoint_dir: %q\n", state, current.Uid, current.Username, checkpoints)
	if err = os.WriteFile(configPath, []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(state, "plans", "one.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	copyUser := *current
	service := &migration.Service{Home: func() (string, error) { return home, nil }, Current: func() (*user.User, error) { value := copyUser; return &value, nil }, LookupID: func(uid string) (*user.User, error) {
		if uid != copyUser.Uid {
			return nil, errors.New("unknown uid")
		}
		value := copyUser
		return &value, nil
	}}
	return migrateCommandFixture{home: home, config: configPath, state: state, service: service}
}

func executeMigrate(t *testing.T, fixture migrateCommandFixture, input string, terminal bool) (string, error) {
	t.Helper()
	var output bytes.Buffer
	root := NewRoot(Dependencies{In: strings.NewReader(input), Out: &output, Err: io.Discard, IsTerminal: func(io.Reader, io.Writer) bool { return terminal }, Migrator: fixture.service})
	root.SetArgs([]string{"migrate-layout"})
	err := root.Execute()
	return output.String(), err
}

func TestMigrateLayoutCommandPTYConfirmationAndDecline(t *testing.T) {
	for _, test := range []struct {
		name, input       string
		terminal, success bool
	}{
		{name: "enter-default", input: "\n", terminal: true, success: true},
		{name: "yes", input: "yes\n", terminal: true, success: true},
		{name: "decline", input: "no\n", terminal: true},
		{name: "unrecognized", input: migration.Confirmation + "\n", terminal: true},
		{name: "eof", input: "", terminal: true},
		{name: "non-tty", input: migration.Confirmation + "\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newMigrateCommandFixture(t)
			output, err := executeMigrate(t, fixture, test.input, test.terminal)
			if test.success {
				if err != nil || !strings.Contains(output, `"state": "canonical"`) {
					t.Fatalf("err=%v output=%s", err, output)
				}
				if _, statErr := os.Stat(filepath.Join(fixture.home, ".argis", "aegis.yaml")); statErr != nil {
					t.Fatal(statErr)
				}
				return
			}
			if test.terminal && err != nil {
				t.Fatalf("decline error=%v", err)
			}
			if !test.terminal && err == nil {
				t.Fatal("non-TTY migration accepted")
			}
			if _, statErr := os.Stat(fixture.config); statErr != nil {
				t.Fatalf("source changed: %v", statErr)
			}
			if _, statErr := os.Lstat(filepath.Join(fixture.home, ".argis")); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("destination changed: %v", statErr)
			}
		})
	}
}

func TestIsolatedLegacyMigrateResetAndCanonicalBootstrap(t *testing.T) {
	fixture := newMigrateCommandFixture(t)
	if _, err := executeMigrate(t, fixture, "\n", true); err != nil {
		t.Fatal(err)
	}

	var resetOutput bytes.Buffer
	resetRoot := NewRoot(Dependencies{In: strings.NewReader(resetdomain.Confirmation + "\n"), Out: &resetOutput, Err: io.Discard, IsTerminal: func(io.Reader, io.Writer) bool { return true }})
	resetRoot.SetArgs([]string{"reset"})
	if err := resetRoot.Execute(); err != nil {
		t.Fatal(err)
	}
	if inspection := config.Inspect(""); inspection.State != config.StateAbsent {
		t.Fatalf("post-reset inspection=%+v", inspection)
	}

	var bootstrap bytes.Buffer
	bootstrapRoot := NewRoot(Dependencies{In: strings.NewReader("yes\n/status\n/quit\n"), Out: &bootstrap, Err: io.Discard, Version: "test", IsTerminal: func(io.Reader, io.Writer) bool { return true }})
	if err := bootstrapRoot.Execute(); err != nil {
		t.Fatal(err)
	}
	canonical := filepath.Join(fixture.home, ".argis", "aegis.yaml")
	if inspection := config.Inspect(""); inspection.State != config.StateValid || inspection.Path != canonical {
		t.Fatalf("canonical bootstrap inspection=%+v output=%s", inspection, bootstrap.String())
	}
	for _, forbidden := range []string{filepath.Join(fixture.home, ".cache", "aegis"), filepath.Join(fixture.home, ".config", "aegis", "aegis.yaml"), filepath.Join(fixture.home, ".local", "state", "aegis", "plans", "one.json")} {
		if _, err := os.Lstat(forbidden); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("obsolete local artifact remains at %s: %v", forbidden, err)
		}
	}
}
