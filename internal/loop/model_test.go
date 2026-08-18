package loop

import (
	"bytes"
	"strings"
	"testing"
)

func publicationProvenance(t *testing.T, revision LoopRevision, validation LoopValidationResult) PublicationProvenance {
	t.Helper()
	digest := "sha256:" + strings.Repeat("c", 64)
	value, err := NewPublicationProvenance(PublicationProvenance{
		Loop:           NewProvenanceRevision(revision.LoopID, revision.Revision, revision.Digest),
		PublisherAgent: NewProvenanceRevision("agent-test", 1, digest),
		Authority:      NewProvenanceDigest("authority-test", digest),
		MandateID:      "mandate-test", StanzaID: "stanza-test", Runtime: ProvenanceRuntime{Runtime: "hermes-agent"},
		Charter: NewProvenanceRevision("charter-test", 1, digest), ValidationDigest: validation.Digest,
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func branchJoinCandidate() LoopRevision {
	value := Port{ID: "value", Type: TypeString, Required: true}
	return LoopRevision{
		LoopID: "loop.branch-join", Revision: 1,
		Inputs: []Port{value}, Outputs: []Port{{ID: "result", Type: TypeString, Required: true}},
		EntryStepID: "prepare",
		Steps: []Step{
			{ID: "prepare", Kind: StepAction, InputPorts: []Port{value}, OutputPorts: []Port{value}, Retry: RetryPolicy{MaxAttempts: 2}, EvidenceClaims: []EvidenceClaim{{Claim: "prepared", MediaType: "application/json"}}},
			{ID: "route", Kind: StepGate, InputPorts: []Port{value}, OutputPorts: []Port{value}, Retry: RetryPolicy{MaxAttempts: 1}, Gate: &GateDefinition{Mode: "exclusive"}},
			{ID: "left", Kind: StepAction, InputPorts: []Port{value}, OutputPorts: []Port{value}, Retry: RetryPolicy{MaxAttempts: 2}},
			{ID: "right", Kind: StepAction, InputPorts: []Port{value}, OutputPorts: []Port{value}, Retry: RetryPolicy{MaxAttempts: 2}},
			{ID: "join", Kind: StepJoin, InputPorts: []Port{value}, OutputPorts: []Port{value}, Retry: RetryPolicy{MaxAttempts: 1}},
			{ID: "done", Kind: StepTerminal, InputPorts: []Port{value}, OutputPorts: []Port{{ID: "final", Type: TypeString, Required: true}}, Retry: RetryPolicy{MaxAttempts: 1}, Terminal: &TerminalDefinition{Outcome: OutcomeSucceeded, OutputMappings: []PortMapping{{SourcePort: "final", TargetPort: "result"}}}},
		},
		Transitions: []Transition{
			{ID: "prepare-route", FromStepID: "prepare", ToStepID: "route", Mappings: []PortMapping{{SourcePort: "value", TargetPort: "value"}}},
			{ID: "route-left", FromStepID: "route", ToStepID: "left", Condition: "left", Mappings: []PortMapping{{SourcePort: "value", TargetPort: "value"}}},
			{ID: "route-right", FromStepID: "route", ToStepID: "right", Condition: "right", Mappings: []PortMapping{{SourcePort: "value", TargetPort: "value"}}},
			{ID: "left-join", FromStepID: "left", ToStepID: "join", Mappings: []PortMapping{{SourcePort: "value", TargetPort: "value"}}},
			{ID: "right-join", FromStepID: "right", ToStepID: "join", Mappings: []PortMapping{{SourcePort: "value", TargetPort: "value"}}},
			{ID: "join-done", FromStepID: "join", ToStepID: "done", Mappings: []PortMapping{{SourcePort: "value", TargetPort: "value"}}},
		},
		RequiredEvidence: []EvidenceRequirement{{Claim: "prepared", ProducerStepID: "prepare"}},
	}
}

func TestBranchJoinRevisionValidatesAndDigestsDeterministically(t *testing.T) {
	first, validation, err := NewRevision(branchJoinCandidate())
	if err != nil {
		t.Fatal(err)
	}
	if validation.Outcome != ValidationValid || validation.RevisionDigest != first.Digest || !validDigest(validation.Digest) {
		t.Fatalf("unexpected validation result: %+v", validation)
	}
	const wantRevisionDigest = "sha256:5fe67abc18a6461b1d18627d8ee465c010577d36b1509bdc485f93262cedb1c6"
	const wantValidationDigest = "sha256:a187322386e86483a647bc3e1a2557cd1de4ddc3c03684d9662eeb60b7c939a0"
	if first.Digest != wantRevisionDigest || validation.Digest != wantValidationDigest {
		t.Fatalf("canonical digest vector changed: revision=%s validation=%s", first.Digest, validation.Digest)
	}
	candidate := branchJoinCandidate()
	candidate.Steps[0], candidate.Steps[5] = candidate.Steps[5], candidate.Steps[0]
	candidate.Transitions[0], candidate.Transitions[5] = candidate.Transitions[5], candidate.Transitions[0]
	second, _, err := NewRevision(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest {
		t.Fatalf("ordering changed digest: %s != %s", first.Digest, second.Digest)
	}
	firstJSON, err := MarshalRevision(first)
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := MarshalRevision(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstJSON, secondJSON) {
		t.Fatal("canonical encodings differ")
	}
	decoded, err := UnmarshalRevision(firstJSON)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Digest != first.Digest {
		t.Fatal("round trip changed digest")
	}
	validationJSON, err := MarshalLoopValidationResult(validation)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UnmarshalLoopValidationResult(validationJSON); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejectsBranchJoinCycleRetryMappingAndEvidenceFailures(t *testing.T) {
	tests := map[string]struct {
		mutate func(*LoopRevision)
		code   string
	}{
		"duplicate step":        {func(r *LoopRevision) { r.Steps[1].ID = r.Steps[0].ID }, "step.duplicate"},
		"unreachable":           {func(r *LoopRevision) { r.Transitions = r.Transitions[:5] }, "step.unreachable"},
		"ambiguous branch":      {func(r *LoopRevision) { r.Transitions[2].Condition = "left" }, "branch.ambiguous"},
		"join without branches": {func(r *LoopRevision) { r.Transitions = append(r.Transitions[:4], r.Transitions[5:]...) }, "join.insufficient"},
		"type mismatch":         {func(r *LoopRevision) { r.Steps[2].InputPorts[0].Type = TypeBoolean }, "mapping.type_mismatch"},
		"unbounded retry":       {func(r *LoopRevision) { r.Steps[0].Retry.MaxAttempts = 0 }, "retry.unbounded"},
		"unsatisfied output":    {func(r *LoopRevision) { r.Steps[5].Terminal.OutputMappings = nil }, "output.unsatisfied"},
		"unsatisfied evidence":  {func(r *LoopRevision) { r.Steps[0].EvidenceClaims = nil }, "evidence.unsatisfied"},
		"terminal transition": {func(r *LoopRevision) {
			r.Transitions = append(r.Transitions, Transition{ID: "bad", FromStepID: "done", ToStepID: "prepare"})
		}, "terminal.transition"},
		"unbounded cycle": {func(r *LoopRevision) {
			r.Transitions = append(r.Transitions, Transition{ID: "cycle", FromStepID: "join", ToStepID: "route", Mappings: []PortMapping{{SourcePort: "value", TargetPort: "value"}}})
		}, "cycle.unbounded"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			candidate := branchJoinCandidate()
			test.mutate(&candidate)
			candidate.SchemaVersion = RevisionSchemaVersion
			candidate.Validator = ValidatorSpec{ID: ValidatorID, Version: ValidatorVersion}
			issues := validateRevision(canonicalRevision(candidate), false)
			for _, issue := range issues {
				if issue.Code == test.code {
					return
				}
			}
			t.Fatalf("missing issue %q in %+v", test.code, issues)
		})
	}
}

func TestLoopDecoderRejectsAmbiguousMalformedAndSubstitutedValues(t *testing.T) {
	revision, _, err := NewRevision(branchJoinCandidate())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := MarshalRevision(revision)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string][]byte{
		"empty":               nil,
		"invalid utf8":        {0xff},
		"malformed":           []byte(`{"schema_version":`),
		"unknown field":       []byte(strings.Replace(string(encoded), `"loop_id":`, `"unknown":true,"loop_id":`, 1)),
		"duplicate key":       []byte(strings.Replace(string(encoded), `"loop_id":"loop.branch-join"`, `"loop_id":"loop.branch-join","loop_id":"other"`, 1)),
		"trailing":            append(append([]byte{}, encoded...), []byte(` {}`)...),
		"unsupported version": []byte(strings.Replace(string(encoded), RevisionSchemaVersion, "aegis.loop.revision.v2", 1)),
		"digest substitution": []byte(strings.Replace(string(encoded), revision.Digest, "sha256:"+strings.Repeat("a", 64), 1)),
		"incomplete":          []byte(strings.Replace(string(encoded), `"entry_step_id":"prepare",`, "", 1)),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := UnmarshalRevision(data); err == nil {
				t.Fatal("invalid value accepted")
			}
		})
	}
}

func TestPublishIsCreateOnlyAndIdempotencyRequiresStoredRequestBinding(t *testing.T) {
	first, validation, err := NewRevision(branchJoinCandidate())
	if err != nil {
		t.Fatal(err)
	}
	request := PublishRequest{Revision: first, Validation: validation, Provenance: publicationProvenance(t, first, validation), IdempotencyKey: "publish-1"}
	if decision, err := ValidatePublication(request, nil, nil); err != nil || decision.Idempotent {
		t.Fatalf("initial publication: %+v %v", decision, err)
	}
	if _, err := ValidatePublication(request, nil, &first); err == nil {
		t.Fatal("existing Loop revision bypassed stored request-key idempotency")
	}
	conflict := first
	conflict.Digest = "sha256:" + strings.Repeat("a", 64)
	if _, err := ValidatePublication(request, nil, &conflict); err == nil {
		t.Fatal("conflicting create-only publication accepted")
	}

	candidate := branchJoinCandidate()
	candidate.Revision = 2
	candidate.PreviousDigest = first.Digest
	second, secondValidation, err := NewRevision(candidate)
	if err != nil {
		t.Fatal(err)
	}
	secondRequest := PublishRequest{Revision: second, Validation: secondValidation, Provenance: publicationProvenance(t, second, secondValidation), ExpectedPreviousDigest: first.Digest, IdempotencyKey: "publish-2"}
	if _, err := ValidatePublication(secondRequest, &first, nil); err != nil {
		t.Fatal(err)
	}
	secondRequest.ExpectedPreviousDigest = "sha256:" + strings.Repeat("b", 64)
	if _, err := ValidatePublication(secondRequest, &first, nil); err == nil {
		t.Fatal("stale predecessor accepted")
	}
}

func TestLifecycleCannotReactivateRetiredLoop(t *testing.T) {
	revision, _, err := NewRevision(branchJoinCandidate())
	if err != nil {
		t.Fatal(err)
	}
	state := Lifecycle{LoopID: revision.LoopID, State: LifecycleDraft}
	state, err = Activate(state, revision)
	if err != nil || state.ActiveDigest != revision.Digest {
		t.Fatalf("activate: %+v %v", state, err)
	}
	state, err = Retire(state)
	if err != nil || state.State != LifecycleRetired {
		t.Fatalf("retire: %+v %v", state, err)
	}
	if _, err := Activate(state, revision); err == nil {
		t.Fatal("retired Loop was reactivated")
	}
	if _, err := Retire(state); err == nil {
		t.Fatal("retired Loop accepted a second retirement intent")
	}
}

func FuzzUnmarshalRevision(f *testing.F) {
	revision, _, _ := NewRevision(branchJoinCandidate())
	encoded, _ := MarshalRevision(revision)
	f.Add(encoded)
	f.Add([]byte(`{}`))
	f.Add([]byte{0xff})
	f.Fuzz(func(t *testing.T, data []byte) { _, _ = UnmarshalRevision(data) })
}
