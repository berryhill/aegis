//go:build linux

package hermes

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
)

func TestInstalledNativeActivationDenials(t *testing.T) {
	install, generation := os.Getenv("AEGIS_TEST_INSTALLED_NATIVE_SOURCE"), os.Getenv("AEGIS_TEST_INSTALLED_NATIVE_GENERATION")
	if install == "" || generation == "" {
		t.Skip("isolated PM fixture required")
	}
	d := core.RuntimeDescriptor{Version: "0.23.0", Installation: install}
	manifest := filepath.Join(generation, "activation.json")
	original, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"missing", "[]", `{}`} {
		t.Run(value, func(t *testing.T) {
			defer func() {
				if err := os.WriteFile(manifest, original, info.Mode()); err != nil {
					t.Fatal(err)
				}
			}()
			if value == "missing" {
				err = os.Remove(manifest)
			} else {
				err = os.WriteFile(manifest, []byte(value), info.Mode())
			}
			if err != nil {
				t.Fatal(err)
			}
			if !errors.Is(qualifyProviderTransport(d), ErrProviderAuthRuntimeUnqualified) {
				t.Fatal("manifest drift qualified")
			}
			// If auth is opened first this impossible source returns Unauthorized,
			// not the activation gate. No auth/profile bytes are supplied at all.
			_, err = runNativeCodexProtocol(context.Background(), d, "/nonexistent-source", "resolve", "")
			if !errors.Is(err, errGatewayCommand) {
				t.Fatal("activation did not precede custody", err)
			}
		})
	}
	// Replace, never edit an inode that a PM installer may have hardlinked.
	dependency := filepath.Join(generation, "venv/lib/python3.14/site-packages/openai/__init__.py")
	data, err := os.ReadFile(dependency)
	if err != nil {
		t.Fatal(err)
	}
	saved := filepath.Join(generation, "aegis-negative-saved")
	if err = os.Rename(dependency, saved); err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if e := os.Rename(saved, dependency); e != nil {
				t.Fatal(e)
			}
		}()
		if e := os.WriteFile(dependency, append(data, []byte("\n# synthetic dependency drift\n")...), 0600); e != nil {
			t.Fatal(e)
		}
		if !errors.Is(qualifyProviderTransport(d), ErrProviderAuthRuntimeUnqualified) {
			t.Fatal("dependency drift qualified")
		}
	}()
	if err = qualifyProviderTransport(d); err != nil {
		t.Fatal("restored fixture denied", err)
	}
	probeHome := t.TempDir()
	marker := filepath.Join(probeHome, "started")
	code := "import os, time; from pathlib import Path; Path(" + controllerCodeLiteral(marker) + ").write_text(str(os.getpid())); time.sleep(60)"
	launch, err := discoverRuntimeCommand(context.Background(), d, probeHome, code, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		out, _, err := runBoundedRuntime(ctx, launch, "/nonexistent", probeHome, 1024)
		if len(out) != 0 {
			err = errors.New("cancellation retained output")
		}
		done <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	var pid int
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(marker); err == nil {
			pid, _ = strconv.Atoi(string(data))
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if heldInstalledLeases(t, generation) == 0 {
		t.Fatal("running child lacks real PM lease")
	}
	cancel()
	err = <-done
	if pid <= 0 || !errors.Is(err, context.Canceled) || !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
		t.Fatal("actual code cancellation/group cleanup", err)
	}
	if heldInstalledLeases(t, generation) != 0 {
		t.Fatal("child retained PM lease")
	}
}

func heldInstalledLeases(t *testing.T, generation string) int {
	t.Helper()
	rows, err := os.ReadDir(filepath.Join(generation, ".leases"))
	if err != nil {
		t.Fatal(err)
	}
	held := 0
	for _, row := range rows {
		f, e := os.Open(filepath.Join(generation, ".leases", row.Name()))
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			t.Fatal(e)
		}
		e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if e == syscall.EWOULDBLOCK {
			held++
		} else if e != nil {
			t.Fatal(e)
		}
		f.Close()
	}
	return held
}
