package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/looprun"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	hermesruntime "github.com/berryhill/aegis/internal/runtime/hermes"
)

func TestDoerFailurePacketRetainsOnlyClosedCode(t *testing.T) {
	raw := errors.New("private runtime text")
	for _, tc := range []struct{ reason, want string }{
		{"runtime_auth_auth_permanent", "runtime_auth_auth_permanent"},
		{"private runtime label", "doer_runtime_preparation_failed"},
	} {
		err := doerTurnFailure(hermesruntime.AttemptTurnResult{Attempt: execution.Turn{Reason: tc.reason}}, raw)
		var f *looprun.Failure
		if !errors.As(err, &f) || f.Code != tc.want || !errors.Is(err, raw) {
			t.Fatal("closed packet or causal classification lost")
		}
		wire, _ := json.Marshal(f)
		if strings.Contains(string(wire), "private") || strings.Contains(err.Error(), "private") {
			t.Fatal("raw failure escaped")
		}
	}
}

func TestDoerFailureConsumesOnePassAndDoesNotReplayTerminal(t *testing.T) {
	for _, code := range []string{"runtime_auth_auth_permanent", "doer_proposal_decode_failed", "doer_proposal_size_invalid", "doer_proposal_json_invalid", "doer_proposal_duplicate_field", "doer_proposal_unknown_field", "doer_proposal_shape_invalid", "doer_proposal_base64_invalid", "doer_proposal_trailing_data", "doer_proposal_edits_invalid", "doer_proposal_report_invalid"} {
		t.Run(code, func(t *testing.T) {
			expected := "fixture"
			contract := loop.DoerContract{Task: "Create selected file", Workspace: t.TempDir(), WritableFiles: []string{"selected.txt"}, VerifyFile: "selected.txt", ExpectedText: &expected, MaxAttempts: 2}
			rev, _, err := loop.NewDoerRevision("failure", 1, "", contract)
			if err != nil {
				t.Fatal(err)
			}
			roles := doerRoles{
				Admit: func(context.Context, string) error { return nil },
				Gate: func(context.Context, string) (LayaGate, error) {
					return LayaGate{Specified: true, ResultDefined: true}, nil
				},
				Implement: func(context.Context, string, string, uint16) (implementation.Proposal, error) {
					return implementation.Proposal{}, closedDoerFailure(code, errors.New("private model output"))
				},
				Apply: func(context.Context, loop.DoerContract, []implementation.Edit) error {
					t.Fatal("applied failed turn")
					return nil
				},
			}
			executor := &doerStepExecutor{contract: contract, roles: roles}
			cursor := &doerMemoryCursor{}
			result, err := looprun.Run(context.Background(), "failed-attempt", rev, looprun.Values{}, cursor, executor, nil)
			if err != nil || result.Outcome != loop.OutcomeFailed || len(result.Failures) != 1 || result.Failures[0].Code != code || executor.attempts != 1 {
				t.Fatalf("failure packet lost: %+v %v", result, err)
			}
			before, _ := json.Marshal(cursor.cursor)
			if strings.Contains(string(before), "private") {
				t.Fatal("raw error stored")
			}
			_, err = looprun.Run(context.Background(), "failed-attempt", rev, looprun.Values{}, cursor, executor, nil)
			after, _ := json.Marshal(cursor.cursor)
			if err != nil || executor.attempts != 1 || string(before) != string(after) {
				t.Fatal("terminal reexecuted or history changed")
			}
		})
	}
}

