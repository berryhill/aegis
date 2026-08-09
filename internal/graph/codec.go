package graph

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"unicode/utf8"

	"github.com/berryhill/aegis/internal/reference"
)

// NewRevision canonicalizes, validates, and content-addresses a candidate. A
// repository must still enforce create-only publication.
func NewRevision(candidate GraphRevision) (GraphRevision, GraphValidationResult, error) {
	candidate.SchemaVersion = RevisionSchemaVersion
	candidate.Validator = ValidatorSpec{ID: ValidatorID, Version: ValidatorVersion}
	candidate.Digest = ""
	candidate = canonicalRevision(candidate)
	issues := validateRevision(candidate, false)
	if len(issues) != 0 {
		return GraphRevision{}, makeValidationResult(candidate, issues), errors.New("Graph revision validation failed")
	}
	digest, err := digestRevision(candidate)
	if err != nil {
		return GraphRevision{}, GraphValidationResult{}, err
	}
	candidate.Digest = digest
	return candidate, makeValidationResult(candidate, nil), nil
}

func MarshalRevision(revision GraphRevision) ([]byte, error) {
	canonical := canonicalRevision(revision)
	if issues := validateRevision(canonical, true); len(issues) != 0 {
		return nil, errors.New("cannot marshal invalid Graph revision")
	}
	return json.Marshal(canonical)
}

func UnmarshalRevision(data []byte) (GraphRevision, error) {
	revision, err := decodeStrict[GraphRevision](data)
	if err != nil {
		return GraphRevision{}, err
	}
	revision = canonicalRevision(revision)
	if issues := validateRevision(revision, true); len(issues) != 0 {
		return GraphRevision{}, fmt.Errorf("invalid Graph revision: %s at %s", issues[0].Code, issues[0].Path)
	}
	return revision, nil
}

func MarshalValidationResult(result GraphValidationResult) ([]byte, error) {
	canonical := canonicalValidationResult(result)
	if err := validateValidationResult(canonical); err != nil {
		return nil, err
	}
	return json.Marshal(canonical)
}

func UnmarshalValidationResult(data []byte) (GraphValidationResult, error) {
	result, err := decodeStrict[GraphValidationResult](data)
	if err != nil {
		return GraphValidationResult{}, err
	}
	result = canonicalValidationResult(result)
	if err := validateValidationResult(result); err != nil {
		return GraphValidationResult{}, err
	}
	return result, nil
}

// NewRunSnapshot derives all exact participant and Loop bindings from the
// sealed Graph revision and normalizes the caller's typed inputs.
func NewRunSnapshot(snapshotID string, revision GraphRevision, inputs []NormalizedInput) (GraphRunSnapshot, error) {
	if issues := validateRevision(revision, true); len(issues) != 0 {
		return GraphRunSnapshot{}, errors.New("cannot snapshot invalid Graph revision")
	}
	normalized := append([]NormalizedInput(nil), inputs...)
	for index := range normalized {
		value, err := canonicalJSON(normalized[index].Value)
		if err != nil {
			return GraphRunSnapshot{}, fmt.Errorf("normalize input %q: %w", normalized[index].PortID, err)
		}
		normalized[index].Value = value
	}
	participants := make([]reference.RevisionRef, 0, len(revision.Nodes))
	loops := make([]reference.RevisionRef, 0, len(revision.Nodes))
	seenParticipants := make(map[string]struct{}, len(revision.Nodes))
	seenLoops := make(map[string]struct{}, len(revision.Nodes))
	for _, node := range revision.Nodes {
		participantKey := revisionRefKey(node.Participant)
		if _, exists := seenParticipants[participantKey]; !exists {
			participants = append(participants, node.Participant)
			seenParticipants[participantKey] = struct{}{}
		}
		loopKey := revisionRefKey(node.Loop)
		if _, exists := seenLoops[loopKey]; !exists {
			loops = append(loops, node.Loop)
			seenLoops[loopKey] = struct{}{}
		}
	}
	snapshot := GraphRunSnapshot{
		SchemaVersion: SnapshotSchemaVersion,
		SnapshotID:    snapshotID,
		Graph: reference.RevisionRef{
			SchemaVersion: reference.RevisionRefSchemaVersion,
			ID:            revision.GraphID,
			Revision:      revision.Revision,
			Digest:        revision.Digest,
		},
		Inputs:       normalized,
		Participants: participants,
		Loops:        loops,
	}
	snapshot = canonicalSnapshot(snapshot)
	if err := validateSnapshotAgainstRevision(snapshot, revision); err != nil {
		return GraphRunSnapshot{}, err
	}
	digest, err := digestSnapshot(snapshot)
	if err != nil {
		return GraphRunSnapshot{}, err
	}
	snapshot.Digest = digest
	if err := validateSnapshot(snapshot); err != nil {
		return GraphRunSnapshot{}, err
	}
	return snapshot, nil
}

