package manager

import "testing"

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

func TestSupportedOllamaVersionBoundaries(t *testing.T) {
	for value, want := range map[string]bool{"0.17.9": false, "0.18.0": true, "0.32.0": true, "0.99.9": true, "1.0.0": false, "invalid": false} {
		if got := supportedOllamaVersion(value); got != want {
			t.Fatalf("supportedOllamaVersion(%q) = %t, want %t", value, got, want)
		}
	}
}
