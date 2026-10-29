package implementation

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/dgraph-io/badger/v4"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

func TestNativeRecoveryExhaustedNoApply(t *testing.T) {
	e, c := addContextFixture(t)
	c = doerContract(c)
	c.MaxPasses = 1
	e.RequireInputEvidence = true
	e.Decision = &decisionStub{gate: GateDecision{true, true}}
	e.Diagnosis = diagnosisFunc(func(context.Context, DiagnosisRequest) ([]byte, error) { return nil, nil })
	e.Proposer = reportProposer(func(_ context.Context, r Request) (Proposal, error) {
		wire, _ := json.Marshal(struct {
			Instruction string  `json:"instruction"`
			Request     Request `json:"request"`
		}{"synthetic", r})
		if err := r.InputObserver(InputObservation{Bytes: wire}); err != nil {
			return Proposal{}, err
		}
		return Proposal{}, errors.New("before delivery")
	})
	r, err := e.Run(context.Background(), "exhausted", c)
	if err == nil || r.State != "failed" {
		t.Fatal(r, err)
	}
	h, err := e.History(r.RunID, c)
	if err != nil || h.RemainingBudget != 0 || h.Recovery.Effect != "no_apply_proven" || h.Recovery.Reason != "native_recovery_budget_exhausted" {
		t.Fatal(h, err)
	}
	if _, err := e.CheckRecoveryState(r.RunID, c); err == nil {
		t.Fatal("exhausted no-apply accepted")
	}
}

