package orchestration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/looprun"
)

func TestDoerVerifierRejectsUnappliedSelectedFile(t *testing.T) {
	workspace := t.TempDir()
	contract := loop.DoerContract{Task: "Create result", Workspace: workspace, WritableFiles: []string{"result.txt", "other.txt"}, VerifyFile: "result.txt", MaxAttempts: 1}
	revision, _, err := loop.NewDoerRevision("doer", 1, "", contract)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"no_selected_edit", "post_apply_substitution"} {
		t.Run(mode, func(t *testing.T) {
			applies := 0
			roles := doerRoles{
				Admit: func(context.Context, string) error { return nil },
				Gate: func(context.Context, string) (LayaGate, error) {
					return LayaGate{Specified: true, ResultDefined: true}, nil
				},
				Implement: func(context.Context, string, string, uint16) (implementation.Proposal, error) {
					path := "result.txt"
					if mode == "no_selected_edit" {
						path = "other.txt"
					}
					return implementation.Proposal{Edits: []implementation.Edit{{Path: path, Content: []byte("new")}}, Report: "created result"}, nil
				},
				Apply: func(_ context.Context, _ loop.DoerContract, edits []implementation.Edit) error {
					applies++
					if err := os.WriteFile(filepath.Join(workspace, edits[0].Path), edits[0].Content, 0600); err != nil {
						return err
					}
					if mode == "post_apply_substitution" {
						return os.WriteFile(filepath.Join(workspace, "result.txt"), []byte("foreign"), 0600)
					}
					return nil
				},
				Judge: func(context.Context, string, string) (LayaVerdict, error) {
					return LayaVerdict{true, true, true, true, true}, nil
				},
				Verify: func(context.Context) (evidence.SelectedFileResult, error) {
					wire, err := os.ReadFile(filepath.Join(workspace, "result.txt"))
					if err != nil {
						return evidence.SelectedFileResult{Outcome: evidence.Failed, FailureCategory: "missing"}, nil
					}
					sum := sha256.Sum256(wire)
					return evidence.SelectedFileResult{Outcome: evidence.Passed, ContentDigest: "sha256:" + hex.EncodeToString(sum[:])}, nil
				},
			}
			executor := &doerStepExecutor{contract: contract, roles: roles}
			result, err := looprun.Run(context.Background(), "attempt", revision, looprun.Values{}, &doerMemoryCursor{}, executor, nil)
			if mode == "no_selected_edit" {
				if err != nil || applies != 0 || result.Outcome != loop.OutcomeFailed {
					t.Fatalf("unlisted selected output accepted: %+v %v applies=%d", result, err, applies)
				}
			} else if err != nil || result.Outcome != loop.OutcomeFailed || applies != 1 {
				t.Fatalf("substituted selected output accepted: %+v %v applies=%d", result, err, applies)
			}
		})
	}
}