func MarshalRunSnapshot(snapshot GraphRunSnapshot) ([]byte, error) {
	canonical, err := canonicalizeSnapshotValues(snapshot)
	if err != nil {
		return nil, err
	}
	if err := validateSnapshot(canonical); err != nil {
		return nil, err
	}
	return json.Marshal(canonical)
}

func UnmarshalRunSnapshot(data []byte) (GraphRunSnapshot, error) {
	snapshot, err := decodeStrict[GraphRunSnapshot](data)
	if err != nil {
		return GraphRunSnapshot{}, err
	}
	snapshot, err = canonicalizeSnapshotValues(snapshot)
	if err != nil {
		return GraphRunSnapshot{}, err
	}
	if err := validateSnapshot(snapshot); err != nil {
		return GraphRunSnapshot{}, err
	}
	return snapshot, nil
}

func validateSnapshotAgainstRevision(snapshot GraphRunSnapshot, revision GraphRevision) error {
	if snapshot.Graph.ID != revision.GraphID || snapshot.Graph.Revision != revision.Revision || snapshot.Graph.Digest != revision.Digest {
		return errors.New("snapshot Graph reference does not match exact revision")
	}
	declared := make(map[string]Port, len(revision.Inputs))
	for _, port := range revision.Inputs {
		declared[port.ID] = port
	}
	seen := make(map[string]struct{}, len(snapshot.Inputs))
	for _, input := range snapshot.Inputs {
		port, ok := declared[input.PortID]
		if !ok || port.Type != input.Type {
			return fmt.Errorf("snapshot input %q does not match Graph input contract", input.PortID)
		}
		if _, duplicate := seen[input.PortID]; duplicate {
			return fmt.Errorf("snapshot input %q is duplicated", input.PortID)
		}
		seen[input.PortID] = struct{}{}
	}
	for _, port := range revision.Inputs {
		if port.Required {
			if _, ok := seen[port.ID]; !ok {
				return fmt.Errorf("required snapshot input %q is missing", port.ID)
			}
		}
	}
	return nil
}

func validateSnapshot(snapshot GraphRunSnapshot) error {
	if snapshot.SchemaVersion != SnapshotSchemaVersion || !validID(snapshot.SnapshotID) {
		return errors.New("invalid Graph run snapshot identity")
	}
	if err := snapshot.Graph.Validate(); err != nil {
		return fmt.Errorf("validate Graph reference: %w", err)
	}
	if len(snapshot.Inputs) > MaxNormalizedInputs || len(snapshot.Participants) == 0 || len(snapshot.Participants) > MaxResolvedReferences || len(snapshot.Loops) == 0 || len(snapshot.Loops) > MaxResolvedReferences {
		return errors.New("Graph run snapshot exceeds bounded limits or omits resolved references")
	}
	for index, input := range snapshot.Inputs {
		if index > 0 && snapshot.Inputs[index-1].PortID >= input.PortID {
			return errors.New("snapshot inputs must be sorted and unique")
		}
		if err := validateInputValue(input); err != nil {
			return fmt.Errorf("validate snapshot input %q: %w", input.PortID, err)
		}
	}
	if err := validateRevisionRefs(snapshot.Participants, "participant"); err != nil {
		return err
	}
	if err := validateRevisionRefs(snapshot.Loops, "Loop"); err != nil {
		return err
	}
	if !validDigest(snapshot.Digest) {
		return errors.New("invalid Graph run snapshot digest")
	}
	digest, err := digestSnapshot(snapshot)
	if err != nil || digest != snapshot.Digest {
		return errors.New("Graph run snapshot digest mismatch")
	}
	return nil
}

func validateRevisionRefs(refs []reference.RevisionRef, name string) error {
	for index, ref := range refs {
		if err := ref.Validate(); err != nil {
			return fmt.Errorf("validate %s reference: %w", name, err)
		}
		if index > 0 && !revisionRefLess(refs[index-1], ref) {
			return fmt.Errorf("%s references must be sorted and unique", name)
		}
	}
	return nil
}

func makeValidationResult(revision GraphRevision, issues []ValidationIssue) GraphValidationResult {
	result := GraphValidationResult{
		SchemaVersion:  ValidationSchemaVersion,
		GraphID:        revision.GraphID,
		Revision:       revision.Revision,
		RevisionDigest: revision.Digest,
		Validator:      ValidatorSpec{ID: ValidatorID, Version: ValidatorVersion},
		Outcome:        ValidationValid,
		Issues:         append([]ValidationIssue(nil), issues...),
	}
	if len(issues) != 0 {
		result.Outcome = ValidationInvalid
	}
	result = canonicalValidationResult(result)
	result.Digest, _ = digestValidationResult(result)
	return result
}

