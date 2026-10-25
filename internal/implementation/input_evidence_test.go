package implementation

import (
	"context"
	"encoding/json"
	"testing"
)

func TestManagedInputCustody(t *testing.T) {
	e, c := addContextFixture(t)
	initial, err := CollectRepositoryContext(context.Background(), c, RepositorySourceDigest(c.RepositoryContext))
	if err != nil {
		t.Fatal(err)
	}
	cd, _ := c.Digest()
	d, err := e.contextBlob(initial)
	if err != nil {
		t.Fatal(err)
	}
	r := Record{RunID: "managed", ContractDigest: cd, ContextDigest: d, ManagedInput: true, State: "failed", Passes: []Pass{{ID: "managed/pass/1"}}}
	if err = e.save(r); err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(struct {
		Instruction string  `json:"instruction"`
		Request     Request `json:"request"`
	}{"synthetic transport fixture", Request{Contract: c, RepositoryContext: initial, PassID: r.Passes[0].ID}})
	observe := e.inputObserver(&r, c, r.Passes[0].ID)
	if err = observe(InputObservation{Bytes: wire}); err != nil {
		t.Fatal(err)
	}
	if err = e.revalidateInputs(c, r, false); err != nil {
		t.Fatal(err)
	}
	if e.revalidateInputs(c, r, true) == nil {
		t.Fatal("prepared accepted as delivered")
	}
	if err = observe(InputObservation{Bytes: wire, Completed: true, TurnID: r.Passes[0].ID}); err != nil {
		t.Fatal(err)
	}
	if err = e.revalidateInputs(c, r, true); err != nil {
		t.Fatal(err)
	}
	if _, err = e.History(r.RunID, c); err != nil {
		t.Fatal(err)
	}
	original := *r.Passes[0].Input
	for _, mutate := range []func(*InputEvidence){
		func(v *InputEvidence) { v.PassID = "other/pass/1" },
		func(v *InputEvidence) { v.ContextDigest = "wrong" },
		func(v *InputEvidence) { v.ContractDigest = "wrong" },
		func(v *InputEvidence) { v.TurnID = "wrong" },
		func(v *InputEvidence) { v.InputDigest = "wrong" },
		func(v *InputEvidence) { v.Delivered = false },
	} {
		v := original
		mutate(&v)
		r.Passes[0].Input = &v
		if e.revalidateInputs(c, r, true) == nil {
			t.Fatal("tampered input accepted")
		}
	}
	r.Passes[0].Input = nil
	if e.revalidateInputs(c, r, true) == nil {
		t.Fatal("missing input accepted")
	}
	r.Passes[0].Input = &original
	if err = e.DB.Put(blobKey(original.InputDigest), []byte("tampered")); err != nil {
		t.Fatal(err)
	}
	if e.revalidateInputs(c, r, true) == nil {
		t.Fatal("tampered custody accepted")
	}
}

func TestManagedInputRequiredSyntheticDenied(t *testing.T) {
	e, c := addContextFixture(t)
	e.RequireInputEvidence = true
	if e.revalidateInputs(c, Record{}, true) == nil {
		t.Fatal("synthetic evidence accepted as managed")
	}
}
