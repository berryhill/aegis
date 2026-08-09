package graph

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/reference"
)

func exactRevision(id string, revision uint64, marker string) reference.RevisionRef {
	return reference.RevisionRef{
		SchemaVersion: reference.RevisionRefSchemaVersion,
		ID:            id,
		Revision:      revision,
		Digest:        "sha256:" + strings.Repeat(marker, 64),
	}
}

func graphCandidate() GraphRevision {
	value := Port{ID: "value", Type: TypeString, Required: true}
	return GraphRevision{
		GraphID:  "graph.review",
		Revision: 1,
		Inputs:   []Port{value},
		Outputs:  []Port{{ID: "result", Type: TypeArtifact, Required: true}},
		Nodes: []Node{
			{
				ID:          "review",
				Participant: exactRevision("agent.reviewer", 3, "a"),
				Loop:        exactRevision("loop.review", 5, "b"),
				Inputs:      []Port{value},
				Outputs:     []Port{{ID: "draft", Type: TypeString, Required: true}},
			},
			{
				ID:          "publish",
				Participant: exactRevision("agent.publisher", 2, "c"),
				Loop:        exactRevision("loop.publish", 7, "d"),
				Inputs:      []Port{{ID: "draft", Type: TypeString, Required: true}},
				Outputs:     []Port{{ID: "artifact", Type: TypeArtifact, Required: true}},
			},
		},
		InputMappings: []InputMapping{{GraphInput: "value", ToNodeID: "review", ToPort: "value"}},
		Dependencies: []Dependency{{
			ID: "review-before-publish", FromNodeID: "review", ToNodeID: "publish",
			Mappings: []PortMapping{{FromPort: "draft", ToPort: "draft"}},
		}},
		OutputMappings: []OutputMapping{{FromNodeID: "publish", FromPort: "artifact", GraphOutput: "result"}},
		AdmissionRules: []AdmissionRule{{
			ID: "operator-only",
			PolicyRef: reference.DigestRef{
				SchemaVersion: reference.DigestRefSchemaVersion,
				ID:            "policy.operator",
				Digest:        "sha256:" + strings.Repeat("e", 64),
			},
		}},
	}
}

func TestGraphRevisionCanonicalRoundTripAndExactBindings(t *testing.T) {
	first, validation, err := NewRevision(graphCandidate())
	if err != nil {
		t.Fatal(err)
	}
	if validation.Outcome != ValidationValid || validation.RevisionDigest != first.Digest || !validDigest(validation.Digest) {
		t.Fatalf("unexpected validation result: %+v", validation)
	}

	reordered := graphCandidate()
	reordered.Nodes[0], reordered.Nodes[1] = reordered.Nodes[1], reordered.Nodes[0]
	second, _, err := NewRevision(reordered)
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
		t.Fatal("canonical Graph encodings differ")
	}
	decoded, err := UnmarshalRevision(firstJSON)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Digest != first.Digest || decoded.Nodes[0].Participant.Revision == 0 || decoded.Nodes[0].Loop.Revision == 0 {
		t.Fatal("round trip did not preserve exact revision bindings")
	}

	validationJSON, err := MarshalValidationResult(validation)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UnmarshalValidationResult(validationJSON); err != nil {
		t.Fatal(err)
	}
}

func TestGraphValidationRejectsUnsafeTopologyAndMutableBindings(t *testing.T) {
	tests := map[string]struct {
		mutate func(*GraphRevision)
		code   string
	}{
		"mutable participant": {func(r *GraphRevision) { r.Nodes[0].Participant.Revision = 0 }, "node.participant_invalid"},
		"mutable Loop":        {func(r *GraphRevision) { r.Nodes[0].Loop.Digest = "" }, "node.loop_invalid"},
		"duplicate node":      {func(r *GraphRevision) { r.Nodes[1].ID = r.Nodes[0].ID }, "node.duplicate"},
		"cycle": {func(r *GraphRevision) {
			r.Dependencies = append(r.Dependencies, Dependency{ID: "cycle", FromNodeID: "publish", ToNodeID: "review", Mappings: []PortMapping{{FromPort: "artifact", ToPort: "value"}}})
		}, "dependency.cycle"},
		"missing node input": {func(r *GraphRevision) { r.InputMappings = nil }, "mapping.required_input_missing"},
		"duplicate target": {func(r *GraphRevision) {
			r.InputMappings = append(r.InputMappings, InputMapping{GraphInput: "value", ToNodeID: "publish", ToPort: "draft"})
		}, "mapping.target_duplicate"},
		"mapping type mismatch": {func(r *GraphRevision) { r.Nodes[1].Inputs[0].Type = TypeBoolean }, "mapping.type_mismatch"},
		"unsatisfied output":    {func(r *GraphRevision) { r.OutputMappings = nil }, "mapping.graph_output_unsatisfied"},
		"mutable policy":        {func(r *GraphRevision) { r.AdmissionRules[0].PolicyRef.Digest = "" }, "admission_rule.policy_invalid"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			candidate := graphCandidate()
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

func TestGraphCodecsRejectAmbiguousMalformedAndSubstitutedValues(t *testing.T) {
	revision, validation, err := NewRevision(graphCandidate())
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
		"unknown field":       []byte(strings.Replace(string(encoded), `"graph_id":`, `"unknown":true,"graph_id":`, 1)),
		"duplicate key":       []byte(strings.Replace(string(encoded), `"graph_id":"graph.review"`, `"graph_id":"graph.review","graph_id":"other"`, 1)),
		"trailing":            append(append([]byte{}, encoded...), []byte(` {}`)...),
		"unsupported version": []byte(strings.Replace(string(encoded), RevisionSchemaVersion, "aegis.graph.revision.v2", 1)),
		"digest substitution": []byte(strings.Replace(string(encoded), revision.Digest, "sha256:"+strings.Repeat("f", 64), 1)),
		"mutable reference":   []byte(strings.Replace(string(encoded), `"revision":3`, `"revision":0`, 1)),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := UnmarshalRevision(data); err == nil {
				t.Fatal("invalid Graph revision accepted")
			}
		})
	}

	validationJSON, err := MarshalValidationResult(validation)
	if err != nil {
		t.Fatal(err)
	}
	tamperedValidation := []byte(strings.Replace(string(validationJSON), validation.RevisionDigest, "sha256:"+strings.Repeat("f", 64), 1))
	if _, err := UnmarshalValidationResult(tamperedValidation); err == nil {
		t.Fatal("validation digest substitution accepted")
	}
}

