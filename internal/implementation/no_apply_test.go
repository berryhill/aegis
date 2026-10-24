package implementation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestManagedNoApplyContinuation(t *testing.T) {
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
		if calls == 1 {
			return Proposal{}, errors.New("synthetic failure before report")
		}
		if err := r.InputObserver(InputObservation{Bytes: wire, Completed: true, TurnID: r.PassID, RuntimeSessionID: "fixture"}); err != nil {
			return Proposal{}, err
		}
		return Proposal{Edits: []Edit{{Path: "add.go", Content: []byte(addRight)}}, Report: "repair"}, nil
	})
	first, err := e.Run(context.Background(), "preapply", c)
	if err == nil || first.State != "failed" {
		t.Fatal(first.State, err)
	}
	h, err := e.History(first.RunID, c)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"running", "interrupted", "expired", "cancelled", "denied", "unknown"} {
		changed := first
		changed.State = state
		e.save(changed)
		if _, err = e.PrepareContinuation(first.RunID, h.Digest, "next", 1, c); err == nil {
			t.Fatal("accepted", state)
		}
	}
	e.save(first)
	for _, name := range []string{"add.go", "add_test.go", "go.mod"} {
		p := filepath.Join(c.Workspace, name)
		b, _ := os.ReadFile(p)
		os.WriteFile(p, append(b, ' '), 0600)
		if _, err = e.PrepareContinuation(first.RunID, h.Digest, "next", 1, c); err == nil {
			t.Fatal("accepted drift", name)
		}
		os.WriteFile(p, b, 0600)
	}
	changed := first
	changed.Passes = append([]Pass(nil), first.Passes...)
	changed.Passes[0].Input = nil
	e.save(changed)
	if _, err = e.PrepareContinuation(first.RunID, h.Digest, "next", 1, c); err == nil {
		t.Fatal("missing input accepted")
	}
	e.save(first)
	changed = first
	if err = e.stage(&changed, first.Passes[0].ID, "implement", "report before apply"); err != nil {
		t.Fatal(err)
	}
	ih, _ := e.History(first.RunID, c)
	if _, err = e.PrepareContinuation(first.RunID, ih.Digest, "next", 1, c); err == nil {
		t.Fatal("implement stage accepted")
	}
	e.save(first)
	e.Continuation, err = e.PrepareContinuation(first.RunID, h.Digest, "next", 1, c)
	if err != nil {
		t.Fatal(err)
	}
	next, err := e.Run(context.Background(), "next", c)
	if err != nil || next.State != "succeeded" {
		t.Fatal(next.State, err)
	}
	nh, err := e.History("next", c)
	if err != nil || nh.CumulativeConsumed != 2 || calls != 2 {
		t.Fatal(nh, err, calls)
	}
	after, err := e.History(first.RunID, c)
	if err != nil || after.Digest != h.Digest || after.Passes[0].Input.Delivered {
		t.Fatal("predecessor changed", err)
	}
	if _, err = e.PrepareContinuation("next", nh.Digest, "third", 1, c); err == nil {
		t.Fatal("exhaustion accepted")
	}
}
