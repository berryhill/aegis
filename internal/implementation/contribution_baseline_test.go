package implementation

import (
	"bytes"
	"encoding/json"
	"github.com/berryhill/aegis/internal/loop"
	"testing"
)

type baselineMissingStore struct {
	Store
	missing []byte
}

func (s baselineMissingStore) Get(key []byte) ([]byte, error) {
	if bytes.Equal(key, s.missing) {
		return nil, ErrNotFound
	}
	return s.Store.Get(key)
}

func TestContributionBaselineReadbackStates(t *testing.T) {
	kernel, _ := fixture(t)
	p := loop.ContributionExecutionPolicy{Kind: "go-stdlib-behavior.v1", RepositoryPath: "/fixture", GitBinary: "/git", BaseBranch: "main", BuildPackage: "."}
	for _, kind := range []string{"behavior", "security", "launch", "denial"} {
		p.Criteria = append(p.Criteria, loop.ContributionCriterion{ID: kind, Kind: kind, Test: loop.RequiredGoTest{Package: "synthetic", Name: "Test" + kind}})
	}
	for _, scope := range []string{"security", "launch"} {
		p.ReviewAssets = append(p.ReviewAssets, loop.ContributionReviewAsset{ID: scope, Scope: scope, Path: scope + ".md", BaselineDigest: digest([]byte(scope)), ApprovedDigest: digest([]byte(scope))})
	}
	e := &ManagedContribution{DB: kernel.DB}
	r := ContributionRecord{RunID: "run", Policy: p}
	for _, state := range []string{"", "new", "running", "interrupted"} {
		r.State = state
		if err := e.validateRecord("run", r); err != nil {
			t.Fatalf("pre-baseline %q: %v", state, err)
		}
	}
	for _, state := range []string{"phase_boundary", "candidate_accepted", "candidate_budget_exhausted"} {
		r.State = state
		if e.validateRecord("run", r) == nil {
			t.Fatalf("completed %s accepted no baseline", state)
		}
	}
	r.State = "phase_boundary"
	var output []byte
	for i, c := range p.Criteria {
		action := "pass"
		if i == 0 {
			action = "fail"
		}
		b, _ := json.Marshal(map[string]string{"Action": action, "Package": c.Test.Package, "Test": c.Test.Name})
		output = append(output, append(b, '\n')...)
		r.Baseline.RequiredTests = append(r.Baseline.RequiredTests, c.Test)
	}
	r.Baseline.Outcome = "failed"
	r.Baseline.OutputDigest = digest(output)
	if err := e.DB.Put(blobKey(r.Baseline.OutputDigest), output); err != nil {
		t.Fatal(err)
	}
	key := []byte("contribution/baseline/" + digest([]byte("run")))
	wire, _ := json.Marshal(r.Baseline)
	if err := e.DB.Put(key, wire); err != nil {
		t.Fatal(err)
	}
	if err := e.validateRecord("run", r); err != nil {
		t.Fatalf("retained RED boundary: %v", err)
	}
	for _, missing := range [][]byte{key, blobKey(r.Baseline.OutputDigest)} {
		copy := *e
		copy.DB = baselineMissingStore{e.DB, missing}
		if copy.validateRecord("run", r) == nil {
			t.Fatal("missing immutable proof accepted")
		}
	}
	blank := r
	blank.State = "running"
	blank.Baseline = ContributionCheck{}
	if e.validateRecord("run", blank) == nil {
		t.Fatal("blank primary hid produced immutable baseline")
	}
}
