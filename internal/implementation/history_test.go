package implementation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeHistoryCountsFailedReservation(t *testing.T) {
	e, c := addContextFixture(t)
	e.Proposer = proposerFunc(func(context.Context, Request) ([]Edit, error) { return nil, errors.New("proposal failed") })
	r, err := e.Run(context.Background(), "reserved", c)
	if err == nil || r.State != "failed" {
		t.Fatalf("%+v %v", r, err)
	}
	h, err := e.History("reserved", c)
	if err != nil || h.ReservedPasses != 1 || h.CumulativeConsumed != 1 || h.ConsumedBefore != 0 || h.RemainingBudget != uint16(c.MaxPasses)-1 || h.PredecessorRun != "" || h.State != "failed" || h.ContractDigest != r.ContractDigest || h.ContextDigest == "" {
		t.Fatalf("%+v %v", h, err)
	}
	if _, err = e.CheckRecoveryState("reserved", c); err == nil {
		t.Fatal("incomplete pass recovered")
	}
	before := h.Digest
	if _, err = e.Run(context.Background(), "reserved", c); err == nil {
		t.Fatal("replayed reservation")
	}
	h, err = e.History("reserved", c)
	if err != nil || h.Digest != before {
		t.Fatal("historical failure changed")
	}
	c.Task = "another task"
	if _, err = e.History("reserved", c); err == nil {
		t.Fatal("swapped task accepted")
	}
	if _, err = e.History("missing", c); err == nil {
		t.Fatal("missing history accepted")
	}
}

// Host bytes changed before completed custody are not a safe recovery preimage.
func TestNativeHistoryPartialApplyDeniesRecovery(t *testing.T) {
	e, c := addContextFixture(t)
	c.MaxPasses = 2
	e.Proposer = proposerFunc(func(context.Context, Request) ([]Edit, error) {
		if err := os.WriteFile(filepath.Join(c.Workspace, "add.go"), []byte(addRight), 0600); err != nil {
			t.Fatal(err)
		}
		return nil, errors.New("interrupted before candidate custody")
	})
	r, err := e.Run(context.Background(), "partial", c)
	if err == nil || r.State != "failed" || len(r.Passes) != 1 {
		t.Fatalf("partial %+v %v", r, err)
	}
	h, err := e.History("partial", c)
	if err != nil || h.RemainingBudget != 1 {
		t.Fatalf("partial readback %+v %v", h, err)
	}
	if _, err = e.PrepareContinuation("partial", h.Digest, "correction", 1, c); err == nil {
		t.Fatal("partial host state adopted")
	}
}

func TestNativeHistoryRejectsCorruptPassIdentity(t *testing.T) {
	e, c := addContextFixture(t)
	cd, err := c.Digest()
	if err != nil {
		t.Fatal(err)
	}
	r := Record{RunID: "history", ContractDigest: cd, State: "failed", Passes: []Pass{{ID: "swapped/pass/1"}}}
	if err = e.save(r); err != nil {
		t.Fatal(err)
	}
	if _, err = e.History("history", c); err == nil {
		t.Fatal("swapped identity accepted")
	}
	r.Passes = []Pass{{ID: "history/pass/1", Passed: true}}
	if err = e.save(r); err != nil {
		t.Fatal(err)
	}
	if _, err = e.History("history", c); err == nil {
		t.Fatal("missing acceptance evidence accepted")
	}
	r.State = "unrecognized"
	if err = e.save(r); err != nil {
		t.Fatal(err)
	}
	if _, err = e.History("history", c); err == nil {
		t.Fatal("unknown state accepted")
	}
}
