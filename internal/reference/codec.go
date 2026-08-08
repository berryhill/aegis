package reference

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

// MarshalDigestRef returns a deterministic JSON representation after
// validating the complete immutable binding.
func MarshalDigestRef(ref DigestRef) ([]byte, error) {
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(ref)
}

// UnmarshalDigestRef strictly decodes exactly one DigestRef object.
func UnmarshalDigestRef(data []byte) (DigestRef, error) {
	return decodeStrict(data, func(ref DigestRef) error { return ref.Validate() })
}

// MarshalRevisionRef returns a deterministic JSON representation after
// validating the complete immutable binding.
func MarshalRevisionRef(ref RevisionRef) ([]byte, error) {
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(ref)
}

// UnmarshalRevisionRef strictly decodes exactly one RevisionRef object.
func UnmarshalRevisionRef(data []byte) (RevisionRef, error) {
	return decodeStrict(data, func(ref RevisionRef) error { return ref.Validate() })
}

func decodeStrict[T any](data []byte, validate func(T) error) (T, error) {
	var value T
	if len(data) == 0 || !utf8.Valid(data) {
		return value, errors.New("reference wire value is empty or invalid UTF-8")
	}
	if err := rejectDuplicateObjectKeys(data); err != nil {
		return value, err
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, fmt.Errorf("decode reference: %w", err)
	}
	if err := requireEOF(decoder); err != nil {
		return value, err
	}
	if err := validate(value); err != nil {
		return value, err
	}
	return value, nil
}

func requireEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("reference wire value contains trailing JSON value")
		}
		return fmt.Errorf("reference wire value contains trailing data: %w", err)
	}
	return nil
}

// rejectDuplicateObjectKeys walks the token stream before typed decoding.
// encoding/json otherwise accepts duplicate names using last-value-wins,
// which is unsafe for immutable identity bindings.
func rejectDuplicateObjectKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	first, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("decode reference tokens: %w", err)
	}
	if err := inspectValue(decoder, first); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("reference wire value contains trailing JSON value")
		}
		return fmt.Errorf("reference wire value contains trailing data: %w", err)
	}
	return nil
}

func inspectValue(decoder *json.Decoder, token json.Token) error {
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
				return fmt.Errorf("decode reference object key: %w", err)
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("reference object key is not a string")
			}
			if _, duplicate := keys[key]; duplicate {
				return fmt.Errorf("duplicate reference object key %q", key)
			}
			keys[key] = struct{}{}
			valueToken, err := decoder.Token()
			if err != nil {
				return fmt.Errorf("decode reference object value: %w", err)
			}
			if err := inspectValue(decoder, valueToken); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return errors.New("reference object is not closed")
		}
	case '[':
		for decoder.More() {
			item, err := decoder.Token()
			if err != nil {
				return fmt.Errorf("decode reference array value: %w", err)
			}
			if err := inspectValue(decoder, item); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return errors.New("reference array is not closed")
		}
	default:
		return errors.New("unexpected closing delimiter in reference wire value")
	}
	return nil
}
