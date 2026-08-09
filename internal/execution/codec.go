package execution

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"
)

var executionID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,254}$`)
var executionDigest = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func NewGraphRun(v GraphRun) (GraphRun, error) {
	v.SchemaVersion = GraphRunSchemaVersion
	v.State = StateRequested
	v.Digest = ""
	return sealRecord(v, validateGraphRun)
}
func NewLoopExecution(v LoopExecution) (LoopExecution, error) {
	v.SchemaVersion = LoopExecutionSchemaVersion
	v.State = StateRequested
	v.Digest = ""
	return sealRecord(v, validateLoopExecution)
}
func NewAttempt(v Attempt) (Attempt, error) {
	v.SchemaVersion = AttemptSchemaVersion
	v.State = StateRequested
	v.Digest = ""
	return sealRecord(v, validateAttempt)
}
func MarshalGraphRun(v GraphRun) ([]byte, error) { return marshalRecord(v, validateGraphRun) }
func MarshalLoopExecution(v LoopExecution) ([]byte, error) {
	return marshalRecord(v, validateLoopExecution)
}
func MarshalAttempt(v Attempt) ([]byte, error) { return marshalRecord(v, validateAttempt) }
func UnmarshalGraphRun(b []byte) (GraphRun, error) {
	return decodeRecord[GraphRun](b, validateGraphRun)
}
func UnmarshalLoopExecution(b []byte) (LoopExecution, error) {
	return decodeRecord[LoopExecution](b, validateLoopExecution)
}
func UnmarshalAttempt(b []byte) (Attempt, error) { return decodeRecord[Attempt](b, validateAttempt) }

func validateGraphRun(v GraphRun) error {
	if v.SchemaVersion != GraphRunSchemaVersion || !validExecutionID(v.GraphRunID) || v.QueueItem.Validate() != nil || v.Snapshot.Validate() != nil || v.Authority.Validate() != nil || v.State != StateRequested || v.CreatedAt.IsZero() {
		return errors.New("invalid initial Graph run")
	}
	return checkRecordDigest(v, v.Digest)
}
func validateLoopExecution(v LoopExecution) error {
	if v.SchemaVersion != LoopExecutionSchemaVersion || !validExecutionID(v.LoopExecutionID) || !validExecutionID(v.GraphRunID) || !validExecutionID(v.GraphNodeID) || v.Loop.Validate() != nil || v.Participant.Validate() != nil || v.State != StateRequested || v.CreatedAt.IsZero() {
		return errors.New("invalid initial Loop execution")
	}
	return checkRecordDigest(v, v.Digest)
}
func validateAttempt(v Attempt) error {
	if v.SchemaVersion != AttemptSchemaVersion || !validExecutionID(v.AttemptID) || !validExecutionID(v.GraphRunID) || !validExecutionID(v.LoopExecutionID) || v.QueueItem.Validate() != nil || !validExecutionID(v.ClaimID) || v.AttemptNumber == 0 || v.AttemptNumber > 100 || v.State != StateRequested || v.CreatedAt.IsZero() {
		return errors.New("invalid initial execution attempt")
	}
	return checkRecordDigest(v, v.Digest)
}
func validExecutionID(v string) bool {
	return utf8.ValidString(v) && strings.TrimSpace(v) == v && executionID.MatchString(v)
}
func sealRecord[T any](v T, validate func(T) error) (T, error) {
	wire, _ := json.Marshal(v)
	var obj map[string]json.RawMessage
	_ = json.Unmarshal(wire, &obj)
	obj["digest"], _ = json.Marshal("sha256:" + strings.Repeat("0", 64))
	candidateWire, _ := json.Marshal(obj)
	var candidate T
	_ = json.Unmarshal(candidateWire, &candidate)
	if err := validate(candidate); err != nil && !strings.Contains(err.Error(), "digest mismatch") {
		var z T
		return z, err
	}
	obj["digest"], _ = json.Marshal("")
	digestWire, _ := json.Marshal(obj)
	sum := sha256.Sum256(digestWire)
	obj["digest"], _ = json.Marshal("sha256:" + hex.EncodeToString(sum[:]))
	wire, _ = json.Marshal(obj)
	var out T
	_ = json.Unmarshal(wire, &out)
	if err := validate(out); err != nil {
		var z T
		return z, err
	}
	return out, nil
}
func checkRecordDigest[T any](v T, digest string) error {
	if !executionDigest.MatchString(digest) {
		return errors.New("invalid execution record digest")
	}
	wire, _ := json.Marshal(v)
	var obj map[string]json.RawMessage
	_ = json.Unmarshal(wire, &obj)
	obj["digest"], _ = json.Marshal("")
	wire, _ = json.Marshal(obj)
	sum := sha256.Sum256(wire)
	if digest != "sha256:"+hex.EncodeToString(sum[:]) {
		return errors.New("execution record digest mismatch")
	}
	return nil
}
func marshalRecord[T any](v T, validate func(T) error) ([]byte, error) {
	if err := validate(v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}
func decodeRecord[T any](data []byte, validate func(T) error) (T, error) {
	var v T
	if len(data) == 0 || !utf8.Valid(data) {
		return v, errors.New("execution wire value is empty or invalid UTF-8")
	}
	if err := rejectExecutionDuplicates(data); err != nil {
		return v, err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&v); err != nil {
		return v, fmt.Errorf("decode execution record: %w", err)
	}
	var x any
	if err := d.Decode(&x); !errors.Is(err, io.EOF) {
		return v, errors.New("execution wire value contains trailing data")
	}
	if err := validate(v); err != nil {
		return v, err
	}
	return v, nil
}
func rejectExecutionDuplicates(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	token, err := d.Token()
	if err != nil {
		return err
	}
	if err = walkExecutionJSON(d, token); err != nil {
		return err
	}
	if _, err = d.Token(); !errors.Is(err, io.EOF) {
		return errors.New("execution wire value contains trailing data")
	}
	return nil
}
func walkExecutionJSON(d *json.Decoder, token json.Token) error {
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]struct{}{}
		for d.More() {
			raw, err := d.Token()
			if err != nil {
				return err
			}
			key, ok := raw.(string)
			if !ok {
				return errors.New("invalid execution object key")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate execution object key %q", key)
			}
			seen[key] = struct{}{}
			value, err := d.Token()
			if err != nil {
				return err
			}
			if err = walkExecutionJSON(d, value); err != nil {
				return err
			}
		}
		_, err := d.Token()
		return err
	case '[':
		for d.More() {
			value, err := d.Token()
			if err != nil {
				return err
			}
			if err = walkExecutionJSON(d, value); err != nil {
				return err
			}
		}
		_, err := d.Token()
		return err
	}
	return errors.New("unexpected execution closing delimiter")
}
