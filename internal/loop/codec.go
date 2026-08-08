package loop

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
)

// NewRevision canonicalizes, validates, and content-addresses a candidate. The
// caller must still publish it through a create-only repository transaction.
func NewRevision(candidate LoopRevision) (LoopRevision, LoopValidationResult, error) {
	candidate.SchemaVersion = RevisionSchemaVersion
	candidate.Validator = ValidatorSpec{ID: ValidatorID, Version: ValidatorVersion}
	candidate.Digest = ""
	candidate = canonicalRevision(candidate)
	issues := validateRevision(candidate, false)
	if len(issues) != 0 {
		result := validationResult(candidate, issues)
		return LoopRevision{}, result, errors.New("Loop revision validation failed")
	}
	digest, err := digestRevision(candidate)
	if err != nil {
		return LoopRevision{}, LoopValidationResult{}, err
	}
	candidate.Digest = digest
	result := validationResult(candidate, nil)
	return candidate, result, nil
}

func MarshalRevision(revision LoopRevision) ([]byte, error) {
	if issues := validateRevision(revision, true); len(issues) != 0 {
		return nil, errors.New("cannot marshal invalid Loop revision")
	}
	return json.Marshal(canonicalRevision(revision))
}

func UnmarshalRevision(data []byte) (LoopRevision, error) {
	revision, err := decodeStrict[LoopRevision](data)
	if err != nil {
		return LoopRevision{}, err
	}
	revision = canonicalRevision(revision)
	if issues := validateRevision(revision, true); len(issues) != 0 {
		return LoopRevision{}, fmt.Errorf("invalid Loop revision: %s at %s", issues[0].Code, issues[0].Path)
	}
	return revision, nil
}

func MarshalLoopValidationResult(result LoopValidationResult) ([]byte, error) {
	canonical := canonicalLoopValidationResult(result)
	if err := validateLoopValidationResult(canonical); err != nil {
		return nil, err
	}
	return json.Marshal(canonical)
}

func UnmarshalLoopValidationResult(data []byte) (LoopValidationResult, error) {
	result, err := decodeStrict[LoopValidationResult](data)
	if err != nil {
		return LoopValidationResult{}, err
	}
	result = canonicalLoopValidationResult(result)
	if err := validateLoopValidationResult(result); err != nil {
		return LoopValidationResult{}, err
	}
	return result, nil
}

func digestRevision(revision LoopRevision) (string, error) {
	revision.Digest = ""
	encoded, err := json.Marshal(canonicalRevision(revision))
	if err != nil {
		return "", err
	}
	return sha256Digest(encoded), nil
}

func digestLoopValidationResult(result LoopValidationResult) (string, error) {
	result.Digest = ""
	encoded, err := json.Marshal(canonicalLoopValidationResult(result))
	if err != nil {
		return "", err
	}
	return sha256Digest(encoded), nil
}

func validationResult(revision LoopRevision, issues []ValidationIssue) LoopValidationResult {
	result := LoopValidationResult{
		SchemaVersion: ValidationSchemaVersion, LoopID: revision.LoopID,
		Revision: revision.Revision, RevisionDigest: revision.Digest,
		Validator: ValidatorSpec{ID: ValidatorID, Version: ValidatorVersion},
		Outcome:   ValidationValid, Issues: append([]ValidationIssue(nil), issues...),
	}
	if len(issues) != 0 {
		result.Outcome = ValidationInvalid
	}
	result = canonicalLoopValidationResult(result)
	result.Digest, _ = digestLoopValidationResult(result)
	return result
}

func validateLoopValidationResult(result LoopValidationResult) error {
	if result.SchemaVersion != ValidationSchemaVersion || !validID(result.LoopID) || result.Revision == 0 ||
		!validDigest(result.RevisionDigest) || result.Validator.ID != ValidatorID || result.Validator.Version != ValidatorVersion ||
		(result.Outcome != ValidationValid && result.Outcome != ValidationInvalid) || !validDigest(result.Digest) {
		return errors.New("invalid Loop validation result")
	}
	if (result.Outcome == ValidationValid) != (len(result.Issues) == 0) {
		return errors.New("Loop validation outcome does not match issues")
	}
	for _, issue := range result.Issues {
		if !validID(issue.Code) || issue.Path == "" || issue.Message == "" || len(issue.Path) > 1024 || len(issue.Message) > 1024 {
			return errors.New("invalid Loop validation issue")
		}
	}
	digest, err := digestLoopValidationResult(result)
	if err != nil || digest != result.Digest {
		return errors.New("Loop validation digest mismatch")
	}
	return nil
}

