package orchestration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/looprun"
)

type doerMemoryCursor struct{ cursor looprun.Cursor }

func (m *doerMemoryCursor) Load(context.Context) (looprun.Cursor, error)   { return m.cursor, nil }
func (m *doerMemoryCursor) Save(_ context.Context, c looprun.Cursor) error { m.cursor = c; return nil }

func TestDoerStepsVerifierFailureDiagnosesAndRetries(t *testing.T) {
	workspace := t.TempDir()
	expected := "hello"
	contract := loop.DoerContract{Task: "Create result.txt containing hello", Workspace: workspace, WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", ExpectedText: &expected, MaxAttempts: 3}
	revision, _, err := loop.NewDoerRevision("doer-test", 1, "", contract)
	if err != nil {
		t.Fatal(err)
	}
	calls := map[string]int{}
	roles := doerRoles{
		Admit: func(_ context.Context, effect string) error { calls[effect]++; return nil },
		Gate: func(context.Context, string) (LayaGate, error) {
			return LayaGate{Specified: true, ResultDefined: true}, nil
		},
		Implement: func(_ context.Context, task, feedback string, n uint16) (implementation.Proposal, error) {
			if task != contract.Task || n > 1 && feedback != "repair the file" {
				t.Fatalf("lost original contract or feedback: %q %q %d", task, feedback, n)
			}
			content := "wrong"
			if n == 2 {
				content = "hello"
			}
			return implementation.Proposal{Edits: []implementation.Edit{{Path: "result.txt", Content: []byte(content)}}, Report: "I wrote the selected file and ran checks"}, nil
		},
		Apply: func(_ context.Context, _ loop.DoerContract, edits []implementation.Edit) error {
			return os.WriteFile(filepath.Join(workspace, edits[0].Path), edits[0].Content, 0600)
		},
		Judge: func(context.Context, string, string) (LayaVerdict, error) {
			return LayaVerdict{Done: true, StaysInScope: true, Fulfills: true, Works: true, Practices: true}, nil
		},
		Verify: func(ctx context.Context) (evidence.SelectedFileResult, error) {
			p := evidence.SelectedFilePolicy{Version: evidence.SelectedFilePolicyV1, RelativePath: "result.txt", Mode: evidence.SelectedFileText, Text: expected}
			d, _ := p.Digest()
			return evidence.VerifySelectedFile(ctx, workspace, p, d, evidence.SelectedFileBinding{AttemptID: "attempt", ActionID: "verify", RunID: "run", OwnerID: "owner", AuthorityContextID: "authority", AuthorityContextDigest: "sha256:authority"})
		},
		Diagnose: func(_ context.Context, _ string, _ string, failures []string, n uint16) (string, error) {
			if n != 1 || len(failures) != 1 || failures[0] != "independent_verification:text_mismatch" {
				t.Fatalf("wrong failure aggregation: %v", failures)
			}
			return "repair the file", nil
		},
	}
	executor := &doerStepExecutor{contract: contract, roles: roles}
	cursor := &doerMemoryCursor{}
	result, err := looprun.Run(context.Background(), "attempt", revision, looprun.Values{}, cursor, executor, nil)
	if err != nil || result.Outcome != loop.OutcomeSucceeded || executor.attempts != 2 || executor.observation.Outcome != evidence.Passed || calls[string(loop.DoerVerify)] != 2 || calls[string(loop.DoerDiagnosis)] != 1 {
		t.Fatalf("Doer result=%+v err=%v attempts=%d calls=%v", result, err, executor.attempts, calls)
	}
	data, err := os.ReadFile(filepath.Join(workspace, "result.txt"))
	if err != nil || string(data) != "hello" {
		t.Fatalf("artifact=%q err=%v", data, err)
	}
}

func TestDoerStepsRejectedJudgmentStillRunsVerifier(t *testing.T) {
	workspace := t.TempDir()
	contract := loop.DoerContract{Task: "Make result.txt", Workspace: workspace, WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", MaxAttempts: 1}
	revision, _, err := loop.NewDoerRevision("doer-test", 1, "", contract)
	if err != nil {
		t.Fatal(err)
	}
	verified := 0
	roles := doerRoles{
		Admit: func(context.Context, string) error { return nil },
		Gate:  func(context.Context, string) (LayaGate, error) { return LayaGate{true, true}, nil },
		Implement: func(context.Context, string, string, uint16) (implementation.Proposal, error) {
			return implementation.Proposal{Edits: []implementation.Edit{{Path: "result.txt", Content: []byte("done")}}, Report: "done"}, nil
		},
		Apply: func(_ context.Context, _ loop.DoerContract, edits []implementation.Edit) error {
			return os.WriteFile(filepath.Join(workspace, "result.txt"), edits[0].Content, 0600)
		},
		Judge: func(context.Context, string, string) (LayaVerdict, error) {
			return LayaVerdict{Done: false, StaysInScope: true, Fulfills: true, Works: true, Practices: true}, nil
		},
		Verify: func(context.Context) (evidence.SelectedFileResult, error) {
			verified++
			return evidence.SelectedFileResult{Outcome: evidence.Passed}, nil
		},
	}
	executor := &doerStepExecutor{contract: contract, roles: roles}
	result, err := looprun.Run(context.Background(), "attempt", revision, looprun.Values{}, &doerMemoryCursor{}, executor, nil)
	if err != nil || result.Outcome != loop.OutcomeFailed || verified != 1 || executor.attempts != 1 {
		t.Fatalf("result=%+v err=%v verified=%d", result, err, verified)
	}
	if _, err := json.Marshal(result); err != nil {
		t.Fatal(err)
	}
}
