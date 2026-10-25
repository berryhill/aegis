package implementation

import (
	"context"
	"encoding/json"
	"testing"
)

// Synthetic observer/proposer exercises native custody and the real offline
// checker. Only orchestration boundary fixtures exercise HermesPatchProposer;
// neither fixture is live-provider acceptance.
func TestManagedInputContinuationReload(t *testing.T) {
	e, c := addContextFixture(t)
	c.MaxPasses = 2
	e.RequireInputEvidence = true
	e.PassLimit = 1
	calls := 0
	e.Proposer = proposerFunc(func(_ context.Context, req Request) ([]Edit, error) {
		calls++
		if req.RepositoryContext.Files[0].Text != addWrong || req.RepositoryContext.Files[1].Text != addTests {
			t.Fatal("original source or read-only tests lost after correction")
		}
		wire, err := json.Marshal(struct {
			Instruction string  `json:"instruction"`
			Request     Request `json:"request"`
		}{"synthetic input continuation", req})
		if err != nil {
			t.Fatal(err)
		}
		if err = req.InputObserver(InputObservation{Bytes: wire}); err != nil {
			t.Fatal(err)
		}
		if err = req.InputObserver(InputObservation{Bytes: wire, Completed: true, TurnID: req.PassID, RuntimeSessionID: "synthetic-session"}); err != nil {
			t.Fatal(err)
		}
		text := "package synthetic\nfunc Add(a,b int) int { return a*b }\n"
		if calls == 2 {
			text = addRight
		}
		return []Edit{{Path: "add.go", Content: []byte(text)}}, nil
	})
	first, err := e.Run(context.Background(), "managed-first", c)
	if err == nil || first.State != "failed" {
		t.Fatalf("first state %s: %v", first.State, err)
	}
	h, err := e.History(first.RunID, c)
	if err != nil || h.RemainingBudget != 1 || !h.Passes[0].Input.Delivered {
		t.Fatalf("failed history: %v", err)
	}
	e.Continuation, err = e.PrepareContinuation(first.RunID, h.Digest, "managed-next", 1, c)
	if err != nil {
		t.Fatal(err)
	}
	e.PassLimit = 0
	next, err := e.Run(context.Background(), "managed-next", c)
	if err != nil || next.State != "succeeded" || calls != 2 {
		t.Fatalf("next state %s calls %d: %v", next.State, calls, err)
	}
	// Fresh executor reloads all commitments rather than trusting returned records.
	reload := &Executor{DB: e.DB, RequireInputEvidence: true}
	if err = reload.Revalidate(next.RunID, c); err != nil {
		t.Fatal(err)
	}
	nh, err := reload.History(next.RunID, c)
	if err != nil || nh.ContextDigest != h.ContextDigest || nh.CumulativeConsumed != 2 || nh.RemainingBudget != 0 {
		t.Fatalf("continuation provenance: %v", err)
	}
	after, err := reload.History(first.RunID, c)
	if err != nil || after.Digest != h.Digest {
		t.Fatal("failed predecessor provenance changed", err)
	}
	original, err := reload.Read(next.RunID)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		mutate func(*Record)
	}{
		{"missing", func(r *Record) { r.Passes[0].Input = nil }},
		{"swapped_pass", func(r *Record) { r.Passes[0].Input = first.Passes[0].Input }},
		{"wrong_context", func(r *Record) { r.Passes[0].Input.ContextDigest = "wrong" }},
		{"wrong_contract", func(r *Record) { r.Passes[0].Input.ContractDigest = "wrong" }},
		{"wrong_commitment", func(r *Record) { r.Passes[0].Input.InputDigest = "wrong" }},
		{"false_delivery", func(r *Record) { r.Passes[0].Input.Delivered = false }},
		{"missing_delivery_stage", func(r *Record) {
			for i, s := range r.Stages {
				if s.Name == "input_delivered" {
					r.Stages = append(r.Stages[:i], r.Stages[i+1:]...)
					break
				}
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(original)
			if err != nil {
				t.Fatal(err)
			}
			var changed Record
			if err = json.Unmarshal(b, &changed); err != nil {
				t.Fatal(err)
			}
			tc.mutate(&changed)
			if err = reload.save(changed); err != nil {
				t.Fatal(err)
			}
			if _, err = reload.History(next.RunID, c); err == nil {
				t.Fatal("tampered history accepted")
			}
			if err = reload.Revalidate(next.RunID, c); err == nil {
				t.Fatal("tampered final acceptance accepted")
			}
			if err = reload.save(original); err != nil {
				t.Fatal(err)
			}
		})
	}
	if err = reload.DB.Put(blobKey(original.Passes[0].Input.InputDigest), []byte("tampered")); err != nil {
		t.Fatal(err)
	}
	if _, err = reload.History(next.RunID, c); err == nil {
		t.Fatal("tampered blob history accepted")
	}
	if err = reload.Revalidate(next.RunID, c); err == nil {
		t.Fatal("tampered blob final acceptance accepted")
	}
}