func TestGraphPublicationIsCreateOnlyExactAndLifecycleIsMonotonic(t *testing.T) {
	first, validation, err := NewRevision(graphCandidate())
	if err != nil {
		t.Fatal(err)
	}
	request := PublishRequest{Revision: first, Validation: validation, IdempotencyKey: "publish-1"}
	if decision, err := ValidatePublication(request, nil, nil); err != nil || decision.Idempotent {
		t.Fatalf("initial publication: %+v %v", decision, err)
	}
	if decision, err := ValidatePublication(request, nil, &first); err != nil || !decision.Idempotent {
		t.Fatalf("idempotent publication: %+v %v", decision, err)
	}

	wrongValidation := validation
	wrongValidation.RevisionDigest = "sha256:" + strings.Repeat("f", 64)
	wrongValidation.Digest, _ = digestValidationResult(wrongValidation)
	request.Validation = wrongValidation
	if _, err := ValidatePublication(request, nil, nil); err == nil {
		t.Fatal("validation for another exact digest accepted")
	}

	candidate := graphCandidate()
	candidate.Revision = 2
	candidate.PreviousDigest = first.Digest
	second, secondValidation, err := NewRevision(candidate)
	if err != nil {
		t.Fatal(err)
	}
	secondRequest := PublishRequest{Revision: second, Validation: secondValidation, ExpectedPreviousDigest: first.Digest, IdempotencyKey: "publish-2"}
	if _, err := ValidatePublication(secondRequest, &first, nil); err != nil {
		t.Fatal(err)
	}
	secondRequest.ExpectedPreviousDigest = "sha256:" + strings.Repeat("f", 64)
	if _, err := ValidatePublication(secondRequest, &first, nil); err == nil {
		t.Fatal("stale predecessor digest accepted")
	}

	state, err := Activate(Lifecycle{GraphID: first.GraphID, State: LifecycleDraft}, first)
	if err != nil || state.ActiveDigest != first.Digest {
		t.Fatalf("activate: %+v %v", state, err)
	}
	state, err = Retire(state)
	if err != nil || state.State != LifecycleRetired {
		t.Fatalf("retire: %+v %v", state, err)
	}
	if _, err := Activate(state, first); err == nil {
		t.Fatal("retired Graph was reactivated")
	}
}

func TestGraphRunSnapshotPinsDefinitionsAndNormalizesInputs(t *testing.T) {
	revision, _, err := NewRevision(graphCandidate())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := NewRunSnapshot("snapshot-1", revision, []NormalizedInput{{PortID: "value", Type: TypeString, Value: json.RawMessage(` "hello" `)}})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Graph.Digest != revision.Digest || len(snapshot.Participants) != 2 || len(snapshot.Loops) != 2 || string(snapshot.Inputs[0].Value) != `"hello"` {
		t.Fatalf("snapshot omitted exact resolved truth: %+v", snapshot)
	}
	encoded, err := MarshalRunSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := UnmarshalRunSnapshot(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Digest != snapshot.Digest {
		t.Fatal("snapshot round trip changed digest")
	}

	tampered := []byte(strings.Replace(string(encoded), snapshot.Graph.Digest, "sha256:"+strings.Repeat("f", 64), 1))
	if _, err := UnmarshalRunSnapshot(tampered); err == nil {
		t.Fatal("snapshot Graph digest substitution accepted")
	}

	denials := map[string][]NormalizedInput{
		"missing required": nil,
		"unknown input":    {{PortID: "other", Type: TypeString, Value: json.RawMessage(`"hello"`)}},
		"wrong type":       {{PortID: "value", Type: TypeBoolean, Value: json.RawMessage(`true`)}},
		"value mismatch":   {{PortID: "value", Type: TypeString, Value: json.RawMessage(`true`)}},
		"duplicate keys":   {{PortID: "value", Type: TypeString, Value: json.RawMessage(`{"x":1,"x":2}`)}},
		"trailing JSON":    {{PortID: "value", Type: TypeString, Value: json.RawMessage(`"hello" {}`)}},
	}
	for name, inputs := range denials {
		t.Run(name, func(t *testing.T) {
			if _, err := NewRunSnapshot("snapshot-denied", revision, inputs); err == nil {
				t.Fatal("invalid snapshot input accepted")
			}
		})
	}
	if _, err := NewRunSnapshot("bad snapshot id", revision, []NormalizedInput{{PortID: "value", Type: TypeString, Value: json.RawMessage(`"hello"`)}}); err == nil {
		t.Fatal("malformed snapshot identity accepted")
	}
}

func FuzzUnmarshalRevision(f *testing.F) {
	revision, _, _ := NewRevision(graphCandidate())
	encoded, _ := MarshalRevision(revision)
	f.Add(encoded)
	f.Add([]byte(`{}`))
	f.Add([]byte{0xff})
	f.Fuzz(func(t *testing.T, data []byte) { _, _ = UnmarshalRevision(data) })
}
