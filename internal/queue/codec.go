package queue

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

var (
	idPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,254}$`)
	digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

func NewSubmission(value Submission) (Submission, error) {
	value.SchemaVersion = SubmissionSchemaVersion
	value.Digest = ""
	return seal(value, validateSubmission)
}
func NewRejection(value Rejection) (Rejection, error) {
	value.SchemaVersion = RejectionSchemaVersion
	value.Digest = ""
	return seal(value, validateRejection)
}
func NewItem(value Item) (Item, error) {
	value.SchemaVersion = ItemSchemaVersion
	value.State = StateQueued
	value.Digest = ""
	return seal(value, validateItem)
}
func NewClaim(value Claim) (Claim, error) {
	value.SchemaVersion = ClaimSchemaVersion
	value.Digest = ""
	return seal(value, validateClaim)
}
func NewTransition(value QueueTransition) (QueueTransition, error) {
	value.SchemaVersion = TransitionSchemaVersion
	value.Digest = ""
	return seal(value, validateTransition)
}
func NewRetry(value Retry) (Retry, error) {
	value.SchemaVersion, value.Digest = RetrySchemaVersion, ""
	return seal(value, validateRetry)
}
func NewCancellation(value Cancellation) (Cancellation, error) {
	value.SchemaVersion, value.Digest = CancellationSchemaVersion, ""
	return seal(value, validateCancellation)
}
func NewProjection(value Projection) (Projection, error) {
	value.SchemaVersion, value.Digest = ProjectionSchemaVersion, ""
	return seal(value, validateProjection)
}

func MarshalSubmission(v Submission) ([]byte, error)      { return marshal(v, validateSubmission) }
func MarshalRejection(v Rejection) ([]byte, error)        { return marshal(v, validateRejection) }
func MarshalItem(v Item) ([]byte, error)                  { return marshal(v, validateItem) }
func MarshalClaim(v Claim) ([]byte, error)                { return marshal(v, validateClaim) }
func MarshalTransition(v QueueTransition) ([]byte, error) { return marshal(v, validateTransition) }
func MarshalRetry(v Retry) ([]byte, error)                { return marshal(v, validateRetry) }
func MarshalCancellation(v Cancellation) ([]byte, error)  { return marshal(v, validateCancellation) }
func MarshalProjection(v Projection) ([]byte, error)      { return marshal(v, validateProjection) }
func UnmarshalSubmission(b []byte) (Submission, error) {
	return decode[Submission](b, validateSubmission)
}
func UnmarshalRejection(b []byte) (Rejection, error) { return decode[Rejection](b, validateRejection) }
func UnmarshalItem(b []byte) (Item, error)           { return decode[Item](b, validateItem) }
func UnmarshalClaim(b []byte) (Claim, error)         { return decode[Claim](b, validateClaim) }
func UnmarshalTransition(b []byte) (QueueTransition, error) {
	return decode[QueueTransition](b, validateTransition)
}
func UnmarshalRetry(b []byte) (Retry, error) { return decode[Retry](b, validateRetry) }
func UnmarshalCancellation(b []byte) (Cancellation, error) {
	return decode[Cancellation](b, validateCancellation)
}
func UnmarshalProjection(b []byte) (Projection, error) {
	return decode[Projection](b, validateProjection)
}

func validateSubmission(v Submission) error {
	if v.SchemaVersion != SubmissionSchemaVersion || !validID(v.SubmissionID) || !validID(v.IdempotencyKey) || !validID(v.MandateID) || !validID(v.Runtime) || v.SubmittedAt.IsZero() || v.Snapshot.Validate() != nil || v.Authority.Validate() != nil {
		return errors.New("invalid queue submission")
	}
	return validateDigest(v, v.Digest)
}
func validateRejection(v Rejection) error {
	if v.SchemaVersion != RejectionSchemaVersion || !validID(v.RejectionID) || !validID(v.SubmissionID) || !validID(v.IdempotencyKey) || !validID(v.ReasonCode) || !validReason(v.Reason) || v.RejectedAt.IsZero() {
		return errors.New("invalid queue rejection")
	}
	return validateDigest(v, v.Digest)
}
func validateItem(v Item) error {
	if v.SchemaVersion != ItemSchemaVersion || !validID(v.ItemID) || !validID(v.GraphRunID) || v.State != StateQueued || v.MaxAttempts == 0 || v.MaxAttempts > MaxAttempts || v.EnqueuedAt.IsZero() || v.AvailableAt.Before(v.EnqueuedAt) || len(v.Dependencies) > MaxDependencies || v.Submission.Validate() != nil || v.Snapshot.Validate() != nil || v.Authority.Validate() != nil {
		return errors.New("invalid queue item")
	}
	seen := make(map[string]struct{}, len(v.Dependencies))
	for _, dependency := range v.Dependencies {
		if dependency.Validate() != nil || dependency.ID == v.ItemID {
			return errors.New("invalid queue item dependency")
		}
		if _, exists := seen[dependency.ID]; exists {
			return errors.New("duplicate queue item dependency")
		}
		seen[dependency.ID] = struct{}{}
	}
	return validateDigest(v, v.Digest)
}
func validateClaim(v Claim) error {
	if v.SchemaVersion != ClaimSchemaVersion || !validID(v.ClaimID) || !validID(v.AttemptID) || !validID(v.WorkerID) || v.QueueItem.Validate() != nil || v.Authority.Validate() != nil || v.ClaimedAt.IsZero() || !v.ExpiresAt.After(v.ClaimedAt) {
		return errors.New("invalid queue claim")
	}
	return validateDigest(v, v.Digest)
}
func validateTransition(v QueueTransition) error {
	legal := (v.From == "" && v.To == StateQueued && v.ClaimID == "") ||
		(v.From == StateQueued && v.To == StateClaimed && validID(v.ClaimID)) ||
		(v.From == StateClaimed && v.To == StateQueued && validID(v.ClaimID)) ||
		(v.From == StateQueued && v.To == StateCancelled && v.ClaimID == "") ||
		(v.From == StateClaimed && terminalState(v.To) && validID(v.ClaimID))
	if v.SchemaVersion != TransitionSchemaVersion || !validID(v.TransitionID) || !validID(v.QueueItemID) || !legal || !validReason(v.Reason) || v.OccurredAt.IsZero() {
		return errors.New("invalid queue transition")
	}
	return validateDigest(v, v.Digest)
}
func validateRetry(v Retry) error {
	if v.SchemaVersion != RetrySchemaVersion || !validID(v.RetryID) || v.QueueItem.Validate() != nil || !validID(v.ClaimID) || v.AttemptNumber == 0 || v.AttemptNumber > MaxAttempts || v.OccurredAt.IsZero() || v.AvailableAt.Before(v.OccurredAt) || !validReason(v.Reason) {
		return errors.New("invalid queue retry")
	}
	return validateDigest(v, v.Digest)
}
func validateCancellation(v Cancellation) error {
	if v.SchemaVersion != CancellationSchemaVersion || !validID(v.CancellationID) || v.QueueItem.Validate() != nil || (v.ClaimID != "" && !validID(v.ClaimID)) || !validReason(v.Reason) || v.OccurredAt.IsZero() {
		return errors.New("invalid queue cancellation")
	}
	return validateDigest(v, v.Digest)
}
func validateProjection(v Projection) error {
	activeOK := (v.State == StateClaimed && validID(v.ActiveClaimID)) || (v.State != StateClaimed && v.ActiveClaimID == "")
	stateOK := v.State == StateQueued || v.State == StateClaimed || terminalState(v.State)
	if v.SchemaVersion != ProjectionSchemaVersion || !validID(v.QueueItemID) || !validID(v.LastTransitionID) || !activeOK || !stateOK || v.Attempts > MaxAttempts || v.AvailableAt.IsZero() || v.UpdatedAt.IsZero() {
		return errors.New("invalid queue projection")
	}
	return validateDigest(v, v.Digest)
}
func terminalState(state State) bool {
	switch state {
	case StateSucceeded, StateFailed, StateDenied, StateCancelled, StateExpired:
		return true
	default:
		return false
	}
}
func validID(v string) bool {
	return utf8.ValidString(v) && strings.TrimSpace(v) == v && idPattern.MatchString(v)
}
func validReason(v string) bool {
	return utf8.ValidString(v) && strings.TrimSpace(v) == v && len(v) > 0 && len(v) <= MaxReasonBytes
}

func seal[T any](v T, validate func(T) error) (T, error) {
	if err := validateWithoutDigest(v, validate); err != nil {
		var zero T
		return zero, err
	}
	wire, err := json.Marshal(v)
	if err != nil {
		var zero T
		return zero, err
	}
	var digestObject map[string]json.RawMessage
	_ = json.Unmarshal(wire, &digestObject)
	digestObject["digest"], _ = json.Marshal("")
	digestWire, _ := json.Marshal(digestObject)
	digest := sha256.Sum256(digestWire)
	encoded, _ := json.Marshal(v)
	var object map[string]json.RawMessage
	_ = json.Unmarshal(encoded, &object)
	object["digest"], _ = json.Marshal("sha256:" + hex.EncodeToString(digest[:]))
	encoded, _ = json.Marshal(object)
	var out T
	_ = json.Unmarshal(encoded, &out)
	if err = validate(out); err != nil {
		var zero T
		return zero, err
	}
	return out, nil
}
func validateWithoutDigest[T any](v T, validate func(T) error) error {
	wire, _ := json.Marshal(v)
	var object map[string]json.RawMessage
	_ = json.Unmarshal(wire, &object)
	object["digest"], _ = json.Marshal("sha256:" + strings.Repeat("0", 64))
	wire, _ = json.Marshal(object)
	var candidate T
	_ = json.Unmarshal(wire, &candidate)
	if err := validate(candidate); err != nil && !strings.Contains(err.Error(), "digest mismatch") {
		return err
	}
	return nil
}
func validateDigest[T any](v T, digest string) error {
	if !digestPattern.MatchString(digest) {
		return errors.New("invalid queue record digest")
	}
	wire, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var object map[string]json.RawMessage
	if err = json.Unmarshal(wire, &object); err != nil {
		return err
	}
	object["digest"], _ = json.Marshal("")
	wire, _ = json.Marshal(object)
	sum := sha256.Sum256(wire)
	if digest != "sha256:"+hex.EncodeToString(sum[:]) {
		return errors.New("queue record digest mismatch")
	}
	return nil
}
func marshal[T any](v T, validate func(T) error) ([]byte, error) {
	if err := validate(v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}
func decode[T any](data []byte, validate func(T) error) (T, error) {
	var v T
	if len(data) == 0 || !utf8.Valid(data) {
		return v, errors.New("queue wire value is empty or invalid UTF-8")
	}
	if err := rejectDuplicates(data); err != nil {
		return v, err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&v); err != nil {
		return v, fmt.Errorf("decode queue record: %w", err)
	}
	var x any
	if err := d.Decode(&x); !errors.Is(err, io.EOF) {
		return v, errors.New("queue wire value contains trailing data")
	}
	if err := validate(v); err != nil {
		return v, err
	}
	return v, nil
}
func rejectDuplicates(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	token, err := d.Token()
	if err != nil {
		return err
	}
	if err = walk(d, token); err != nil {
		return err
	}
	if _, err = d.Token(); !errors.Is(err, io.EOF) {
		return errors.New("queue wire value contains trailing data")
	}
	return nil
}
func walk(d *json.Decoder, token json.Token) error {
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]struct{}{}
		for d.More() {
			k, _ := d.Token()
			key, ok := k.(string)
			if !ok {
				return errors.New("invalid object key")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate queue object key %q", key)
			}
			seen[key] = struct{}{}
			v, err := d.Token()
			if err != nil {
				return err
			}
			if err = walk(d, v); err != nil {
				return err
			}
		}
		_, err := d.Token()
		return err
	case '[':
		for d.More() {
			v, err := d.Token()
			if err != nil {
				return err
			}
			if err = walk(d, v); err != nil {
				return err
			}
		}
		_, err := d.Token()
		return err
	}
	return errors.New("unexpected closing delimiter")
}
