package implementation

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/loop"
)

type decisionStub struct {
	gate    GateDecision
	verdict Judgment
	calls   int
}

func (d *decisionStub) Gate(_ context.Context, task string) (GateDecision, error) {
	d.calls++
	return d.gate, nil
}
func (d *decisionStub) Judge(_ context.Context, task, report string) (Judgment, error) {
	d.calls++
	return d.verdict, nil
}

type diagnosisFunc func(context.Context, DiagnosisRequest) ([]byte, error)

func (f diagnosisFunc) Diagnose(ctx context.Context, r DiagnosisRequest) ([]byte, error) {
	return f(ctx, r)
}

type reportProposer func(context.Context, Request) (Proposal, error)

func (f reportProposer) Propose(ctx context.Context, r Request) ([]Edit, error) {
	p, e := f(ctx, r)
	return p.Edits, e
}
func (f reportProposer) ProposeReport(ctx context.Context, r Request) (Proposal, error) {
	return f(ctx, r)
}

type completionFunc func(context.Context, Record) (string, error)

func (f completionFunc) Report(ctx context.Context, r Record) (string, error) { return f(ctx, r) }
func doerContract(c loop.VerifiedImplementation) loop.VerifiedImplementation {
	c.DecisionMode = "doer.v1"
	c.MaxPasses = 3
	return c
}

func TestDoerGateStopsWithoutPass(t *testing.T) {
	e, c := fixture(t)
	c = doerContract(c)
	d := &decisionStub{gate: GateDecision{Specified: false, ResultDefined: true}}
	e.Decision = d
	e.Diagnosis = diagnosisFunc(func(context.Context, DiagnosisRequest) ([]byte, error) {
		t.Fatal("gate allowed diagnosis")
		return nil, nil
	})
	e.Proposer = reportProposer(func(context.Context, Request) (Proposal, error) {
		t.Fatal("gate allowed implement")
		return Proposal{}, nil
	})
	r, err := e.Run(context.Background(), "gate", c)
	if err != nil || r.State != "needs_input" || len(r.Passes) != 0 || len(r.Stages) != 1 || r.Stages[0].Name != "gate" {
		t.Fatalf("record=%+v err=%v", r, err)
	}
	if _, err = e.StageOutput("gate", r.Stages[0]); err != nil {
		t.Fatal(err)
	}
}
func TestDoerThreePassesDiagnosisAndStageReadback(t *testing.T) {
	e, c := fixture(t)
	c = doerContract(c)
	e.Decision = &decisionStub{gate: GateDecision{true, true}, verdict: Judgment{true, true, true, true, true}}
	calls := 0
	diagnoses := 0
	e.Proposer = reportProposer(func(_ context.Context, r Request) (Proposal, error) {
		calls++
		if r.Contract.Task != c.Task || r.Contract.Acceptance != c.Acceptance {
			t.Fatal("original request changed")
		}
		if calls > 1 && (!strings.Contains(string(r.PreviousCheck), "acceptance not satisfied") || !strings.Contains(string(r.PreviousDiagnosis), "repair value")) {
			t.Fatalf("missing aggregate feedback %+v", r)
		}
		value := "1"
		if calls == 3 {
			value = "42"
		}
		return Proposal{Edits: edits(value), Report: "completed patch with test evidence"}, nil
	})
	e.Diagnosis = diagnosisFunc(func(_ context.Context, r DiagnosisRequest) ([]byte, error) {
		diagnoses++
		if len(r.Check) == 0 || r.PassID == "" {
			t.Fatal("diagnosis lacks check")
		}
		return []byte("repair value"), nil
	})
	r, err := e.Run(context.Background(), "three", c)
	if err != nil || r.State != "succeeded" || len(r.Passes) != 3 || calls != 3 || diagnoses != 2 {
		t.Fatalf("record=%+v err=%v calls=%d diagnoses=%d", r, err, calls, diagnoses)
	}
	read, err := e.Read("three")
	if err != nil || len(read.Stages) < 9 {
		t.Fatalf("durable stages %+v %v", read, err)
	}
	for _, s := range read.Stages {
		if _, err = e.StageOutput("three", s); err != nil {
			t.Fatalf("stage %s: %v", s.Name, err)
		}
	}
	if err = e.Revalidate("three", c); err != nil {
		t.Fatal(err)
	}
}
func TestDoerJudgmentDoesNotReplaceNativeCheck(t *testing.T) {
	e, c := fixture(t)
	c = doerContract(c)
	c.MaxPasses = 1
	e.Decision = &decisionStub{gate: GateDecision{true, true}, verdict: Judgment{true, true, true, true, true}}
	e.Proposer = reportProposer(func(context.Context, Request) (Proposal, error) {
		return Proposal{Edits: edits("1"), Report: "done"}, nil
	})
	e.Diagnosis = diagnosisFunc(func(context.Context, DiagnosisRequest) ([]byte, error) { return []byte("fix"), nil })
	r, err := e.Run(context.Background(), "false-pass", c)
	if err == nil || r.State != "failed" || len(r.Passes) != 1 || r.Passes[0].Passed {
		t.Fatalf("%+v %v", r, err)
	}
}
func TestDoerRequiresPortsAndModeBound(t *testing.T) {
	e, c := fixture(t)
	c = doerContract(c)
	e.Proposer = proposerFunc(func(context.Context, Request) ([]Edit, error) { return edits("42"), nil })
	if _, err := e.Run(context.Background(), "missing-ports", c); err == nil {
		t.Fatal("missing decision ports accepted")
	}
	c.DecisionMode = ""
	if c.Validate() == nil {
		t.Fatal("legacy accepted three passes")
	}
	c.DecisionMode = "doer.v1"
	c.MaxPasses = 4
	if c.Validate() == nil {
		t.Fatal("unbounded doer accepted")
	}
}