func canonicalRevision(value LoopRevision) LoopRevision {
	value.Inputs = canonicalPorts(value.Inputs)
	value.Outputs = canonicalPorts(value.Outputs)
	value.Steps = append([]Step(nil), value.Steps...)
	for index := range value.Steps {
		value.Steps[index].InputPorts = canonicalPorts(value.Steps[index].InputPorts)
		value.Steps[index].OutputPorts = canonicalPorts(value.Steps[index].OutputPorts)
		value.Steps[index].EvidenceClaims = append([]EvidenceClaim(nil), value.Steps[index].EvidenceClaims...)
		sort.Slice(value.Steps[index].EvidenceClaims, func(i, j int) bool {
			return value.Steps[index].EvidenceClaims[i].Claim < value.Steps[index].EvidenceClaims[j].Claim
		})
		if value.Steps[index].EvidenceClaims == nil {
			value.Steps[index].EvidenceClaims = []EvidenceClaim{}
		}
		if terminal := value.Steps[index].Terminal; terminal != nil {
			copyTerminal := *terminal
			copyTerminal.OutputMappings = canonicalMappings(copyTerminal.OutputMappings)
			value.Steps[index].Terminal = &copyTerminal
		}
		if gate := value.Steps[index].Gate; gate != nil {
			copyGate := *gate
			value.Steps[index].Gate = &copyGate
		}
	}
	sort.Slice(value.Steps, func(i, j int) bool { return value.Steps[i].ID < value.Steps[j].ID })
	if value.Steps == nil {
		value.Steps = []Step{}
	}
	value.Transitions = append([]Transition(nil), value.Transitions...)
	for index := range value.Transitions {
		value.Transitions[index].Mappings = canonicalMappings(value.Transitions[index].Mappings)
	}
	sort.Slice(value.Transitions, func(i, j int) bool { return value.Transitions[i].ID < value.Transitions[j].ID })
	if value.Transitions == nil {
		value.Transitions = []Transition{}
	}
	value.RequiredEvidence = append([]EvidenceRequirement(nil), value.RequiredEvidence...)
	sort.Slice(value.RequiredEvidence, func(i, j int) bool { return value.RequiredEvidence[i].Claim < value.RequiredEvidence[j].Claim })
	if value.RequiredEvidence == nil {
		value.RequiredEvidence = []EvidenceRequirement{}
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
func canonicalMappings(values []PortMapping) []PortMapping {
	result := append([]PortMapping(nil), values...)
	sort.Slice(result, func(i, j int) bool {
		if result[i].TargetPort != result[j].TargetPort {
			return result[i].TargetPort < result[j].TargetPort
		}
		return result[i].SourcePort < result[j].SourcePort
	})
	if result == nil {
		result = []PortMapping{}
	}
	return result
}
func canonicalLoopValidationResult(value LoopValidationResult) LoopValidationResult {
	value.Issues = append([]ValidationIssue(nil), value.Issues...)
	sort.Slice(value.Issues, func(i, j int) bool {
		if value.Issues[i].Path != value.Issues[j].Path {
			return value.Issues[i].Path < value.Issues[j].Path
		}
		if value.Issues[i].Code != value.Issues[j].Code {
			return value.Issues[i].Code < value.Issues[j].Code
		}
		return value.Issues[i].Message < value.Issues[j].Message
	})
	if value.Issues == nil {
		value.Issues = []ValidationIssue{}
	}
	return value
}
func sha256Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func decodeStrict[T any](data []byte) (T, error) {
	var value T
	if len(data) == 0 || !utf8.Valid(data) {
		return value, errors.New("Loop wire value is empty or invalid UTF-8")
	}
	if err := rejectDuplicateKeys(data); err != nil {
		return value, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, fmt.Errorf("decode Loop value: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return value, errors.New("Loop wire value contains trailing JSON value")
		}
		return value, fmt.Errorf("Loop wire value contains trailing data: %w", err)
	}
	return value, nil
}

func rejectDuplicateKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	first, err := decoder.Token()
	if err != nil {
		return err
	}
	if err := inspectJSONValue(decoder, first); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("Loop wire value contains trailing data")
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
		keys := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("Loop object key is not a string")
			}
			if _, duplicate := keys[key]; duplicate {
				return fmt.Errorf("duplicate Loop object key %q", key)
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
			return errors.New("Loop object is not closed")
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
			return errors.New("Loop array is not closed")
		}
	default:
		return errors.New("unexpected closing delimiter in Loop wire value")
	}
	return nil
}
