package loop

import (
	"bytes"
	"errors"
	"reflect"
)

// ManagedDoerSchemaVersion is deliberately distinct from legacy fixed v4,
// reusable v5 and contribution v6. It grants no host, provider or runtime rights.
const ManagedDoerSchemaVersion = "aegis.loop.revision.v7"

// ManagedDoerContract pins the selected-file assertion before any source input
// or runtime effect. Source data never replaces the independent expected bytes.
type ManagedDoerContract struct {
	Doer           DoerContract `json:"doer"`
	SourceType     ValueType    `json:"source_type"`
	ExpectedDigest string       `json:"expected_digest"`
}

func (c ManagedDoerContract) valid() bool {
	return c.Doer.Coding == nil && validDoerContract(c.Doer) && c.Doer.MaxAttempts <= 2 &&
		len(c.Doer.WritableFiles) == 1 && c.Doer.WritableFiles[0] == c.Doer.VerifyFile &&
		c.Doer.ExactBytes && c.Doer.ExpectedText != nil && validDigest(c.ExpectedDigest) &&
		sha256Digest([]byte(*c.Doer.ExpectedText)) == c.ExpectedDigest &&
		(c.SourceType == TypeString || c.SourceType == TypeArtifact)
}

func NewManagedDoerRevision(id string, revision uint64, previous string, c ManagedDoerContract) (LoopRevision, LoopValidationResult, error) {
	r := managedDoerCandidate(c)
	r.LoopID, r.Revision, r.PreviousDigest = id, revision, previous
	return NewRevision(r)
}

func managedDoerCandidate(c ManagedDoerContract) LoopRevision {
	r := doerCandidate(c.Doer)
	r.SchemaVersion, r.Doer, r.ManagedDoer = ManagedDoerSchemaVersion, nil, &c
	r.Inputs = []Port{{ID: "source", Type: c.SourceType, Required: true}}
	result := []Port{{ID: "result", Type: TypeArtifact, Required: true}}
	r.Outputs = result
	for i := range r.Steps {
		s := &r.Steps[i]
		switch s.ID {
		case "eligibility":
			s.InputPorts = r.Inputs
		case "verify":
			s.OutputPorts = []Port{{ID: "result", Type: TypeArtifact, Required: false}}
			s.EvidenceClaims = []EvidenceClaim{{Claim: "selected-file-verified", MediaType: "application/octet-stream", ExpectedDigest: c.ExpectedDigest, VerifierID: "aegis-artifact-verifier", PolicyVersion: "aegis.dev/artifact-verification/v1"}}
		case "completion":
			s.InputPorts = result
			s.OutputPorts = result
		case "done":
			s.InputPorts = result
			s.OutputPorts = result
			s.Terminal.OutputMappings = []PortMapping{{SourcePort: "result", TargetPort: "result"}}
		}
	}
	for i := range r.Transitions {
		switch r.Transitions[i].ID {
		case "verified", "completed":
			r.Transitions[i].Mappings = []PortMapping{{SourcePort: "result", TargetPort: "result"}}
		}
	}
	return r
}

func validateManagedDoerRevision(r LoopRevision, add func(string, string, string)) {
	if r.ManagedDoer == nil || !r.ManagedDoer.valid() {
		add("managed_doer.contract", "managed_doer", "v7 requires one selected write, precommitted exact bytes, typed source and one to two passes")
		return
	}
	actual, expected := canonicalRevision(r), canonicalRevision(managedDoerCandidate(*r.ManagedDoer))
	if !reflect.DeepEqual(actual.Inputs, expected.Inputs) || !reflect.DeepEqual(actual.Outputs, expected.Outputs) || actual.EntryStepID != expected.EntryStepID || !reflect.DeepEqual(actual.Steps, expected.Steps) || !reflect.DeepEqual(actual.Transitions, expected.Transitions) || !reflect.DeepEqual(actual.RequiredEvidence, expected.RequiredEvidence) {
		add("managed_doer.shape", "steps", "v7 requires closed selected-file geometry, source and required raw artifact output")
	}
}

// ManagedDoerSource is data, never evidence of admission or successful upstream
// execution. Artifact binding requires separate controller lineage/receipt checks.
type ManagedDoerSource struct {
	Text     *string
	Artifact *ManagedArtifactSource
}

// ManagedArtifactSource is a causal reference, not an inline content envelope.
// The controller must independently reload matching verified bytes and lineage.
type ManagedArtifactSource struct {
	ArtifactID string `json:"artifact_id"`
	AttemptID  string `json:"attempt_id"`
	RunID      string `json:"run_id"`
	ContentRef string `json:"content_ref"`
}

// NormalizeManagedDoerBinding rejects type confusion, unrecognized artifact
// fields, forged malformed references and extra inputs. It does not read a host
// path, infer content from expected_text or authorize any runtime effect.
func NormalizeManagedDoerBinding(r LoopRevision, inputs []DoerInput) (ManagedDoerSource, error) {
	denied := errors.New("exact typed managed Doer source required")
	if r.SchemaVersion != ManagedDoerSchemaVersion || len(validateRevision(r, true)) != 0 || len(inputs) != 1 {
		return ManagedDoerSource{}, denied
	}
	input := inputs[0]
	if input.PortID != "source" || input.Type != r.ManagedDoer.SourceType || len(input.Value) == 0 || len(input.Value) > 65536 || bytes.Equal(bytes.TrimSpace(input.Value), []byte("null")) {
		return ManagedDoerSource{}, denied
	}
	if input.Type == TypeString {
		text, err := decodeStrict[string](input.Value)
		if err != nil || len(text) > 4096 {
			return ManagedDoerSource{}, denied
		}
		return ManagedDoerSource{Text: &text}, nil
	}
	artifact, err := decodeStrict[ManagedArtifactSource](input.Value)
	if err != nil || !validID(artifact.ArtifactID) || !validID(artifact.AttemptID) || !validID(artifact.RunID) || !validDigest(artifact.ContentRef) {
		return ManagedDoerSource{}, denied
	}
	return ManagedDoerSource{Artifact: &artifact}, nil
}
