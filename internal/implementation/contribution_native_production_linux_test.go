//go:build linux

package implementation

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/runtime/hermes"
)

func TestContributionNativeProductionCallerJoinsWriter(t *testing.T) {
	ctx := context.Background()
	kernel, c := fixture(t)
	source, err := Inventory(ctx, c.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	evidence := t.TempDir()
	home := ""
	e := &ManagedContribution{DB: kernel.DB, GoBinary: kernel.GoBinary, StateRoot: t.TempDir(), Admit: kernel.Admit}
	e.Native = func(ctx context.Context, cmd *exec.Cmd) error {
		home = filepath.Dir(cmd.Dir)
		// Execute a real descendant-writing command through the production primitive,
		// retaining the actual caller's materialized source/home and cleanup path.
		cmd.Path = "/bin/sh"
		cmd.Args = []string{"sh", "-c", `(while :; do printf x >> "$1/writes"; printf x >> source-owned; sleep 0.001; done) & while [ ! -s "$1/writes" ]; do sleep 0.001; done; exit 0`, "fixture", evidence}
		return hermes.RunContributionNative(ctx, execution.NativeBinding{CommandDigest: hermes.NativeCommandDigest(cmd)}, cmd, func(context.Context) error { return nil }, func(_ context.Context, p hermes.NativeCompletion) error {
			if !p.Joined() {
				return execution.ErrNativeQuiescenceUnavailable
			}
			return nil
		})
	}
	_, err = e.native(ctx, c, source, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("joined caller home not removed: %v", err)
	}
	before, _ := os.ReadFile(filepath.Join(evidence, "writes"))
	time.Sleep(30 * time.Millisecond)
	after, _ := os.ReadFile(filepath.Join(evidence, "writes"))
	if len(before) == 0 || len(before) != len(after) {
		t.Fatal("owned writer survived caller cleanup")
	}
}

func TestContributionNativeBuildOutputCustody(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "joined", true: "unknown"}[fail], func(t *testing.T) {
			ctx := context.Background()
			kernel, c := fixture(t)
			source, err := Inventory(ctx, c.Workspace)
			if err != nil {
				t.Fatal(err)
			}
			home, path := "", ""
			e := &ManagedContribution{DB: kernel.DB, GoBinary: kernel.GoBinary, StateRoot: t.TempDir(), Admit: kernel.Admit}
			e.Native = func(_ context.Context, cmd *exec.Cmd) error {
				home = filepath.Dir(cmd.Dir)
				for i, arg := range cmd.Args {
					if arg == "-o" {
						path = cmd.Args[i+1]
					}
				}
				if path == "" {
					t.Fatal("missing build output")
				}
				if err := os.WriteFile(path, []byte("fixture executable bytes"), 0600); err != nil {
					t.Fatal(err)
				}
				if fail {
					return execution.ErrNativeQuiescenceUnavailable
				}
				return nil
			}
			_, err = e.build(ctx, c, loop.ContributionExecutionPolicy{BuildPackage: "."}, source)
			if fail && !errors.Is(err, execution.ErrNativeQuiescenceUnavailable) {
				t.Fatal(err)
			}
			if !fail && err != nil {
				t.Fatal(err)
			}
			for _, p := range []string{home, path} {
				_, statErr := os.Stat(p)
				if fail && statErr != nil {
					t.Fatalf("unknown custody removed %s: %v", p, statErr)
				}
				if !fail && !os.IsNotExist(statErr) {
					t.Fatalf("joined artifact retained %s: %v", p, statErr)
				}
			}
		})
	}
}
