package api

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
)

func TestInstalledBinaryFixtureCopies(t *testing.T) {
	const executable = "independent executable bytes"
	var builds atomic.Int32
	built := sync.OnceValues(func() (string, error) {
		builds.Add(1)
		return executable, nil
	})
	const clients = 8
	paths := make([]string, clients)
	for i := range paths {
		root := t.TempDir()
		t.Run("concurrent copy", func(t *testing.T) {
			t.Parallel()
			path, err := installedLoopQueueBinary(root, "", built)
			if err != nil {
				t.Fatal(err)
			}
			paths[i] = path
		})
	}
	// Parallel children finish before parent cleanups, while their private files
	// still exist. Mutating/deleting one must not affect any other or the cache.
	t.Cleanup(func() {
		if builds.Load() != 1 {
			t.Fatalf("built %d times", builds.Load())
		}
		first, err := os.Stat(paths[0])
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range paths[1:] {
			info, err := os.Stat(path)
			if err != nil || path == paths[0] || os.SameFile(first, info) || info.Mode().Perm() != 0700 {
				t.Fatalf("shared/unsafe executable %q: %v", path, err)
			}
		}
		if err := os.WriteFile(paths[0], []byte("mutated"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(paths[0]); err != nil {
			t.Fatal(err)
		}
		for _, path := range paths[1:] {
			raw, err := os.ReadFile(path)
			if err != nil || string(raw) != executable {
				t.Fatalf("copy changed after sibling mutation/deletion: %v", err)
			}
		}
		if raw, err := built(); err != nil || raw != executable {
			t.Fatal("private file mutation escaped into immutable cache")
		}
	})
}

func TestInstalledBinaryFixtureFrozenBypass(t *testing.T) {
	root := t.TempDir()
	candidate := filepath.Join(root, "frozen")
	if err := os.WriteFile(candidate, []byte("frozen bytes"), 0700); err != nil {
		t.Fatal(err)
	}
	mustNotBuild := func() (string, error) {
		t.Fatal("explicit frozen candidate invoked build/cache")
		return "", nil
	}
	for _, path := range []string{candidate, "relative", filepath.Join(root, "absent"), root} {
		got, err := installedLoopQueueBinary(t.TempDir(), path, mustNotBuild)
		if path == candidate {
			if err != nil || got != candidate {
				t.Fatalf("frozen path changed: %q %v", got, err)
			}
		} else if err == nil || got != "" {
			t.Fatalf("invalid frozen path fell back: %q %v", got, err)
		}
	}
	t.Setenv("AEGIS_TEST_FROZEN_BINARY", candidate)
	var first *installedLoopQueueClient
	t.Run("client cleanup", func(t *testing.T) {
		first = newInstalledLoopQueueClient(t, &app.Service{})
	})
	if _, err := os.Stat(first.root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("client root not cleaned: %v", err)
	}
	second := newInstalledLoopQueueClient(t, &app.Service{})
	if first.config == second.config || first.root == second.root || second.binary != candidate {
		t.Fatal("clients share config/root or changed frozen candidate")
	}
	if err := os.WriteFile(second.config, []byte("private mutation"), 0600); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(candidate); err != nil || string(raw) != "frozen bytes" {
		t.Fatalf("frozen bytes changed by config mutation/cleanup: %v", err)
	}
}

func TestInstalledBinaryFixtureBuildFailureAndCleanup(t *testing.T) {
	failure := errors.New("fixture build failed")
	for _, mode := range []string{"success", "failure", "missing output"} {
		t.Run(mode, func(t *testing.T) {
			var binary string
			var builds int
			built := sync.OnceValues(func() (string, error) {
				return buildInstalledLoopQueueExecutable(func(command *exec.Cmd, timeout time.Duration) ([]byte, error) {
					builds++
					want := []string{"go", "build", "-p=1", "-ldflags=-X github.com/berryhill/aegis/internal/buildinfo.Version=0.0.0-loop-queue-fixture", "-o"}
					if timeout != 2*time.Minute || command.Dir != "../.." || len(command.Args) != 7 || strings.Join(command.Args[:5], "\n") != strings.Join(want, "\n") || command.Args[6] != "./cmd/aegis" {
						t.Fatalf("build specification/bound changed: %v %v", command, timeout)
					}
					binary = command.Args[5]
					if mode != "missing output" {
						if err := os.WriteFile(binary, []byte("built bytes"), 0700); err != nil {
							t.Fatal(err)
						}
					}
					if mode == "failure" {
						return []byte("compiler diagnostic"), failure
					}
					return nil, nil
				})
			})
			for i := 0; i < 2; i++ {
				raw, err := built()
				switch mode {
				case "success":
					if err != nil || raw != "built bytes" {
						t.Fatalf("build bytes: %q %v", raw, err)
					}
				case "failure":
					if !errors.Is(err, failure) || !strings.Contains(err.Error(), "compiler diagnostic") || raw != "" {
						t.Fatalf("lost build failure: %q %v", raw, err)
					}
				case "missing output":
					if !errors.Is(err, os.ErrNotExist) || raw != "" {
						t.Fatalf("missing output accepted: %q %v", raw, err)
					}
				}
			}
			clientRoot := t.TempDir()
			path, err := installedLoopQueueBinary(clientRoot, "", built)
			if mode == "success" {
				if err != nil || path != filepath.Join(clientRoot, "aegis") {
					t.Fatalf("cached build not published privately: %q %v", path, err)
				}
			} else {
				if err == nil || path != "" {
					t.Fatalf("failed build published: %q %v", path, err)
				}
				if _, err := os.Stat(filepath.Join(clientRoot, "aegis")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("failed build left executable: %v", err)
				}
			}
			if builds != 1 {
				t.Fatalf("build outcome retried %d times", builds)
			}
			if _, err := os.Stat(filepath.Dir(binary)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("build scratch directory retained: %v", err)
			}
		})
	}
}