// Synthetic final-pass failure only; never opens a retained/live task store.
func TestDoerStreamingTimeoutDoesNotRestoreExhaustedBudget(t *testing.T) {
	contract := loop.DoerContract{Task: "Create selected file", Workspace: t.TempDir(), WritableFiles: []string{"selected.txt"}, VerifyFile: "selected.txt", MaxAttempts: 2}
	revision, _, err := loop.NewDoerRevision("stream-timeout-budget", 1, "", contract)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	roles := doerRoles{
		Admit: func(context.Context, string) error { return nil },
		Gate: func(context.Context, string) (LayaGate, error) {
			return LayaGate{Specified: true, ResultDefined: true}, nil
		},
		Implement: func(_ context.Context, _, _ string, pass uint16) (implementation.Proposal, error) {
			calls++
			if pass != 2 {
				t.Fatalf("final pass reset: %d", pass)
			}
			return implementation.Proposal{}, doerTurnFailure(hermesruntime.AttemptTurnResult{Attempt: execution.Turn{Reason: "runtime_streaming_timeout"}}, errors.New("synthetic timeout"))
		},
		Apply: func(context.Context, loop.DoerContract, []implementation.Edit) error {
			t.Fatal("failed stream applied edits")
			return nil
		},
	}
	executor := &doerStepExecutor{contract: contract, roles: roles, attempts: 1}
	cursor := &doerMemoryCursor{}
	result, err := looprun.Run(context.Background(), "exhausted-stream", revision, looprun.Values{}, cursor, executor, nil)
	if err != nil || result.Outcome != loop.OutcomeFailed || executor.attempts != 2 || calls != 1 || len(result.Failures) != 1 || result.Failures[0].Code != "runtime_streaming_timeout" {
		t.Fatalf("final timeout lost failure/budget: %+v %v attempts=%d calls=%d", result, err, executor.attempts, calls)
	}
	before, err := json.Marshal(cursor.cursor)
	if err != nil {
		t.Fatal(err)
	}
	// Terminal checkpoint replay must not reopen work even with a new executor.
	reloaded := &doerStepExecutor{contract: contract, roles: roles, attempts: executor.attempts}
	replayed, err := looprun.Run(context.Background(), "exhausted-stream", revision, looprun.Values{}, cursor, reloaded, nil)
	after, marshalErr := json.Marshal(cursor.cursor)
	if err != nil || marshalErr != nil || replayed.Outcome != loop.OutcomeFailed || calls != 1 || reloaded.attempts != 2 || string(before) != string(after) {
		t.Fatalf("terminal timeout reopened/refunded: %+v %v attempts=%d calls=%d", replayed, err, reloaded.attempts, calls)
	}
	for _, step := range revision.Steps {
		if step.Executable != nil && step.Executable.Operation == loop.DoerImplement {
			if _, err := reloaded.Execute(context.Background(), looprun.StepRequest{Step: step}); err == nil || reloaded.attempts != 2 || calls != 1 {
				t.Fatal("exhausted implementation budget restored")
			}
			return
		}
	}
	t.Fatal("implementation step missing")
}

func TestDoerTraceLegacyAndUnknownFailureCodesAreContentFree(t *testing.T) {
	for _, code := range []string{"step_error", "doer_proposal_decode_failed", "runtime_provider_format_error", "private arbitrary code", "doer_proposal_size_invalid", "doer_proposal_json_invalid", "doer_proposal_duplicate_field", "doer_proposal_unknown_field", "doer_proposal_shape_invalid", "doer_proposal_base64_invalid", "doer_proposal_trailing_data", "doer_proposal_edits_invalid", "doer_proposal_report_invalid"} {
		t.Run(code, func(t *testing.T) {
			ctx := context.Background()
			repo, err := fleetbadger.Open(ctx, filepath.Join(t.TempDir(), "fleet-v1"))
			if err != nil {
				t.Fatal(err)
			}
			defer repo.Close()
			digest := "sha256:" + strings.Repeat("a", 64)
			facts := implementation.StepCheckpointStore{DB: repo.ImplementationStore(), RunID: "attempt", RevisionDigest: digest}
			payload, _ := json.Marshal(map[string]any{"step_id": "implement", "visits": map[string]int{"implement": 1}, "done": map[string]string{"outcome": "failed"}, "failures": []map[string]string{{"code": code, "message": "private runtime text"}}})
			if _, err = facts.Append(ctx, "", payload); err != nil {
				t.Fatal(err)
			}
			worker := &QueueWorker{repository: repo}
			trace, err := worker.DoerTrace(ctx, "attempt", digest)
			if err != nil || len(trace) != 1 {
				t.Fatal("trace missing", err)
			}
			want := code
			if code == "private arbitrary code" {
				want = "step_error"
			}
			if trace[0].FailureCode != want || !trace[0].Terminal {
				t.Fatal("wrong closed projection")
			}
			wire, _ := json.Marshal(trace)
			if strings.Contains(string(wire), "private") {
				t.Fatal("raw failure projected")
			}
		})
	}
}
