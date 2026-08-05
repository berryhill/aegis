package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

const AuthorityRecordSchema = "aegis.authority/v1"

const (
	AuthorityRecordMandate        = "mandate"
	AuthorityRecordContext        = "authority_context"
	AuthorityRecordTransitionFact = "authority_transition_fact"
	AuthorityRecordTransitionRoot = "authority_transition_root"
)

type AuthorityState string

const (
	AuthorityStateActive  AuthorityState = "active"
	AuthorityStateRevoked AuthorityState = "revoked"
	AuthorityStateExpired AuthorityState = "expired"
)

// AuthorityTransitionFact is controller-authored authority history. Facts are
// immutable, append-only, and hash-linked per authority context.
type AuthorityTransitionFact struct {
	ID                 string         `json:"id"`
	Sequence           uint64         `json:"sequence"`
	MandateID          string         `json:"mandate_id"`
	AuthorityContextID string         `json:"authority_context_id"`
	From               AuthorityState `json:"from,omitempty"`
	To                 AuthorityState `json:"to"`
	OccurredAt         time.Time      `json:"occurred_at"`
	RecordedBy         string         `json:"recorded_by"`
	Reason             string         `json:"reason,omitempty"`
	PreviousDigest     string         `json:"previous_digest,omitempty"`
	Digest             string         `json:"digest"`
}

// AuthorityTransitionRoot is a deterministic projection, not a mutable source
// of authority. It can always be reconstructed from the retained fact chain.
type AuthorityTransitionRoot struct {
	MandateID          string         `json:"mandate_id"`
	AuthorityContextID string         `json:"authority_context_id"`
	State              AuthorityState `json:"state"`
	Sequence           uint64         `json:"sequence"`
	LastFactID         string         `json:"last_fact_id"`
	LastFactDigest     string         `json:"last_fact_digest"`
	UpdatedAt          time.Time      `json:"updated_at"`
	Digest             string         `json:"digest"`
}

type authorityRecord[T any] struct {
	Schema string `json:"schema"`
	Kind   string `json:"kind"`
	Value  T      `json:"value"`
}

func encodeAuthorityRecord[T any](kind string, value T) ([]byte, error) {
	encoded, err := json.Marshal(authorityRecord[T]{Schema: AuthorityRecordSchema, Kind: kind, Value: value})
	if err != nil {
		return nil, fmt.Errorf("encode canonical authority %s: %w", kind, err)
	}
	return encoded, nil
}

func decodeAuthorityRecord[T any](data []byte, kind string) (T, error) {
	var record authorityRecord[T]
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return record.Value, fmt.Errorf("decode authority %s: %w", kind, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return record.Value, fmt.Errorf("decode authority %s: trailing data", kind)
	}
	if record.Schema != AuthorityRecordSchema || record.Kind != kind {
		return record.Value, fmt.Errorf("decode authority %s: schema or record kind mismatch", kind)
	}
	return record.Value, nil
}

func EncodeMandateCanonical(value Mandate) ([]byte, error) {
	return encodeAuthorityRecord(AuthorityRecordMandate, value)
}

func DecodeMandateCanonical(data []byte) (Mandate, error) {
	return decodeAuthorityRecord[Mandate](data, AuthorityRecordMandate)
}

func EncodeAuthorityContextCanonical(value AuthorityContext) ([]byte, error) {
	return encodeAuthorityRecord(AuthorityRecordContext, value)
}

func DecodeAuthorityContextCanonical(data []byte) (AuthorityContext, error) {
	return decodeAuthorityRecord[AuthorityContext](data, AuthorityRecordContext)
}

func EncodeAuthorityTransitionFactCanonical(value AuthorityTransitionFact) ([]byte, error) {
	return encodeAuthorityRecord(AuthorityRecordTransitionFact, value)
}

func DecodeAuthorityTransitionFactCanonical(data []byte) (AuthorityTransitionFact, error) {
	return decodeAuthorityRecord[AuthorityTransitionFact](data, AuthorityRecordTransitionFact)
}

func EncodeAuthorityTransitionRootCanonical(value AuthorityTransitionRoot) ([]byte, error) {
	return encodeAuthorityRecord(AuthorityRecordTransitionRoot, value)
}

func DecodeAuthorityTransitionRootCanonical(data []byte) (AuthorityTransitionRoot, error) {
	return decodeAuthorityRecord[AuthorityTransitionRoot](data, AuthorityRecordTransitionRoot)
}

func CanonicalAuthorityDigest(encoded []byte) string {
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func AuthorityTransitionFactDigest(fact AuthorityTransitionFact) string {
	fact.Digest = ""
	encoded, err := EncodeAuthorityTransitionFactCanonical(fact)
	if err != nil {
		return ""
	}
	return CanonicalAuthorityDigest(encoded)
}

func AuthorityTransitionRootDigest(root AuthorityTransitionRoot) string {
	root.Digest = ""
	encoded, err := EncodeAuthorityTransitionRootCanonical(root)
	if err != nil {
		return ""
	}
	return CanonicalAuthorityDigest(encoded)
}

// ReplayAuthorityTransitions verifies the entire retained chain before
// returning its projection. Missing, reordered, duplicated, malformed, or
// invalid facts deny reconstruction.
func ReplayAuthorityTransitions(facts []AuthorityTransitionFact) (AuthorityTransitionRoot, error) {
	if len(facts) == 0 {
		return AuthorityTransitionRoot{}, errors.New("authority transition chain is empty")
	}
	var root AuthorityTransitionRoot
	seen := make(map[string]struct{}, len(facts))
	for index, fact := range facts {
		if fact.ID == "" || fact.MandateID == "" || fact.AuthorityContextID == "" || fact.RecordedBy == "" || fact.OccurredAt.IsZero() {
			return AuthorityTransitionRoot{}, fmt.Errorf("authority transition fact %d is incomplete", index)
		}
		if _, duplicate := seen[fact.ID]; duplicate {
			return AuthorityTransitionRoot{}, fmt.Errorf("authority transition fact %d has duplicate id", index)
		}
		seen[fact.ID] = struct{}{}
		if fact.Sequence != uint64(index+1) || fact.Digest == "" || fact.Digest != AuthorityTransitionFactDigest(fact) {
			return AuthorityTransitionRoot{}, fmt.Errorf("authority transition fact %d has invalid sequence or digest", index)
		}
		if index == 0 {
			if fact.From != "" || fact.To != AuthorityStateActive || fact.PreviousDigest != "" {
				return AuthorityTransitionRoot{}, errors.New("authority transition chain must begin by activating exactly one context")
			}
			root.MandateID = fact.MandateID
			root.AuthorityContextID = fact.AuthorityContextID
		} else {
			if fact.MandateID != root.MandateID || fact.AuthorityContextID != root.AuthorityContextID ||
				fact.PreviousDigest != root.LastFactDigest || fact.From != root.State || fact.OccurredAt.Before(root.UpdatedAt) {
				return AuthorityTransitionRoot{}, fmt.Errorf("authority transition fact %d does not extend the current root", index)
			}
			if root.State != AuthorityStateActive || (fact.To != AuthorityStateRevoked && fact.To != AuthorityStateExpired) {
				return AuthorityTransitionRoot{}, fmt.Errorf("authority transition fact %d is not an allowed fail-closed transition", index)
			}
		}
		root.State = fact.To
		root.Sequence = fact.Sequence
		root.LastFactID = fact.ID
		root.LastFactDigest = fact.Digest
		root.UpdatedAt = fact.OccurredAt
	}
	root.Digest = AuthorityTransitionRootDigest(root)
	return root, nil
}