func validateValidationResult(result GraphValidationResult) error {
	if result.SchemaVersion != ValidationSchemaVersion || !validID(result.GraphID) || result.Revision == 0 || !validDigest(result.RevisionDigest) || result.Validator.ID != ValidatorID || result.Validator.Version != ValidatorVersion || !validDigest(result.Digest) {
		return errors.New("invalid Graph validation result")
	}
	if (result.Outcome == ValidationValid) != (len(result.Issues) == 0) {
		return errors.New("Graph validation outcome does not match issues")
	}
	for _, issue := range result.Issues {
		if !validID(issue.Code) || issue.Path == "" || issue.Message == "" || len(issue.Path) > 1024 || len(issue.Message) > 1024 {
			return errors.New("invalid Graph validation issue")
		}
	}
	digest, err := digestValidationResult(result)
	if err != nil || digest != result.Digest {
		return errors.New("Graph validation digest mismatch")
	}
	return nil
}

func digestRevision(revision GraphRevision) (string, error) {
	revision.Digest = ""
	encoded, err := json.Marshal(canonicalRevision(revision))
	if err != nil {
		return "", err
	}
	return sha256Digest(encoded), nil
}

func digestValidationResult(result GraphValidationResult) (string, error) {
	result.Digest = ""
	encoded, err := json.Marshal(canonicalValidationResult(result))
	if err != nil {
		return "", err
	}
	return sha256Digest(encoded), nil
}

func digestSnapshot(snapshot GraphRunSnapshot) (string, error) {
	snapshot.Digest = ""
	canonical, err := canonicalizeSnapshotValues(snapshot)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	return sha256Digest(encoded), nil
}

func canonicalRevision(value GraphRevision) GraphRevision {
	value.Inputs = canonicalPorts(value.Inputs)
	value.Outputs = canonicalPorts(value.Outputs)
	value.Nodes = append([]Node(nil), value.Nodes...)
	for index := range value.Nodes {
		value.Nodes[index].Inputs = canonicalPorts(value.Nodes[index].Inputs)
		value.Nodes[index].Outputs = canonicalPorts(value.Nodes[index].Outputs)
	}
	sort.Slice(value.Nodes, func(i, j int) bool { return value.Nodes[i].ID < value.Nodes[j].ID })
	if value.Nodes == nil {
		value.Nodes = []Node{}
	}
	value.InputMappings = append([]InputMapping(nil), value.InputMappings...)
	sort.Slice(value.InputMappings, func(i, j int) bool {
		if value.InputMappings[i].ToNodeID != value.InputMappings[j].ToNodeID {
			return value.InputMappings[i].ToNodeID < value.InputMappings[j].ToNodeID
		}
		if value.InputMappings[i].ToPort != value.InputMappings[j].ToPort {
			return value.InputMappings[i].ToPort < value.InputMappings[j].ToPort
		}
		return value.InputMappings[i].GraphInput < value.InputMappings[j].GraphInput
	})
	if value.InputMappings == nil {
		value.InputMappings = []InputMapping{}
	}
	value.Dependencies = append([]Dependency(nil), value.Dependencies...)
	for index := range value.Dependencies {
		value.Dependencies[index].Mappings = append([]PortMapping(nil), value.Dependencies[index].Mappings...)
		sort.Slice(value.Dependencies[index].Mappings, func(i, j int) bool {
			if value.Dependencies[index].Mappings[i].ToPort != value.Dependencies[index].Mappings[j].ToPort {
				return value.Dependencies[index].Mappings[i].ToPort < value.Dependencies[index].Mappings[j].ToPort
			}
			return value.Dependencies[index].Mappings[i].FromPort < value.Dependencies[index].Mappings[j].FromPort
		})
		if value.Dependencies[index].Mappings == nil {
			value.Dependencies[index].Mappings = []PortMapping{}
		}
	}
	sort.Slice(value.Dependencies, func(i, j int) bool { return value.Dependencies[i].ID < value.Dependencies[j].ID })
	if value.Dependencies == nil {
		value.Dependencies = []Dependency{}
	}
	value.OutputMappings = append([]OutputMapping(nil), value.OutputMappings...)
	sort.Slice(value.OutputMappings, func(i, j int) bool {
		if value.OutputMappings[i].GraphOutput != value.OutputMappings[j].GraphOutput {
			return value.OutputMappings[i].GraphOutput < value.OutputMappings[j].GraphOutput
		}
		if value.OutputMappings[i].FromNodeID != value.OutputMappings[j].FromNodeID {
			return value.OutputMappings[i].FromNodeID < value.OutputMappings[j].FromNodeID
		}
		return value.OutputMappings[i].FromPort < value.OutputMappings[j].FromPort
	})
	if value.OutputMappings == nil {
		value.OutputMappings = []OutputMapping{}
	}
	value.AdmissionRules = append([]AdmissionRule(nil), value.AdmissionRules...)
	sort.Slice(value.AdmissionRules, func(i, j int) bool { return value.AdmissionRules[i].ID < value.AdmissionRules[j].ID })
	if value.AdmissionRules == nil {
		value.AdmissionRules = []AdmissionRule{}
	}
	return value
}

