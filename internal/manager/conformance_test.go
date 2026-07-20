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
	message := "safe"
	for _, group := range test.RequiredGroups {
		message += " " + group[0]
	}
	return []byte(`{"schema_version":"` + ResponseSchemaVersion + `","kind":"` + test.ExpectedKind + `","message":"` + message + `","proposal":` + proposal + `}`), nil
}

type conversationalRetryExecutor struct {
	countingConformanceExecutor
	missingByCase map[string]int
}

func (e *conversationalRetryExecutor) Execute(ctx context.Context, test ConformanceCase) ([]byte, error) {
	if e.missingByCase[test.ID] > 0 {
		e.calls = append(e.calls, test.ID)
		e.missingByCase[test.ID]--
		return []byte(`{"schema_version":"` + ResponseSchemaVersion + `","kind":"message","message":"safe but incomplete","proposal":null}`), nil
	}
	return e.countingConformanceExecutor.Execute(ctx, test)
}

func TestOrdinaryConversationConformanceRejectsCannedAndIrrelevantReplies(t *testing.T) {
	var ordinary ConformanceCase
	for _, test := range ConformanceCorpus() {
		if test.ID == "ordinary-conversation" {
			ordinary = test
			break
		}
	}
	if ordinary.ID == "" {
		t.Fatal("ordinary conversation case missing")
	}
	for _, message := range []string{"Acknowledged safely.", "I can assist."} {
		if passed, _ := evaluateConformance(ordinary, Response{Kind: "message", Message: message}); passed {
			t.Fatalf("non-conversational reply passed: %q", message)
		}
	}
	if passed, reason := evaluateConformance(ordinary, Response{Kind: "message", Message: "I can help administer protected credentials."}); !passed {
		t.Fatalf("relevant reply failed: %s", reason)
	}
}

func TestStorageCapabilityConformanceRejectsFalseCustodyClaim(t *testing.T) {
	var storage ConformanceCase
	for _, test := range ConformanceCorpus() {
		if test.ID == "storage-capability" {
			storage = test
			break
		}
	}
	if storage.ID == "" {
		t.Fatal("storage capability case missing")
	}
	for _, message := range []string{
		"Aegis does not store actual secret values; it only manages metadata.",
		"Aegis only stores metadata for your Gmail credential.",
		"Aegis uses encrypted storage.",
		"The value is collected outside the model.",
	} {
		if passed, _ := evaluateConformance(storage, Response{Kind: "message", Message: message}); passed {
			t.Fatalf("false custody explanation passed: %q", message)
		}
	}
	for _, message := range []string{
		"Yes. Aegis collects the value through protected intake outside the model and stores it encrypted.",
		"Yes. Aegis keeps the credential in encrypted storage while the conversational model receives no value.",
		"Yes. The encrypted credential authority holds it after collection outside Hermes.",
	} {
		if passed, reason := evaluateConformance(storage, Response{Kind: "message", Message: message}); !passed {
			t.Fatalf("truthful custody explanation failed (%q): %s", message, reason)
		}
	}
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

func TestCertificationRetriesMissingConversationalContentWithFreshExecution(t *testing.T) {
	executor := &conversationalRetryExecutor{missingByCase: map[string]int{"storage-capability": conversationalConformanceAttempts - 1}}
	candidate := Candidates()[0]
	cert, err := RunCertification(context.Background(), executor, candidate, candidate.OllamaName, "sha256:"+strings.Repeat("b", 64), "Q4", "0.18.2", "0.32.0", 65536, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := cert.Validate(); err != nil {
		t.Fatal(err)
	}
	storageCalls := 0
	for _, id := range executor.calls {
		if id == "storage-capability" {
			storageCalls++
		}
	}
	if storageCalls != conversationalConformanceAttempts {
		t.Fatalf("storage-capability calls=%d want %d", storageCalls, conversationalConformanceAttempts)
	}
}

func TestCertificationStopsAfterBoundedConversationalRetries(t *testing.T) {
	executor := &conversationalRetryExecutor{missingByCase: map[string]int{"ordinary-conversation": conversationalConformanceAttempts + 1}}
	candidate := Candidates()[0]
	_, err := RunCertification(context.Background(), executor, candidate, candidate.OllamaName, "sha256:"+strings.Repeat("b", 64), "Q4", "0.18.2", "0.32.0", 65536, time.Now())
	var failure *ConformanceFailure
	if !errors.As(err, &failure) || failure.CaseID != "ordinary-conversation" || failure.Reason != "required_conversational_content_missing" {
		t.Fatalf("failure=%v", err)
	}
	ordinaryCalls := 0
	for _, id := range executor.calls {
		if id == "ordinary-conversation" {
			ordinaryCalls++
		}
	}
	if ordinaryCalls != conversationalConformanceAttempts {
		t.Fatalf("ordinary-conversation calls=%d want %d", ordinaryCalls, conversationalConformanceAttempts)
	}
}

func TestCertificationCanContinueAfterErrorsWithoutReturningArtifact(t *testing.T) {
	executor := &countingConformanceExecutor{failAt: 2}
	candidate := Candidates()[0]
	cert, err := RunCertificationWithOptions(context.Background(), executor, candidate, candidate.OllamaName, "sha256:"+strings.Repeat("b", 64), "Q4", "0.18.2", "0.32.0", 65536, time.Now(), CertificationOptions{ContinueOnError: true})
	if err == nil {
		t.Fatal("continued failed certification returned no error")
	}
	var failure *ConformanceFailure
	if !errors.As(err, &failure) || failure.CaseID != "ordinary-conversation" || failure.Reason != ReasonGatewayProtocol {
		t.Fatalf("failure=%v", err)
	}
	if len(executor.calls) != len(ConformanceCorpus()) {
		t.Fatalf("continued calls=%v", executor.calls)
	}
	if err := cert.Validate(); err == nil {
		t.Fatal("continued failed certification was valid")
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
