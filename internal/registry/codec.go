package registry

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/berryhill/aegis/internal/reference"
)

type revisionDigestInput struct {
	SchemaVersion          string                `json:"schema_version"`
	AgentID                string                `json:"agent_id"`
	Revision               uint64                `json:"revision"`
	Source                 FleetSource           `json:"source"`
	Runtime                RuntimeBinding        `json:"runtime"`
	Ownership              Ownership             `json:"ownership"`
	Lifecycle              Lifecycle             `json:"lifecycle"`
	Charter                reference.RevisionRef `json:"charter"`
	CapabilityDeclarations []string              `json:"capability_declarations"`
	PolicyRefs             []reference.DigestRef `json:"policy_refs"`
}

// SealRevision validates a revision and returns it with its canonical digest.
// Any caller-supplied digest is ignored so it cannot authorize substitution.
func SealRevision(revision AgentRevision) (AgentRevision, error) {
	revision.Digest = ""
	if err := revision.validateContent(); err != nil {
		return AgentRevision{}, err
	}
	digest, err := revisionDigest(revision)
	if err != nil {
		return AgentRevision{}, err
	}
	revision.Digest = digest
	return revision, nil
}

func revisionDigest(revision AgentRevision) (string, error) {
	wire, err := json.Marshal(revisionDigestInput{
		SchemaVersion:          revision.SchemaVersion,
		AgentID:                revision.AgentID,
		Revision:               revision.Revision,
		Source:                 revision.Source,
		Runtime:                revision.Runtime,
		Ownership:              revision.Ownership,
		Lifecycle:              revision.Lifecycle,
		Charter:                revision.Charter,
		CapabilityDeclarations: revision.CapabilityDeclarations,
		PolicyRefs:             revision.PolicyRefs,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(wire)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func validateSealedRevision(revision AgentRevision) error {
	if err := revision.validateContent(); err != nil {
		return err
	}
	digest, err := revisionDigest(revision)
	if err != nil {
		return err
	}
	if revision.Digest != digest {
		return errors.New("agent revision digest does not match canonical content")
	}
	return nil
}

func MarshalAgentRevision(revision AgentRevision) ([]byte, error) {
	if err := validateSealedRevision(revision); err != nil {
		return nil, err
	}
	return json.Marshal(revision)
}

func UnmarshalAgentRevision(data []byte) (AgentRevision, error) {
	return decodeStrict(data, validateSealedRevision)
}

func MarshalAgentRegistration(registration AgentRegistration) ([]byte, error) {
	if err := registration.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(registration)
}

func UnmarshalAgentRegistration(data []byte) (AgentRegistration, error) {
	return decodeStrict(data, func(registration AgentRegistration) error { return registration.Validate() })
}

func decodeStrict[T any](data []byte, validate func(T) error) (T, error) {
	var value T
	if len(data) == 0 || !utf8.Valid(data) {
		return value, errors.New("registry wire value is empty or invalid UTF-8")
	}
	if err := rejectDuplicateObjectKeys(data); err != nil {
		return value, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, fmt.Errorf("decode registry value: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return value, errors.New("registry wire value contains trailing JSON value")
		}
		return value, fmt.Errorf("registry wire value contains trailing data: %w", err)
	}
	if err := validate(value); err != nil {
		return value, err
	}
	return value, nil
}

func rejectDuplicateObjectKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	first, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("decode registry tokens: %w", err)
	}
	if err := inspectJSONValue(decoder, first); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("registry wire value contains trailing JSON value")
		}
		return fmt.Errorf("registry wire value contains trailing data: %w", err)
	}
	return nil
}

func inspectJSONValue(decoder *json.Decoder, token json.Token) error {
	delimiter, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	switch delimiter {
	case '{':
		keys := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return fmt.Errorf("decode registry object key: %w", err)
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("registry object key is not a string")
			}
			if _, duplicate := keys[key]; duplicate {
				return fmt.Errorf("duplicate registry object key %q", key)
			}
			keys[key] = struct{}{}
			value, err := decoder.Token()
			if err != nil {
				return fmt.Errorf("decode registry object value: %w", err)
			}
			if err := inspectJSONValue(decoder, value); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return errors.New("registry object is not closed")
		}
	case '[':
		for decoder.More() {
			value, err := decoder.Token()
			if err != nil {
				return fmt.Errorf("decode registry array value: %w", err)
			}
			if err := inspectJSONValue(decoder, value); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return errors.New("registry array is not closed")
		}
	default:
		return errors.New("unexpected closing delimiter in registry wire value")
	}
	return nil
}
