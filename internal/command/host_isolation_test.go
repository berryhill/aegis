package command

import (
	"context"
	"errors"
	"fmt"
	"io"

	resetdomain "github.com/berryhill/aegis/internal/reset"
	"github.com/spf13/cobra"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/userservice"
)

// Changing HOME alone does not isolate the running user service manager.
// Every lifecycle fixture must also inject a runner rather than use systemctl.
type isolatedUserServiceRunner struct {
	t            *testing.T
	load, active string
	calls        [][]string
}

func (r *isolatedUserServiceRunner) Run(_ context.Context, args ...string) error {
	r.t.Errorf("unexpected service mutation: %v", args)
	return fmt.Errorf("test forbids service mutation")
}

func (r *isolatedUserServiceRunner) Output(_ context.Context, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	for property, value := range map[string]string{"LoadState": r.load, "ActiveState": r.active} {
		if reflect.DeepEqual(args, []string{"show", userservice.UnitName, "--property", property, "--value"}) {
			return []byte(value + "\n"), nil
		}
	}
	r.t.Errorf("unexpected service query: %v", args)
	return nil, fmt.Errorf("unexpected service query")
}

func absentUserService(t *testing.T) *isolatedUserServiceRunner {
	t.Helper()
	return &isolatedUserServiceRunner{t: t, load: "not-found", active: "inactive"}
}

func isolateLifecycleEnvironment(t *testing.T, home string) {
	t.Helper()
	// Viper accepts AEGIS_* overrides even for an explicit configuration path.
	// Reset also rejects present-but-empty overrides. Setenv registers restoration
	// before Unsetenv removes the variable entirely for the fixture.
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "AEGIS_") {
			t.Setenv(key, "")
			if err := os.Unsetenv(key); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	if runtime.GOOS != "windows" {
		bin := t.TempDir()
		// Catch accidental removal of an injected runner without contacting the host.
		script := "#!/bin/sh\nprintf invoked > \"$0.invoked\"\nexit 99\n"
		path := filepath.Join(bin, "systemctl")
		marker := path + ".invoked"
		if err := os.WriteFile(path, []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
		t.Cleanup(func() {
			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("fixture invoked external systemctl: %v", err)
			}
		})
	}
}

func isolatedResetPurger(t *testing.T) resetGatewayPurger {
	t.Helper()
	runner := absentUserService(t)
	return func(ctx context.Context, configPath string) (bool, error) {
		executable, err := os.Executable()
		if err != nil {
			return false, err
		}
		return userservice.PurgeForReset(ctx, executable, configPath, runner)
	}
}

func TestResetAbsentUnitWithLoadedManagerPreservesState(t *testing.T) {
	for _, active := range []string{"active", "inactive"} {
		t.Run(active, func(t *testing.T) {
			fixture := newResetCommandFixture(t, true)
			runner := &isolatedUserServiceRunner{t: t, load: "loaded", active: active}
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			purge := func(ctx context.Context, path string) (bool, error) {
				return userservice.PurgeForReset(ctx, executable, path, runner)
			}
			command := resetCmdWithHooks(fixture.service, func(io.Reader, io.Writer) bool { return true }, &rootOptions{configFile: fixture.config}, ProductionProfile, func(*cobra.Command, resetdomain.Plan) error { return nil }, purge)
			command.SetIn(strings.NewReader("yes\n"))
			command.SetOut(io.Discard)
			command.SetErr(io.Discard)
			before, err := os.ReadFile(fixture.config)
			if err != nil {
				t.Fatal(err)
			}
			if err := command.Execute(); !errors.Is(err, userservice.ErrForeignUnit) {
				t.Fatalf("loaded foreign manager accepted: %v", err)
			}
			if len(runner.calls) != 2 {
				t.Fatalf("queries=%v", runner.calls)
			}
			after, err := os.ReadFile(fixture.config)
			if err != nil || string(before) != string(after) {
				t.Fatalf("config changed: %v", err)
			}
			artifact, err := os.ReadFile(filepath.Join(fixture.state, "plans", "one.json"))
			if err != nil || string(artifact) != "{}" {
				t.Fatalf("state changed: %v", err)
			}
		})
	}
}

func TestResetFixtureIgnoresHostUnitAndEnvironment(t *testing.T) {
	host := t.TempDir()
	unit := filepath.Join(host, "xdg", "systemd", "user", userservice.UnitName)
	if err := os.MkdirAll(filepath.Dir(unit), 0700); err != nil {
		t.Fatal(err)
	}
	const foreign = "[Service]\nExecStart=/test-owned/foreign\n"
	if err := os.WriteFile(unit, []byte(foreign), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", host)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(host, "xdg"))
	t.Setenv("AEGIS_STATE_DIR", filepath.Join(host, "foreign-state"))
	fixture := newResetCommandFixture(t, true)
	present, err := userservice.UnitPresent()
	if err != nil || present {
		t.Fatalf("fixture inherited host unit: present=%t err=%v", present, err)
	}
	if os.Getenv("HOME") != fixture.home || os.Getenv("XDG_CONFIG_HOME") != filepath.Join(fixture.home, ".config") || os.Getenv("AEGIS_STATE_DIR") != "" {
		t.Fatal("fixture inherited host environment")
	}
	if _, err := executeReset(t, fixture, "yes\n", true, nil); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(unit)
	if err != nil || string(contents) != foreign {
		t.Fatalf("foreign unit changed: %v", err)
	}
}