func TestDoerExhaustionDoesNotDiagnoseAfterFinalPass(t *testing.T) {
	e, c := fixture(t)
	c = doerContract(c)
	c.MaxPasses = 1
	e.Decision = &decisionStub{gate: GateDecision{true, true}, verdict: Judgment{true, true, true, true, true}}
	e.Proposer = reportProposer(func(context.Context, Request) (Proposal, error) {
		return Proposal{Edits: edits("1"), Report: "attempted patch"}, nil
	})
	e.Diagnosis = diagnosisFunc(func(context.Context, DiagnosisRequest) ([]byte, error) {
		t.Fatal("final pass diagnosed")
		return nil, nil
	})
	e.Reporter = completionFunc(func(_ context.Context, r Record) (string, error) {
		if r.State != "failed" {
			t.Fatal("wrong report outcome")
		}
		return "The native test failed.", nil
	})
	r, err := e.Run(context.Background(), "exhausted", c)
	if err == nil || r.State != "failed" || len(r.Passes) != 1 || r.Completion != "The native test failed." {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestDoerNeedsInputReportFailureDoesNotChangeState(t *testing.T) {
	e, c := fixture(t)
	c = doerContract(c)
	e.Decision = &decisionStub{gate: GateDecision{false, true}}
	e.Proposer = reportProposer(func(context.Context, Request) (Proposal, error) {
		t.Fatal("implement called")
		return Proposal{}, nil
	})
	e.Diagnosis = diagnosisFunc(func(context.Context, DiagnosisRequest) ([]byte, error) {
		t.Fatal("diagnose called")
		return nil, nil
	})
	e.Reporter = completionFunc(func(context.Context, Record) (string, error) {
		return "", errors.New("unavailable")
	})
	r, err := e.Run(context.Background(), "needs-input-report", c)
	if err != nil || r.State != "needs_input" || len(r.Passes) != 0 || r.ReportError == "" || r.Completion != "" {
		t.Fatalf("%+v %v", r, err)
	}
}

type failingReportStore struct{ Store }

func (s failingReportStore) Put(key, value []byte) error {
	if bytes.Contains(value, []byte(`"completion":"`)) {
		return errors.New("optional report write failed")
	}
	return s.Store.Put(key, value)
}

func TestDoerReportWriteFailureCannotStrandVerifiedResult(t *testing.T) {
	e, c := fixture(t)
	e.DB = failingReportStore{e.DB}
	c = doerContract(c)
	e.Decision = &decisionStub{gate: GateDecision{true, true}, verdict: Judgment{true, true, true, true, true}}
	e.Proposer = reportProposer(func(context.Context, Request) (Proposal, error) {
		return Proposal{Edits: edits("42"), Report: "completed"}, nil
	})
	e.Diagnosis = diagnosisFunc(func(context.Context, DiagnosisRequest) ([]byte, error) {
		t.Fatal("diagnosis after passed check")
		return nil, nil
	})
	e.Reporter = completionFunc(func(context.Context, Record) (string, error) {
		return "The native check passed.", nil
	})
	r, err := e.Run(context.Background(), "optional-write-failure", c)
	if err != nil || r.State != "succeeded" {
		t.Fatalf("verified result stranded by optional write: %+v %v", r, err)
	}
	if err := e.Revalidate("optional-write-failure", c); err != nil {
		t.Fatalf("authoritative evidence lost: %v", err)
	}
}
