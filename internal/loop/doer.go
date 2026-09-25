package loop

import (
	"encoding/json"
	"errors"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"unicode/utf8"
)

// DoerContract is definition data, not a grant of runtime authority. A worker
// must separately authenticate, seal and freshly admit every runtime effect.
// VerifyFile is a workspace-relative selected regular file; the worker must
// reject symlinks (including parent components) and independently read it.
type DoerContract struct {
	Task          string   `json:"task"`
	Workspace     string   `json:"workspace"`
	WritableFiles []string `json:"writable_files"`
	VerifyFile    string   `json:"verify_file"`
	ExpectedText  *string  `json:"expected_text,omitempty"` // nil means presence-only; pointer to empty string means exact empty text
	MaxAttempts   uint16   `json:"max_attempts"`
}

type DoerOperation string

const (
	DoerEligibility DoerOperation = "laya.eligibility"
	DoerImplement   DoerOperation = "hermes.implement"
	DoerJudgment    DoerOperation = "laya.judgment"
	DoerDiagnosis   DoerOperation = "hermes.diagnosis"
	DoerVerify      DoerOperation = "selected-file.verify"
	DoerCompletion  DoerOperation = "hermes.completion"
)

// Authority is a required controller binding, never chosen by the model.
// Backend names denote fixed adapters, not model access or credentials.
type DoerStepBinding struct {
	Operation DoerOperation `json:"operation"`
	Backend   string        `json:"backend"`
	Authority string        `json:"authority"`
}

const DoerVerifierID = "aegis.doer.selected-file.verifier"
const DoerVerifierPolicy = "aegis.doer.selected-file.v1"

// Digest seals the selected file assertion together with the exact workspace,
// writable paths, task and retry budget before any output exists.
func (c DoerContract) Digest() (string, error) {
	if !validDoerContract(c) {
		return "", errors.New("invalid Doer contract")
	}
	wire, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return sha256Digest(wire), nil
}

func validDoerContract(c DoerContract) bool {
	if c.Task == "" || strings.TrimSpace(c.Task) != c.Task || !utf8.ValidString(c.Task) || len(c.Task) > 4096 || strings.ContainsAny(c.Task, "\x00\r") ||
		!filepath.IsAbs(c.Workspace) || filepath.Clean(c.Workspace) != c.Workspace || c.Workspace == "/" || len(c.Workspace) > 1024 ||
		c.VerifyFile == "" || !utf8.ValidString(c.VerifyFile) || len(c.VerifyFile) > 255 || strings.ContainsAny(c.VerifyFile, "\\\x00\r\n") ||
		path.IsAbs(c.VerifyFile) || path.Clean(c.VerifyFile) != c.VerifyFile || c.VerifyFile == "." || strings.HasPrefix(c.VerifyFile, "../") ||
		c.MaxAttempts < 1 || c.MaxAttempts > 3 {
		return false
	}
	if c.ExpectedText != nil && (!utf8.ValidString(*c.ExpectedText) || len(*c.ExpectedText) > 4096) {
		return false
	}
	if len(c.WritableFiles) == 0 || len(c.WritableFiles) > 128 {
		return false
	}
	seen := make(map[string]bool, len(c.WritableFiles))
	verifiedWritable := false
	for _, file := range c.WritableFiles {
		if file == "" || len(file) > 255 || path.IsAbs(file) || path.Clean(file) != file || strings.ContainsAny(file, "\\\x00\r\n") || seen[file] {
			return false
		}
		for _, component := range strings.Split(file, "/") {
			if component == "" || component == "." || component == ".." || component == ".git" || component == ".env" {
				return false
			}
		}
		seen[file] = true
		verifiedWritable = verifiedWritable || file == c.VerifyFile
	}
	if !verifiedWritable {
		return false
	}
	for _, part := range strings.Split(c.VerifyFile, "/") {
		if part == "." || part == ".." || part == "" {
			return false
		}
	}
	return true
}

// NewDoerRevision constructs the only supported v4 geometry. It does not run
// models, grant authority, or infer an available provider.
func NewDoerRevision(id string, revision uint64, previous string, contract DoerContract) (LoopRevision, LoopValidationResult, error) {
	candidate := doerCandidate(contract)
	candidate.LoopID, candidate.Revision, candidate.PreviousDigest = id, revision, previous
	return NewRevision(candidate)
}

