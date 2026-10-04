package orchestration

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/registry"
)

func TestDoerProtectedCustodyCannotBeAllowlisted(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	if err := os.MkdirAll(filepath.Join(state, "controller-continuation"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, selected := range []string{"controller-continuation/ed25519-seed", "checkpoints/doer-host-write-ed25519.key", "auth/principal-password.json"} {
		t.Run(selected, func(t *testing.T) {
			c := loop.DoerContract{Task: "Create selected file", Workspace: state, WritableFiles: []string{selected}, VerifyFile: selected, MaxAttempts: 2}
			digest, err := c.Digest()
			if err != nil {
				t.Fatal(err)
			}
			agent := registry.AgentRevision{Runtime: registry.RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent"}}
			w := &QueueWorker{implementation: &ImplementationController{config: config.Implementation{AuthorizedContracts: []string{digest}}}, doerProtectedPaths: []string{state}, doerHostApproval: func(context.Context, loop.DoerContract, registry.AgentRevision) error { return nil }}
			if err = w.authorizeDoer(context.Background(), c, agent); err == nil {
				t.Fatal("configured allowlist bypassed controller custody protection")
			}
			w.implementation.config.AuthorizedContracts = nil
			if err = w.authorizeDoer(context.Background(), c, agent); err == nil {
				t.Fatal("signed approval resolver bypassed controller custody protection")
			}
		})
	}
}
