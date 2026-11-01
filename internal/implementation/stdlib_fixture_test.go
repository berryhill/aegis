package implementation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stdlibSeededNative avoids rebuilding the identical standard library for each
// check, while retaining a fresh cache, source compilation and test execution.
func stdlibSeededNative(t *testing.T, goBinary string) func(context.Context, *exec.Cmd) error {
	// Seed ONLY immutable standard-library compilation, never candidate code,
	// test results, toolchain validation, evidence or authority. Each invocation
	// still gets its own writable cache and executes every test with -count=1.
	seed := filepath.Join(t.TempDir(), "stdlib")
	seeded := false
	return func(ctx context.Context, cmd *exec.Cmd) error {
		if cmd.Path != goBinary {
			return runCheckerNative(ctx, cmd)
		}
		if !seeded {
			warm := exec.CommandContext(ctx, cmd.Path, "list", "-export", "-deps", "-f", "{{.Standard}}", "fmt", "os/exec", "testing/internal/testdeps")
			warm.Dir = t.TempDir()
			for _, value := range cmd.Env {
				if !strings.HasPrefix(value, "GOCACHE=") && !strings.HasPrefix(value, "AEGIS_CONTRIBUTION_CANDIDATE=") {
					warm.Env = append(warm.Env, value)
				}
			}
			warm.Env = append(warm.Env, "GOCACHE="+seed, "GO111MODULE=off")
			started := time.Now()
			output, err := warm.CombinedOutput()
			if err != nil {
				return fmt.Errorf("stdlib fixture compilation: %w: %s", err, output)
			}
			packages := strings.Fields(string(output))
			if len(packages) == 0 {
				return errors.New("empty standard-library fixture seed")
			}
			for _, standard := range packages {
				if standard != "true" {
					return errors.New("non-standard package in fixture seed")
				}
			}
			t.Logf("stdlib-only fixture compilation: %s", time.Since(started))
			seeded = true
		}
		cache := ""
		for _, value := range cmd.Env {
			if strings.HasPrefix(value, "GOCACHE=") {
				cache = strings.TrimPrefix(value, "GOCACHE=")
			}
		}
		if cache == "" {
			return errors.New("fixture missing private native cache")
		}
		if err := os.CopyFS(cache, os.DirFS(seed)); err != nil {
			return err
		}
		started := time.Now()
		err := runCheckerNative(ctx, cmd)
		t.Logf("fresh native %s with stdlib-only seed: %s", cmd.Args[1], time.Since(started))
		return err
	}
}