func doerCandidate(c DoerContract) LoopRevision {
	step := func(id string, kind StepKind, op DoerOperation, backend string) Step {
		return Step{ID: id, Kind: kind, Retry: RetryPolicy{MaxAttempts: 1}, Executable: &DoerStepBinding{Operation: op, Backend: backend, Authority: "controller-sealed"}}
	}
	eligibility := step("eligibility", StepGate, DoerEligibility, "laya")
	eligibility.Gate = &GateDefinition{Mode: "exclusive"}
	implement := step("implement", StepAction, DoerImplement, "hermes")
	judgment := step("judgment", StepAction, DoerJudgment, "laya")
	diagnosis := step("diagnosis", StepAction, DoerDiagnosis, "hermes")
	missingInput := step("missing-input", StepAction, DoerDiagnosis, "hermes")
	verify := step("verify", StepGate, DoerVerify, "aegis")
	verify.Gate = &GateDefinition{Mode: "exclusive"}
	contractWire, _ := json.Marshal(c)
	verify.EvidenceClaims = []EvidenceClaim{{Claim: "selected-file-verified", MediaType: "application/octet-stream", ExpectedDigest: sha256Digest(contractWire), VerifierID: DoerVerifierID, PolicyVersion: DoerVerifierPolicy}}
	completion := step("completion", StepAction, DoerCompletion, "hermes")
	done := Step{ID: "done", Kind: StepTerminal, Retry: RetryPolicy{MaxAttempts: 1}, Terminal: &TerminalDefinition{Outcome: OutcomeSucceeded}}
	failed := Step{ID: "failed", Kind: StepTerminal, Retry: RetryPolicy{MaxAttempts: 1}, Terminal: &TerminalDefinition{Outcome: OutcomeFailed}}
	edge := func(id, from, to, condition string, bound uint16) Transition {
		return Transition{ID: id, FromStepID: from, ToStepID: to, Condition: condition, MaxTraversals: bound}
	}
	return LoopRevision{SchemaVersion: DoerRevisionSchemaVersion, Doer: &c, EntryStepID: "eligibility",
		Steps: []Step{eligibility, implement, judgment, diagnosis, missingInput, verify, completion, done, failed},
		Transitions: []Transition{
			edge("eligible", "eligibility", "implement", "eligible", 0), edge("needs-input", "eligibility", "missing-input", "needs_input", 0),
			edge("missing-input-failed", "missing-input", "failed", "", 0),
			edge("implemented", "implement", "judgment", "", c.MaxAttempts), edge("judged", "judgment", "verify", "", c.MaxAttempts),
			edge("diagnosed", "diagnosis", "implement", "", c.MaxAttempts), edge("verified", "verify", "completion", "verified", 0),
			edge("verification-retry", "verify", "diagnosis", "retry", c.MaxAttempts), edge("verification-exhausted", "verify", "failed", "exhausted", 0),
			edge("completed", "completion", "done", "", 0)},
		RequiredEvidence: []EvidenceRequirement{{Claim: "selected-file-verified", ProducerStepID: "verify"}}}
}

func validateDoerRevision(r LoopRevision, add func(string, string, string)) {
	if r.Doer == nil || !validDoerContract(*r.Doer) {
		add("doer.contract", "doer", "v4 requires a bounded task, selected relative file and one to three attempts")
		return
	}
	expected := canonicalRevision(doerCandidate(*r.Doer))
	actual := canonicalRevision(r)
	if len(actual.Inputs) != 0 || len(actual.Outputs) != 0 || actual.EntryStepID != expected.EntryStepID ||
		!reflect.DeepEqual(actual.Steps, expected.Steps) || !reflect.DeepEqual(actual.Transitions, expected.Transitions) ||
		!reflect.DeepEqual(actual.RequiredEvidence, expected.RequiredEvidence) {
		add("doer.shape", "steps", "v4 requires the closed gate/action/verifier geometry and sealed bindings")
	}
}
