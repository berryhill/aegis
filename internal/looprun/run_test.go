package looprun

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/loop"
)

func revision(t *testing.T, cycle bool) loop.LoopRevision {
	t.Helper()
	p := loop.Port{ID: "value", Type: loop.TypeString, Required: true}
	steps := []loop.Step{
		{ID: "work", Kind: loop.StepAction, InputPorts: []loop.Port{p}, OutputPorts: []loop.Port{p}, Retry: loop.RetryPolicy{MaxAttempts: 2}},
		{ID: "gate", Kind: loop.StepGate, InputPorts: []loop.Port{p}, OutputPorts: []loop.Port{p}, Retry: loop.RetryPolicy{MaxAttempts: 1}, Gate: &loop.GateDefinition{Mode: "exclusive"}},
		{ID: "join", Kind: loop.StepJoin, InputPorts: []loop.Port{p}, OutputPorts: []loop.Port{p}, Retry: loop.RetryPolicy{MaxAttempts: 1}},
		{ID: "done", Kind: loop.StepTerminal, InputPorts: []loop.Port{p}, OutputPorts: []loop.Port{{ID: "final", Type: loop.TypeString, Required: true}}, Retry: loop.RetryPolicy{MaxAttempts: 1}, Terminal: &loop.TerminalDefinition{Outcome: loop.OutcomeSucceeded, OutputMappings: []loop.PortMapping{{SourcePort: "final", TargetPort: "result"}}}},
	}
	tr := []loop.Transition{
		{ID: "wg", FromStepID: "work", ToStepID: "gate", Mappings: []loop.PortMapping{{SourcePort: "value", TargetPort: "value"}}},
		{ID: "gj", FromStepID: "gate", ToStepID: "join", Condition: "finish", Mappings: []loop.PortMapping{{SourcePort: "value", TargetPort: "value"}}},
		{ID: "gw", FromStepID: "gate", ToStepID: "work", Condition: "again", Mappings: []loop.PortMapping{{SourcePort: "value", TargetPort: "value"}}},
		{ID: "jd", FromStepID: "join", ToStepID: "done", Mappings: []loop.PortMapping{{SourcePort: "value", TargetPort: "value"}}},
	}
	// A second incoming edge to join, while retaining exclusive traversal.
	steps = append(steps, loop.Step{ID: "side", Kind: loop.StepAction, InputPorts: []loop.Port{p}, OutputPorts: []loop.Port{p}, Retry: loop.RetryPolicy{MaxAttempts: 1}})
	tr = append(tr, loop.Transition{ID: "gs", FromStepID: "gate", ToStepID: "side", Condition: "side", Mappings: []loop.PortMapping{{SourcePort: "value", TargetPort: "value"}}}, loop.Transition{ID: "sj", FromStepID: "side", ToStepID: "join", Mappings: []loop.PortMapping{{SourcePort: "value", TargetPort: "value"}}})
	if cycle {
		tr[2].MaxTraversals = 1
		tr[0].MaxTraversals = 2
	} else { // no cycle possible in this fixture only when cycle=true
		tr[2].MaxTraversals = 1
		tr[0].MaxTraversals = 2
	}
	r, _, err := loop.NewRevision(loop.LoopRevision{LoopID: "test", Revision: 1, Inputs: []loop.Port{p}, Outputs: []loop.Port{{ID: "result", Type: loop.TypeString, Required: true}}, EntryStepID: "work", Steps: steps, Transitions: tr})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

type memory struct {
	cursor Cursor
	saves  int
	failAt int
}

func (m *memory) Load(context.Context) (Cursor, error) { return m.cursor, nil }
func (m *memory) Save(_ context.Context, c Cursor) error {
	m.saves++
	if m.saves == m.failAt {
		return errors.New("checkpoint unavailable")
	}
	m.cursor = c
	return nil
}

type stepFn func(context.Context, StepRequest) (StepResult, error)

func (f stepFn) Execute(c context.Context, r StepRequest) (StepResult, error) { return f(c, r) }
func values(s string) Values                                                  { return Values{"value": json.RawMessage(`"` + s + `"`)} }

func TestBranchRetryAndTerminalOutput(t *testing.T) {
	r := revision(t, true)
	m := &memory{}
	attempts := map[string]int{}
	original := Values{}
	exec := stepFn(func(_ context.Context, q StepRequest) (StepResult, error) {
		attempts[q.Step.ID]++
		if q.Step.ID == "work" && q.Visit == 1 && q.Attempt == 1 {
			original = cloneValues(q.Inputs)
			q.Inputs["value"] = json.RawMessage(`"mutated"`)
			return StepResult{}, &Failure{Code: "transient", Message: "temporary"}
		}
		if q.Step.ID == "work" && q.Visit == 1 && !reflect.DeepEqual(q.Inputs, original) {
			t.Fatal("retry changed original input")
		}
		if q.Step.ID == "gate" {
			label := "finish"
			if q.Visit == 1 {
				label = "again"
			}
			return StepResult{Outputs: q.Inputs, Branch: label}, nil
		}
		if q.Step.ID == "done" {
			return StepResult{Outputs: Values{"final": q.Inputs["value"]}}, nil
		}
		return StepResult{Outputs: q.Inputs}, nil
	})
	result, err := Run(context.Background(), "run-1", r, values("start"), m, exec, nil)
	if err != nil || result.Outcome != loop.OutcomeSucceeded || string(result.Outputs["result"]) != `"start"` {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if attempts["work"] != 3 || m.cursor.Traversals["gw"] != 1 || m.cursor.Traversals["wg"] != 2 || len(result.Failures) != 1 {
		t.Fatalf("attempts=%v cursor=%+v result=%+v", attempts, m.cursor, result)
	}
}
func TestUnknownBranchAndRetryExhaustion(t *testing.T) {
	for _, tc := range []struct {
		name string
		exec stepFn
		code string
	}{
		{"branch", func(_ context.Context, q StepRequest) (StepResult, error) {
			if q.Step.Kind == loop.StepGate {
				return StepResult{Outputs: q.Inputs, Branch: "unknown"}, nil
			}
			return StepResult{Outputs: q.Inputs}, nil
		}, "branch_invalid"},
		{"retry", func(_ context.Context, q StepRequest) (StepResult, error) {
			return StepResult{}, &Failure{Code: "bad", Message: "blocked"}
		}, "retry_exhausted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := revision(t, true)
			res, err := Run(context.Background(), "run-1", r, values("x"), &memory{}, tc.exec, nil)
			if err != nil || res.Failure == nil || res.Failure.Code != tc.code {
				t.Fatalf("%+v %v", res, err)
			}
		})
	}
}
func TestResumePendingUsesSameAttemptAndRequest(t *testing.T) {
	r := revision(t, true)
	m := &memory{failAt: 3}
	var calls []StepRequest
	exec := stepFn(func(_ context.Context, q StepRequest) (StepResult, error) {
		calls = append(calls, q)
		if q.Step.ID == "gate" {
			return StepResult{Outputs: q.Inputs, Branch: "finish"}, nil
		}
		if q.Step.ID == "done" {
			return StepResult{Outputs: Values{"final": q.Inputs["value"]}}, nil
		}
		return StepResult{Outputs: q.Inputs}, nil
	})
	_, err := Run(context.Background(), "run-1", r, values("x"), m, exec, nil)
	if err == nil {
		t.Fatal("expected checkpoint error")
	}
	_, err = Run(context.Background(), "run-1", r, values("x"), m, exec, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) < 2 || calls[0].Key != calls[1].Key || calls[0].Attempt != calls[1].Attempt || !reflect.DeepEqual(calls[0].Inputs, calls[1].Inputs) {
		t.Fatalf("replay changed request: %+v", calls)
	}
}
func TestCycleLimitAndReporterCannotChangeOutcome(t *testing.T) {
	r := revision(t, true)
	m := &memory{}
	exec := stepFn(func(_ context.Context, q StepRequest) (StepResult, error) {
		if q.Step.Kind == loop.StepGate {
			return StepResult{Outputs: q.Inputs, Branch: "again"}, nil
		}
		return StepResult{Outputs: q.Inputs}, nil
	})
	report := ReportFunc(func(context.Context, Result) error { return errors.New("report offline") })
	res, err := Run(context.Background(), "run-1", r, values("x"), m, exec, report)
	if err != nil || res.Failure == nil || res.Failure.Code != "traversal_exhausted" || res.ReportError == "" {
		t.Fatalf("%+v %v", res, err)
	}
}
func TestRejectsForgedPendingStepAndUnscopedKey(t *testing.T) {
	r := revision(t, true)
	m := &memory{failAt: 3}
	exec := stepFn(func(_ context.Context, q StepRequest) (StepResult, error) { return StepResult{Outputs: q.Inputs}, nil })
	_, _ = Run(context.Background(), "run-1", r, values("x"), m, exec, nil)
	if m.cursor.Pending == nil {
		t.Fatal("expected pending request")
	}
	m.cursor.Pending.Step.Retry.MaxAttempts = 10
	count := 0
	guarded := stepFn(func(_ context.Context, q StepRequest) (StepResult, error) { count++; return StepResult{}, nil })
	if _, err := Run(context.Background(), "run-1", r, values("x"), m, guarded, nil); err == nil || count != 0 {
		t.Fatalf("forged step executed: %v %d", err, count)
	}
}

func TestSuccessfulOutcomeSurvivesCompletionReportFailure(t *testing.T) {
	r := revision(t, true)
	calls := 0
	exec := stepFn(func(_ context.Context, q StepRequest) (StepResult, error) {
		if q.Step.Kind == loop.StepGate {
			return StepResult{Outputs: q.Inputs, Branch: "finish"}, nil
		}
		if q.Step.Kind == loop.StepTerminal {
			return StepResult{Outputs: Values{"final": q.Inputs["value"]}}, nil
		}
		return StepResult{Outputs: q.Inputs}, nil
	})
	reporter := ReportFunc(func(context.Context, Result) error { calls++; return errors.New("offline") })
	m := &memory{}
	result, err := Run(context.Background(), "run-success", r, values("x"), m, exec, reporter)
	if err != nil || result.Outcome != loop.OutcomeSucceeded || result.ReportError == "" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	result, err = Run(context.Background(), "run-success", r, values("x"), m, exec, reporter)
	if err != nil || calls != 1 || result.Outcome != loop.OutcomeSucceeded {
		t.Fatalf("resume=%+v calls=%d err=%v", result, calls, err)
	}
	if _, err := Run(context.Background(), "different-run", r, values("x"), m, exec, nil); err == nil {
		t.Fatal("cross-run cursor accepted")
	}
}

func TestRejectsInvalidRevisionAndTamperedCursorBeforeEffects(t *testing.T) {
	r := revision(t, true)
	m := &memory{cursor: Cursor{RevisionDigest: "sha256:" + strings.Repeat("0", 64), StepID: "work"}}
	count := 0
	exec := stepFn(func(_ context.Context, q StepRequest) (StepResult, error) { count++; return StepResult{}, nil })
	if _, err := Run(context.Background(), "run-1", r, values("x"), m, exec, nil); err == nil || count != 0 {
		t.Fatalf("err=%v calls=%d", err, count)
	}
	r.Digest = "invalid"
	if _, err := Run(context.Background(), "run-1", r, values("x"), &memory{}, exec, nil); err == nil || count != 0 {
		t.Fatalf("err=%v calls=%d", err, count)
	}
}
