package implementation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestManagedDeliveredNoApplyContinuation(t *testing.T) {
	e, c := addContextFixture(t)
	c = doerContract(c)
	c.MaxPasses = 2
	e.RequireInputEvidence = true
	e.Decision = &decisionStub{gate: GateDecision{true, true}, verdict: Judgment{true, true, true, true, true}}
	e.Diagnosis = diagnosisFunc(func(context.Context, DiagnosisRequest) ([]byte, error) { return nil, nil })
	calls := 0
	e.Proposer = reportProposer(func(_ context.Context, r Request) (Proposal, error) {
		calls++
		wire, _ := json.Marshal(struct {
			Instruction string  `json:"instruction"`
			Request     Request `json:"request"`
		}{"synthetic", r})
		if err := r.InputObserver(InputObservation{Bytes: wire}); err != nil {
			return Proposal{}, err
		}
		if err := r.InputObserver(InputObservation{Bytes: wire, Completed: true, TurnID: r.PassID, RuntimeSessionID: "fixture"}); err != nil {
			return Proposal{}, err
		}
		if calls == 1 {
			if err := r.FailureObserver(ManagedFailure{State: "failed", Code: "doer_proposal_json_invalid"}); err != nil {
				return Proposal{}, err
			}
			return Proposal{}, errors.New("closed decode failure")
		}
		return Proposal{Edits: []Edit{{Path: "add.go", Content: []byte(addRight)}}, Report: "repair"}, nil
	})
	first, err := e.Run(context.Background(), "delivered", c)
	if err == nil || first.State != "failed" {
		t.Fatal(first.State, err)
	}
	reload := &Executor{DB: e.DB, RequireInputEvidence: true}
	h, err := reload.History(first.RunID, c)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"unknown", "missing_failure", "implement", "missing_delivery", "corrupt_stage"} {
		t.Run(mode, func(t *testing.T) {
			b, _ := json.Marshal(first)
			var changed Record
			json.Unmarshal(b, &changed)
			switch mode {
			case "unknown":
				changed.Passes[0].Failure = &ManagedFailure{State: "failed", Code: "step_error"}
			case "missing_failure":
				changed.Passes[0].Failure = nil
			case "implement":
				if err := e.stage(&changed, changed.Passes[0].ID, "implement", "private report"); err != nil {
					t.Fatal(err)
				}
			case "missing_delivery":
				for i, s := range changed.Stages {
					if s.Name == "input_delivered" {
						changed.Stages = append(changed.Stages[:i], changed.Stages[i+1:]...)
						break
					}
				}
			case "corrupt_stage":
				changed.Stages[0].Digest = "sha256:bad"
			}
			if err := e.save(changed); err != nil {
				t.Fatal(err)
			}
			hh, _ := reload.History(first.RunID, c)
			if _, err := reload.PrepareContinuation(first.RunID, hh.Digest, "next", 1, c); err == nil {
				t.Fatal("unsafe continuation accepted")
			}
			if err := e.save(first); err != nil {
				t.Fatal(err)
			}
		})
	}
	e.Continuation, err = reload.PrepareContinuation(first.RunID, h.Digest, "next", 1, c)
	if err != nil {
		t.Fatal(err)
	}
	next, err := e.Run(context.Background(), "next", c)
	if err != nil || next.State != "succeeded" {
		t.Fatal(next.State, err)
	}
	nh, err := reload.History("next", c)
	if err != nil || nh.CumulativeConsumed != 2 || nh.RemainingBudget != 0 || calls != 2 {
		t.Fatal(nh, err, calls)
	}
	if _, err := reload.PrepareContinuation("next", nh.Digest, "third", 1, c); err == nil {
		t.Fatal("budget reset")
	}
	if _, err := e.Run(context.Background(), "next", c); err == nil {
		t.Fatal("replay accepted")
	}
}
