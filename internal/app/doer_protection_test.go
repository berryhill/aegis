package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/berryhill/aegis/internal/loop"
)

func TestDoerHostApprovalRejectsControllerTargets(t *testing.T) {
	for _, which := range []string{"continuation-key", "host-key", "configuration"} {
		t.Run(which, func(t *testing.T) {
			s, subject, _, _, repo := hostApprovalFixture(t)
			target := filepath.Join(s.Config.StateDir, "controller-continuation", "ed25519-seed")
			if which == "host-key" {
				target = filepath.Join(s.Config.Audit.CheckpointDir, "doer-host-write-ed25519.key")
			}
			if which == "configuration" {
				s.ConfigFile = filepath.Join(t.TempDir(), "aegis.yaml")
				target = s.ConfigFile
			}
			if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				t.Fatal(err)
			}
			c := loop.DoerContract{Task: "Create selected file", Workspace: filepath.Dir(target), WritableFiles: []string{filepath.Base(target)}, VerifyFile: filepath.Base(target), MaxAttempts: 2}
			draft, err := s.SaveDoerDraftAs(context.Background(), subject, DoerDraftInput{Agent: agentRevisionRef(repo.latest), LoopID: "protected-target", Revision: 1, Contract: c})
			if err != nil {
				t.Fatal(err)
			}
			candidate, _, err := loop.NewDoerRevision(draft.LoopID, 1, "", c)
			if err != nil {
				t.Fatal(err)
			}
			digest, err := c.Digest()
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.ApproveDoerHostWriteAs(context.Background(), subject, DoerHostApprovalInput{DraftID: draft.ID, DraftVersion: draft.Version, ExpectedCandidateDigest: candidate.Digest, ExpectedContractDigest: digest, Decision: "approve-host-write"})
			if err == nil {
				t.Fatal("host-write approval admitted a control-plane target")
			}
		})
	}
}