func canonicalPorts(values []Port) []Port {
	result := append([]Port(nil), values...)
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	if result == nil {
		result = []Port{}
	}
	return result
}

func canonicalValidationResult(value GraphValidationResult) GraphValidationResult {
	value.Issues = append([]ValidationIssue(nil), value.Issues...)
	sortIssues(value.Issues)
	if value.Issues == nil {
		value.Issues = []ValidationIssue{}
	}
	return value
}

func canonicalSnapshot(value GraphRunSnapshot) GraphRunSnapshot {
	value.Inputs = append([]NormalizedInput(nil), value.Inputs...)
	for index := range value.Inputs {
		value.Inputs[index].Value = append(json.RawMessage(nil), value.Inputs[index].Value...)
	}
	sort.Slice(value.Inputs, func(i, j int) bool { return value.Inputs[i].PortID < value.Inputs[j].PortID })
	if value.Inputs == nil {
		value.Inputs = []NormalizedInput{}
	}
	value.Participants = canonicalRevisionRefs(value.Participants)
	value.Loops = canonicalRevisionRefs(value.Loops)
	return value
}

func canonicalizeSnapshotValues(value GraphRunSnapshot) (GraphRunSnapshot, error) {
	value = canonicalSnapshot(value)
	for index := range value.Inputs {
		canonical, err := canonicalJSON(value.Inputs[index].Value)
		if err != nil {
			return GraphRunSnapshot{}, fmt.Errorf("canonicalize snapshot input %q: %w", value.Inputs[index].PortID, err)
		}
		value.Inputs[index].Value = canonical
	}
	return value, nil
}

func canonicalRevisionRefs(values []reference.RevisionRef) []reference.RevisionRef {
	result := append([]reference.RevisionRef(nil), values...)
	sort.Slice(result, func(i, j int) bool { return revisionRefLess(result[i], result[j]) })
	if result == nil {
		result = []reference.RevisionRef{}
	}
	return result
}

func revisionRefLess(left, right reference.RevisionRef) bool {
	if left.ID != right.ID {
		return left.ID < right.ID
	}
	if left.Revision != right.Revision {
		return left.Revision < right.Revision
	}
	return left.Digest < right.Digest
}

func revisionRefKey(ref reference.RevisionRef) string {
	return ref.ID + "\x00" + fmt.Sprint(ref.Revision) + "\x00" + ref.Digest
}

func canonicalJSON(data []byte) ([]byte, error) {
	if len(data) == 0 || len(data) > MaxInputValueBytes || !utf8.Valid(data) {
		return nil, errors.New("JSON value is empty, oversized, or invalid UTF-8")
	}
	if err := rejectDuplicateKeys(data); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := requireEOF(decoder); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func sha256Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func decodeStrict[T any](data []byte) (T, error) {
	var value T
	if len(data) == 0 || !utf8.Valid(data) {
		return value, errors.New("Graph wire value is empty or invalid UTF-8")
	}
	if err := rejectDuplicateKeys(data); err != nil {
		return value, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, fmt.Errorf("decode Graph value: %w", err)
	}
	if err := requireEOF(decoder); err != nil {
		return value, err
	}
	return value, nil
}

func requireEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("Graph wire value contains trailing JSON value")
		}
		return fmt.Errorf("Graph wire value contains trailing data: %w", err)
	}
	return nil
}

func rejectDuplicateKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	first, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("decode Graph tokens: %w", err)
	}
	if err := inspectJSONValue(decoder, first); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("Graph wire value contains trailing JSON value")
		}
		return fmt.Errorf("Graph wire value contains trailing data: %w", err)
	}
	return nil
}

func inspectJSONValue(decoder *json.Decoder, token json.Token) error {
	delim, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	switch delim {
	case '{':
		keys := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("Graph object key is not a string")
			}
			if _, duplicate := keys[key]; duplicate {
				return fmt.Errorf("duplicate Graph object key %q", key)
			}
			keys[key] = struct{}{}
			item, err := decoder.Token()
			if err != nil {
				return err
			}
			if err := inspectJSONValue(decoder, item); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return errors.New("Graph object is not closed")
		}
	case '[':
		for decoder.More() {
			item, err := decoder.Token()
			if err != nil {
				return err
			}
			if err := inspectJSONValue(decoder, item); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return errors.New("Graph array is not closed")
		}
	default:
		return errors.New("unexpected closing delimiter in Graph wire value")
	}
	return nil
}
