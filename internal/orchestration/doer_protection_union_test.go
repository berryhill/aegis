package orchestration

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/registry"
	hermesruntime "github.com/berryhill/aegis/internal/runtime/hermes"
)

func TestDoerMandatoryProtectionSurvivesScopeReplacementAndReconfigure(t *testing.T) {
	for _, after := range []bool{false, true} {
		t.Run(map[bool]string{false: "scopes-first", true: "scopes-after"}[after], func(t *testing.T) {
			root := t.TempDir()
			state := filepath.Join(root, "state")
			home := filepath.Join(root, "actual-laya")
			if err := os.MkdirAll(home, 0700); err != nil {
				t.Fatal(err)
			}
			goBin := filepath.Join(root, "actual-go")
			py := filepath.Join(root, "actual-python")
			for _, file := range []string{goBin, py} {
				if err := os.WriteFile(file, []byte("inert"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			w := &QueueWorker{}
			if !after {
				w.SetDoerProtectedPaths([]string{filepath.Join(root, "supplemental")})
			}
			if err := w.ConfigureImplementation(config.Implementation{GoBinary: goBin, LayaPython: py, LayaHome: home}, state, &hermesruntime.Adapter{}); err != nil {
				t.Fatal(err)
			}
			if after {
				w.SetDoerProtectedPaths([]string{filepath.Join(root, "supplemental")})
			}
			for _, target := range []string{goBin, py, filepath.Join(home, "checkpoint")} {
				c := loop.DoerContract{Task: "Create selected file", Workspace: filepath.Dir(target), WritableFiles: []string{filepath.Base(target)}, VerifyFile: filepath.Base(target), MaxAttempts: 2}
				digest, err := c.Digest()
				if err != nil {
					t.Fatal(err)
				}
				w.implementation.config.AuthorizedContracts = []string{digest}
				agent := registry.AgentRevision{Runtime: registry.RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent"}}
				if err := w.authorizeDoer(context.Background(), c, agent); err == nil {
					t.Fatalf("actual helper path writable: %s", target)
				}
			}
		})
	}
}