// A real managed reservation with the legacy gate/prepared/delivered sequence.
// No historical rejection or apply evidence is invented for recovery.
func TestLegacyNativeRecoveryReadback(t *testing.T) {
	e, c := addContextFixture(t)
	c = doerContract(c)
	c.MaxPasses = 2
	e.RequireInputEvidence = true
	e.Decision = &decisionStub{gate: GateDecision{true, true}}
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
		if err := r.InputObserver(InputObservation{Bytes: wire, Completed: true, TurnID: r.PassID, RuntimeSessionID: "legacy"}); err != nil {
			return Proposal{}, err
		}
		return Proposal{}, errors.New("unclassified historical failure")
	})
	r, err := e.Run(context.Background(), "legacy", c)
	if err == nil || r.State != "failed" {
		t.Fatal(r, err)
	}
	if len(r.Stages) != 3 || r.Stages[0].Name != "gate" || r.Stages[1].Name != "input_prepared" || r.Stages[2].Name != "input_delivered" {
		t.Fatal("not the exact legacy stage shape", r.Stages)
	}
	disk := e.DB.(testStore).db
	dir := disk.Opts().Dir
	envelope, _ := json.Marshal(struct {
		C  loop.VerifiedImplementation
		ID string
	}{c, r.RunID})
	cf := filepath.Join(t.TempDir(), "contract.json")
	if x := os.WriteFile(cf, envelope, 0600); x != nil {
		t.Fatal(x)
	}
	if x := disk.Close(); x != nil {
		t.Fatal(x)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestLegacyRecoveryRestartChild$", "-test.v")
	cmd.Env = append(os.Environ(), "AEGIS_LEGACY_RECOVERY_DB="+dir, "AEGIS_LEGACY_RECOVERY_CONTRACT="+cf)
	b, x := cmd.CombinedOutput()
	t.Log(string(b))
	if x != nil {
		t.Fatal("restart child", x)
	}
	disk, x = badger.Open(badger.DefaultOptions(dir).WithLogger(nil).WithSyncWrites(true))
	if x != nil {
		t.Fatal(x)
	}
	t.Cleanup(func() { disk.Close() })
	e.DB = testStore{disk}
	before, _ := json.Marshal(r)
	reload := &Executor{DB: e.DB, RequireInputEvidence: true}
	h, err := reload.History(r.RunID, c)
	if err != nil {
		t.Fatal(err)
	}
	// JSON assertion makes the causal baseline an absent service-contract field,
	// not a compile failure against a new type.
	wire, _ := json.Marshal(h)
	var view struct {
		Recovery struct{ Effect, Decision, Reason string }
	}
	if err = json.Unmarshal(wire, &view); err != nil {
		t.Fatal(err)
	}
	if view.Recovery.Effect != "unknown" || view.Recovery.Decision != "policy_decision_required" || view.Recovery.Reason != "native_recovery_effect_evidence_missing" {
		t.Fatalf("legacy recovery has no actionable typed classification: %s", wire)
	}
	if h.CumulativeConsumed != 1 || h.RemainingBudget != 1 || h.ReservedPasses != 1 || h.Recovery.NextAction != "preserve_history_no_supported_reconciliation" {
		t.Fatal(h)
	}
	for _, state := range []string{"cancelled", "expired", "revoked"} {
		changed := r
		changed.State = state
		if err := e.save(changed); err != nil {
			t.Fatal(err)
		}
		terminal, err := reload.History(r.RunID, c)
		if err != nil || terminal.Recovery.Decision != "unavailable" || terminal.Recovery.Reason != "native_recovery_"+state {
			t.Fatal(terminal, err)
		}
		if _, err := reload.CheckRecoveryState(r.RunID, c); err == nil {
			t.Fatal("terminal recovery admitted", state)
		}
	}
	if err := e.save(r); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"wrong_run", "wrong_pass", "corrupt_stage", "substituted_witness"} {
		var changed Record
		json.Unmarshal(before, &changed)
		switch mode {
		case "wrong_run":
			changed.RunID = "another-run"
		case "wrong_pass":
			changed.Stages[1].PassID = "another-run/pass/1"
		case "corrupt_stage":
			changed.Stages[0].Digest = "sha256:bad"
		case "substituted_witness":
			changed.Stages[1].Digest = changed.Stages[0].Digest
		}
		b, _ := json.Marshal(changed)
		if err := e.DB.Put([]byte("implementation/run/"+digest([]byte(r.RunID))), b); err != nil {
			t.Fatal(err)
		}
		if _, err := reload.History(r.RunID, c); !errors.Is(err, ErrRecoveryEvidenceInvalid) {
			t.Fatal(mode, err)
		}
		if err := e.save(r); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if _, err = reload.PrepareContinuation(r.RunID, h.Digest, "successor", 1, c); err == nil {
			t.Fatal("legacy ambiguity accepted")
		}
	}
	for _, name := range []string{"add.go", "add_test.go", "go.mod"} {
		p := filepath.Join(c.Workspace, name)
		b, _ := os.ReadFile(p)
		if err = os.WriteFile(p, append(append([]byte(nil), b...), ' '), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err = reload.PrepareContinuation(r.RunID, h.Digest, "successor", 1, c); err == nil {
			t.Fatal("changed source admitted")
		}
		if err = os.WriteFile(p, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	after, err := reload.Read(r.RunID)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(after)
	if string(before) != string(got) || calls != 1 {
		t.Fatal("recovery changed history or reexecuted runtime")
	}
	if _, err = reload.DB.Get([]byte("implementation/successor/" + digest([]byte(r.RunID)))); !errors.Is(err, ErrNotFound) {
		t.Fatal("denial reserved successor", err)
	}
}

func TestLegacyRecoveryRestartChild(t *testing.T) {
	dir := os.Getenv("AEGIS_LEGACY_RECOVERY_DB")
	if dir == "" {
		t.Skip("child only")
	}
	b, err := os.ReadFile(os.Getenv("AEGIS_LEGACY_RECOVERY_CONTRACT"))
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		C  loop.VerifiedImplementation
		ID string
	}
	if json.Unmarshal(b, &v) != nil {
		t.Fatal("contract")
	}
	db, err := badger.Open(badger.DefaultOptions(dir).WithLogger(nil).WithSyncWrites(true))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	e := &Executor{DB: testStore{db}, RequireInputEvidence: true}
	h, err := e.History(v.ID, v.C)
	if err != nil || h.Recovery.Reason != "native_recovery_effect_evidence_missing" || h.CumulativeConsumed != 1 || h.RemainingBudget != 1 {
		t.Fatal(h, err)
	}
	before, _ := e.Read(v.ID)
	old, _ := json.Marshal(before)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, x := e.PrepareContinuation(v.ID, h.Digest, "forbidden", 1, v.C); x == nil {
				t.Error("restart concurrent admitted")
			}
		}()
	}
	wg.Wait()
	after, _ := e.Read(v.ID)
	now, _ := json.Marshal(after)
	if string(old) != string(now) {
		t.Fatal("mutated")
	}
	if _, err = e.DB.Get([]byte("implementation/successor/" + digest([]byte(v.ID)))); !errors.Is(err, ErrNotFound) {
		t.Fatal("successor", err)
	}
	t.Log("TRUE_PROCESS_RESTART reopened durable Badger; exact legacy classification/counters preserved; eight concurrent attempts denied without successor/history effects")
}
