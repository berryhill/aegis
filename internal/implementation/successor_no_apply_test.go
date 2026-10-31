package implementation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeSuccessorPreparedNoApplyChain(t *testing.T) {
	for _, delivered := range []bool{false, true} {
		t.Run(map[bool]string{false: "prepared", true: "rejected"}[delivered], func(t *testing.T) {
			e, c := addContextFixture(t)
			c = doerContract(c)
			c.MaxPasses = 3
			e.PassLimit = 1
			e.RequireInputEvidence = true
			e.Decision = &decisionStub{gate: GateDecision{true, true}, verdict: Judgment{true, true, true, true, true}}
			e.Diagnosis = diagnosisFunc(func(context.Context, DiagnosisRequest) ([]byte, error) { return nil, nil })
			calls := 0
			changed := "package synthetic\nfunc Add(a,b int) int { return a*b }\n"
			e.Proposer = reportProposer(func(_ context.Context, r Request) (Proposal, error) {
				calls++
				wire, _ := json.Marshal(struct {
					Instruction string  `json:"instruction"`
					Request     Request `json:"request"`
				}{"synthetic", r})
				if err := r.InputObserver(InputObservation{Bytes: wire}); err != nil {
					return Proposal{}, err
				}
				if calls == 2 && !delivered {
					return Proposal{}, errors.New("before delivery")
				}
				if err := r.InputObserver(InputObservation{Bytes: wire, Completed: true, TurnID: r.PassID, RuntimeSessionID: "fixture"}); err != nil {
					return Proposal{}, err
				}
				if calls == 2 {
					if err := r.FailureObserver(ManagedFailure{State: "failed", Code: "doer_proposal_json_invalid"}); err != nil {
						return Proposal{}, err
					}
					return Proposal{}, errors.New("closed rejection")
				}
				text := changed
				if calls == 3 {
					text = addRight
				}
				return Proposal{Edits: []Edit{{Path: "add.go", Content: []byte(text)}}, Report: "fixture"}, nil
			})
			first, err := e.Run(context.Background(), "first", c)
			if err == nil || first.State != "failed" {
				t.Fatal(first, err)
			}
			h, err := e.History("first", c)
			if err != nil {
				t.Fatal(err)
			}
			e.Continuation, err = e.PrepareContinuation("first", h.Digest, "second", 2, c)
			if err != nil {
				t.Fatal(err)
			}
			second, err := e.Run(context.Background(), "second", c)
			if err == nil || second.State != "failed" {
				t.Fatal(second, err)
			}
			h2, err := e.History("second", c)
			if err != nil {
				t.Fatal(err)
			}
			if h2.Recovery.Effect != "no_apply_proven" || h2.CumulativeConsumed != 2 || h2.RemainingBudget != 1 {
				t.Fatalf("lost successor preimage/cap: %+v", h2)
			}
			// Recovery must use the applied predecessor candidate, never the root context.
			if err := os.WriteFile(filepath.Join(c.Workspace, "add.go"), []byte(addWrong), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err = e.PrepareContinuation("second", h2.Digest, "third", 1, c); err == nil {
				t.Fatal("original context substituted for predecessor candidate")
			}
			if err := os.WriteFile(filepath.Join(c.Workspace, "add.go"), []byte(changed), 0600); err != nil {
				t.Fatal(err)
			}
			// Every link must remain exact. Neither current no-apply evidence nor
			// a matching file can repair missing predecessor custody.
			for _, mode := range []string{"wrong_history", "budget_reset", "cycle", "unknown", "implement"} {
				t.Run(mode, func(t *testing.T) {
					wire, _ := json.Marshal(second)
					var bad Record
					if err := json.Unmarshal(wire, &bad); err != nil {
						t.Fatal(err)
					}
					switch mode {
					case "wrong_history":
						bad.PredecessorHistory = "sha256:wrong"
					case "budget_reset":
						bad.ConsumedBefore = 0
					case "cycle":
						bad.PredecessorRun = bad.RunID
					case "unknown":
						bad.Passes[0].Input = nil
					case "implement":
						if err := e.stage(&bad, bad.Passes[0].ID, "implement", "applied or unknown"); err != nil {
							t.Fatal(err)
						}
					}
					if err := e.save(bad); err != nil {
						t.Fatal(err)
					}
					if _, err := e.PrepareContinuation("second", h2.Digest, "third", 1, c); err == nil {
						t.Fatal("unsafe successor admitted")
					}
					if err := e.save(second); err != nil {
						t.Fatal(err)
					}
				})
			}
			e.Continuation, err = e.PrepareContinuation("second", h2.Digest, "third", 1, c)
			if err != nil {
				t.Fatal(err)
			}
			third, err := e.Run(context.Background(), "third", c)
			if err != nil || third.State != "succeeded" {
				t.Fatal(third, err)
			}
			h3, err := e.History("third", c)
			if err != nil || h3.CumulativeConsumed != 3 || h3.RemainingBudget != 0 || calls != 3 {
				t.Fatal(h3, err, calls)
			}
			if _, err = e.PrepareContinuation("third", h3.Digest, "fourth", 1, c); err == nil {
				t.Fatal("cumulative budget reset")
			}
			after, err := e.History("first", c)
			if err != nil || after.Digest != h.Digest {
				t.Fatal("predecessor mutated", err)
			}
		})
	}
}
