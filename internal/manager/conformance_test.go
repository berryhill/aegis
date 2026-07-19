package manager

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCandidatesAreTraceableAndNoUncertifiedDefault(t *testing.T) {
	candidates := Candidates()
	if len(candidates) != 4 {
		t.Fatalf("candidate count %d", len(candidates))
	}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if candidate.ID == "" || candidate.OllamaName == "" || candidate.Publisher == "" || candidate.Source == "" || candidate.License == "" || candidate.LicenseURL == "" {
			t.Fatalf("incomplete candidate %#v", candidate)
		}
		if candidate.Default {
			t.Fatalf("uncertified default selected: %s", candidate.ID)
		}
		if seen[candidate.ID] {
			t.Fatalf("duplicate candidate %s", candidate.ID)
		}
		seen[candidate.ID] = true
	}
}

func TestCertificationRequiresEveryExactCase(t *testing.T) {
	var results []ConformanceResult
	for _, test := range ConformanceCorpus() {
		results = append(results, ConformanceResult{CaseID: test.ID, Passed: true, Reason: "fixture-pass"})
	}
	if err := ValidateCertification(results); err != nil {
		t.Fatal(err)
	}
	results[0].Passed = false
	if err := ValidateCertification(results); err == nil {
		t.Fatal("critical failure certified")
	}
}

type countingConformanceExecutor struct {
	calls  []string
	failAt int
}

func (e *countingConformanceExecutor) Execute(_ context.Context, test ConformanceCase) ([]byte, error) {
	e.calls = append(e.calls, test.ID)
	if len(e.calls) == e.failAt {
		return nil, errors.New("fixture transport failure")
	}
	proposal := "null"
	if test.ExpectedOperation != "" {
		proposal = `{"operation":"` + string(test.ExpectedOperation) + `","arguments":{}}`
	}
	return []byte(`{"schema_version":"` + ResponseSchemaVersion + `","kind":"` + test.ExpectedKind + `","message":"safe","proposal":` + proposal + `}`), nil
}

func TestCertificationFailureNamesCaseStopsAndWritesNoArtifact(t *testing.T) {
	executor := &countingConformanceExecutor{failAt: 2}
	candidate := Candidates()[0]
	cert, err := RunCertification(context.Background(), executor, candidate, candidate.OllamaName, "sha256:"+strings.Repeat("b", 64), "Q4", "0.18.2", "0.32.0", 65536, time.Now())
	var failure *ConformanceFailure
	if !errors.As(err, &failure) || failure.CaseID != ConformanceCorpus()[1].ID || failure.Reason != ReasonGatewayProtocol {
		t.Fatalf("failure=%v", err)
	}
	if len(executor.calls) != 2 {
		t.Fatalf("corpus continued: %v", executor.calls)
	}
	path := filepath.Join(t.TempDir(), "certification.json")
	if err := SaveCertification(path, cert); err == nil {
		t.Fatal("failed certification was saveable")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed certification artifact exists: %v", err)
	}
}

func TestSuccessfulCertificationRunsExactCorpusAndSavesAtomically(t *testing.T) {
	executor := &countingConformanceExecutor{}
	candidate := Candidates()[0]
	cert, err := RunCertification(context.Background(), executor, candidate, candidate.OllamaName, "sha256:"+strings.Repeat("b", 64), "Q4", "0.18.2", "0.32.0", 65536, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	cases := ConformanceCorpus()
	if len(executor.calls) != len(cases) {
		t.Fatalf("calls=%v", executor.calls)
	}
	for index := range cases {
		if executor.calls[index] != cases[index].ID {
			t.Fatalf("case %d=%s want %s", index, executor.calls[index], cases[index].ID)
		}
	}
	path := filepath.Join(t.TempDir(), "certification.json")
	if err := SaveCertification(path, cert); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".new"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("atomic temporary remains: %v", err)
	}
}

func TestSupportedOllamaVersionBoundaries(t *testing.T) {
	for value, want := range map[string]bool{"0.17.9": false, "0.18.0": true, "0.32.0": true, "0.99.9": true, "1.0.0": false, "invalid": false} {
		if got := supportedOllamaVersion(value); got != want {
			t.Fatalf("supportedOllamaVersion(%q) = %t, want %t", value, got, want)
		}
	}
}
