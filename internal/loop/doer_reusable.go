package loop

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
)

// DoerReusableContract fixes the retry budget, not task-specific paths or authority.
// A v5 definition is inert until an independently authorized Graph run binds inputs.
type DoerReusableContract struct {
	MaxAttempts uint16 `json:"max_attempts"`
}

// DoerInput is one typed, JSON-encoded Graph-to-Loop input binding.
type DoerInput struct {
	PortID string          `json:"port_id"`
	Type   ValueType       `json:"type"`
	Value  json.RawMessage `json:"value"`
}

func doerReusablePorts() []Port {
	return []Port{
		{ID: "task", Type: TypeString, Required: true},
		{ID: "workspace", Type: TypeString, Required: true},
		{ID: "writable_files", Type: TypeArray, Required: true},
		{ID: "verify_file", Type: TypeString, Required: true},
		{ID: "expected_text", Type: TypeString},
	}
}

// NewDoerReusableRevision builds the closed v5 geometry without selecting a
// provider, granting a mandate, publishing, activating or executing anything.
func NewDoerReusableRevision(id string, revision uint64, previous string, contract DoerReusableContract) (LoopRevision, LoopValidationResult, error) {
	candidate := doerReusableCandidate(contract)
	candidate.LoopID, candidate.Revision, candidate.PreviousDigest = id, revision, previous
	return NewRevision(candidate)
}

func doerReusableCandidate(c DoerReusableContract) LoopRevision {
	// Reuse the closed v4 topology, but remove all task-specific material from
	// the immutable definition. Historical v4 bytes and digests are unchanged.
	base := doerCandidate(DoerContract{Task: "placeholder", Workspace: "/placeholder", WritableFiles: []string{"file"}, VerifyFile: "file", MaxAttempts: c.MaxAttempts})
	base.SchemaVersion, base.Doer, base.DoerReusable = DoerReusableSchemaVersion, nil, &c
	base.Inputs = doerReusablePorts()
	for i := range base.Steps {
		if base.Steps[i].ID == "eligibility" {
			base.Steps[i].InputPorts = doerReusablePorts()
		}
		if base.Steps[i].ID == "verify" {
			base.Steps[i].EvidenceClaims = []EvidenceClaim{{Claim: "selected-file-verified", MediaType: "application/json", VerifierID: DoerVerifierID, PolicyVersion: DoerVerifierPolicy}}
		}
	}
	return base
}

func validateDoerReusableRevision(r LoopRevision, add func(string, string, string)) {
	if r.DoerReusable == nil || r.DoerReusable.MaxAttempts < 1 || r.DoerReusable.MaxAttempts > 3 {
		add("doer_reusable.contract", "doer_reusable", "v5 requires one to three attempts")
		return
	}
	expected := canonicalRevision(doerReusableCandidate(*r.DoerReusable))
	actual := canonicalRevision(r)
	if !reflect.DeepEqual(actual.Inputs, expected.Inputs) || len(actual.Outputs) != 0 || actual.EntryStepID != expected.EntryStepID ||
		!reflect.DeepEqual(actual.Steps, expected.Steps) || !reflect.DeepEqual(actual.Transitions, expected.Transitions) ||
		!reflect.DeepEqual(actual.RequiredEvidence, expected.RequiredEvidence) {
		add("doer_reusable.shape", "steps", "v5 requires closed typed inputs, steps, transitions and verifier")
	}
}

// NormalizeDoerBinding validates exact typed run inputs and returns a bounded
// task contract. It confers no write, session, model or Queue authority.
func NormalizeDoerBinding(revision LoopRevision, inputs []DoerInput) (DoerContract, error) {
	if revision.SchemaVersion != DoerReusableSchemaVersion || len(validateRevision(revision, true)) != 0 {
		return DoerContract{}, errors.New("invalid v5 Doer revision")
	}
	if len(inputs) < 4 || len(inputs) > 5 {
		return DoerContract{}, errors.New("v5 Doer requires four typed inputs and at most one optional input")
	}
	declared := portsByID(doerReusablePorts())
	values := make(map[string]json.RawMessage, len(inputs))
	for _, input := range inputs {
		port, ok := declared[input.PortID]
		if !ok || port.Type != input.Type || values[input.PortID] != nil || len(input.Value) == 0 || len(input.Value) > 65536 {
			return DoerContract{}, fmt.Errorf("invalid or duplicate Doer input %q", input.PortID)
		}
		if _, err := decodeStrict[any](input.Value); err != nil {
			return DoerContract{}, fmt.Errorf("invalid Doer input %q: %w", input.PortID, err)
		}
		values[input.PortID] = input.Value
	}
	for _, port := range doerReusablePorts() {
		if port.Required && values[port.ID] == nil {
			return DoerContract{}, fmt.Errorf("missing Doer input %q", port.ID)
		}
	}
	c := DoerContract{MaxAttempts: revision.DoerReusable.MaxAttempts}
	fields := []struct {
		id     string
		target any
	}{
		{"task", &c.Task}, {"workspace", &c.Workspace}, {"writable_files", &c.WritableFiles}, {"verify_file", &c.VerifyFile},
	}
	for _, field := range fields {
		if bytes.Equal(bytes.TrimSpace(values[field.id]), []byte("null")) || json.Unmarshal(values[field.id], field.target) != nil {
			return DoerContract{}, fmt.Errorf("Doer input %q has wrong type", field.id)
		}
	}
	if raw := values["expected_text"]; raw != nil {
		var text string
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &text) != nil {
			return DoerContract{}, errors.New("Doer input expected_text has wrong type")
		}
		c.ExpectedText = &text
	}
	if !validDoerContract(c) {
		return DoerContract{}, errors.New("invalid bounded Doer binding")
	}
	return c, nil
}
