package core

import (
	"errors"
	"fmt"
	"time"
)

const (
	AuthorityRecordCommand    = "authority_command"
	AuthorityRecordFact       = "authority_fact"
	AuthorityRecordReceipt    = "authority_receipt"
	AuthorityRecordProjection = "authority_projection"
	AuthorityRecordReplay     = "authority_replay"
)

// AuthorityCommandKind is a closed controller command vocabulary. Adding a
// kind is a schema change; arbitrary strings and model-authored instructions
// are not authority commands.
type AuthorityCommandKind string

const (
	AuthorityCommandActivate AuthorityCommandKind = "activate"
	AuthorityCommandRevoke   AuthorityCommandKind = "revoke"
	AuthorityCommandExpire   AuthorityCommandKind = "expire"
)

// AuthorityCommand captures authenticated intent and the exact fact-chain
// precondition against which the controller must evaluate it.
type AuthorityCommand struct {
	ID                     string               `json:"id"`
	Kind                   AuthorityCommandKind `json:"command_kind"`
	MandateID              string               `json:"mandate_id"`
	AuthorityContextID     string               `json:"authority_context_id"`
	ExpectedSequence       uint64               `json:"expected_sequence"`
	ExpectedPreviousDigest string               `json:"expected_previous_digest,omitempty"`
	ActorSubjectID         string               `json:"actor_subject_id"`
	ActorAuthentication    string               `json:"actor_authentication"`
	IssuedAt               time.Time            `json:"issued_at"`
	ExpiresAt              time.Time            `json:"expires_at"`
	Reason                 string               `json:"reason,omitempty"`
	Digest                 string               `json:"digest"`
}

// AuthorityFact is an immutable controller-authored result of one accepted
// command. Command identity is retained so replay never infers authority from
// a receipt, projection, or human-readable reason.
type AuthorityFact struct {
	ID                 string         `json:"id"`
	Sequence           uint64         `json:"sequence"`
	CommandID          string         `json:"command_id"`
	CommandDigest      string         `json:"command_digest"`
	MandateID          string         `json:"mandate_id"`
	AuthorityContextID string         `json:"authority_context_id"`
	From               AuthorityState `json:"from,omitempty"`
	To                 AuthorityState `json:"to"`
	OccurredAt         time.Time      `json:"occurred_at"`
	RecordedBy         string         `json:"recorded_by"`
	PreviousDigest     string         `json:"previous_digest,omitempty"`
	Digest             string         `json:"digest"`
}

// AuthorityReceipt is non-authoritative processing evidence. Accepted
// receipts point at canonical facts; rejected receipts cannot point at facts
// or projections and never change replayed authority.
type AuthorityReceipt struct {
	ID               string    `json:"id"`
	CommandID        string    `json:"command_id"`
	CommandDigest    string    `json:"command_digest"`
	Accepted         bool      `json:"accepted"`
	FactID           string    `json:"fact_id,omitempty"`
	FactDigest       string    `json:"fact_digest,omitempty"`
	ProjectionDigest string    `json:"projection_digest,omitempty"`
	ReasonCode       string    `json:"reason_code"`
	RecordedAt       time.Time `json:"recorded_at"`
	RecordedBy       string    `json:"recorded_by"`
	Digest           string    `json:"digest"`
}

// AuthorityProjection is rebuildable derived state. It records its complete
// canonical source boundary but grants no authority on its own.
type AuthorityProjection struct {
	MandateID          string         `json:"mandate_id"`
	AuthorityContextID string         `json:"authority_context_id"`
	State              AuthorityState `json:"state"`
	SourceSequence     uint64         `json:"source_sequence"`
	SourceFactID       string         `json:"source_fact_id"`
	SourceFactDigest   string         `json:"source_fact_digest"`
	SourceOccurredAt   time.Time      `json:"source_occurred_at"`
	Digest             string         `json:"digest"`
}

// AuthorityReplay is a portable deterministic representation. Receipts are
// retained as evidence but replay consumes only verified commands and facts.
type AuthorityReplay struct {
	Commands []AuthorityCommand `json:"commands"`
	Facts    []AuthorityFact    `json:"facts"`
	Receipts []AuthorityReceipt `json:"receipts,omitempty"`
}

func EncodeAuthorityCommandCanonical(value AuthorityCommand) ([]byte, error) {
	return encodeAuthorityRecord(AuthorityRecordCommand, value)
}
func DecodeAuthorityCommandCanonical(data []byte) (AuthorityCommand, error) {
	return decodeAuthorityRecord[AuthorityCommand](data, AuthorityRecordCommand)
}
func EncodeAuthorityFactCanonical(value AuthorityFact) ([]byte, error) {
	return encodeAuthorityRecord(AuthorityRecordFact, value)
}
func DecodeAuthorityFactCanonical(data []byte) (AuthorityFact, error) {
	return decodeAuthorityRecord[AuthorityFact](data, AuthorityRecordFact)
}
func EncodeAuthorityReceiptCanonical(value AuthorityReceipt) ([]byte, error) {
	return encodeAuthorityRecord(AuthorityRecordReceipt, value)
}
func DecodeAuthorityReceiptCanonical(data []byte) (AuthorityReceipt, error) {
	return decodeAuthorityRecord[AuthorityReceipt](data, AuthorityRecordReceipt)
}
func EncodeAuthorityProjectionCanonical(value AuthorityProjection) ([]byte, error) {
	return encodeAuthorityRecord(AuthorityRecordProjection, value)
}
func DecodeAuthorityProjectionCanonical(data []byte) (AuthorityProjection, error) {
	return decodeAuthorityRecord[AuthorityProjection](data, AuthorityRecordProjection)
}
func EncodeAuthorityReplayCanonical(value AuthorityReplay) ([]byte, error) {
	return encodeAuthorityRecord(AuthorityRecordReplay, value)
}
func DecodeAuthorityReplayCanonical(data []byte) (AuthorityReplay, error) {
	return decodeAuthorityRecord[AuthorityReplay](data, AuthorityRecordReplay)
}

func AuthorityCommandDigest(value AuthorityCommand) string {
	value.Digest = ""
	encoded, err := EncodeAuthorityCommandCanonical(value)
	if err != nil {
		return ""
	}
	return CanonicalAuthorityDigest(encoded)
}
func AuthorityFactDigest(value AuthorityFact) string {
	value.Digest = ""
	encoded, err := EncodeAuthorityFactCanonical(value)
	if err != nil {
		return ""
	}
	return CanonicalAuthorityDigest(encoded)
}
func AuthorityReceiptDigest(value AuthorityReceipt) string {
	value.Digest = ""
	encoded, err := EncodeAuthorityReceiptCanonical(value)
	if err != nil {
		return ""
	}
	return CanonicalAuthorityDigest(encoded)
}
func AuthorityProjectionDigest(value AuthorityProjection) string {
	value.Digest = ""
	encoded, err := EncodeAuthorityProjectionCanonical(value)
	if err != nil {
		return ""
	}
	return CanonicalAuthorityDigest(encoded)
}

func validAuthorityCommandKind(kind AuthorityCommandKind) bool {
	return kind == AuthorityCommandActivate || kind == AuthorityCommandRevoke || kind == AuthorityCommandExpire
}

func ValidateAuthorityCommand(command AuthorityCommand) error {
	if command.ID == "" || !validAuthorityCommandKind(command.Kind) || command.MandateID == "" || command.AuthorityContextID == "" ||
		command.ExpectedSequence == 0 || command.ActorSubjectID == "" || command.ActorAuthentication == "" || command.IssuedAt.IsZero() ||
		!command.ExpiresAt.After(command.IssuedAt) || command.Digest == "" || command.Digest != AuthorityCommandDigest(command) {
		return errors.New("authority command is incomplete, unknown, expired, or has an invalid digest")
	}
	if command.Kind == AuthorityCommandActivate {
		if command.ExpectedSequence != 1 || command.ExpectedPreviousDigest != "" {
			return errors.New("activation command has invalid genesis precondition")
		}
	} else if command.ExpectedSequence < 2 || command.ExpectedPreviousDigest == "" {
		return errors.New("terminal command has invalid chain precondition")
	}
	return nil
}

// ValidateAuthorityCommandAt applies the command's short-lived acceptance
// window. Historical replay does not use this check: an accepted immutable
// fact remains replayable after its originating command expires.
func ValidateAuthorityCommandAt(command AuthorityCommand, now time.Time) error {
	if err := ValidateAuthorityCommand(command); err != nil {
		return err
	}
	if now.IsZero() || now.Before(command.IssuedAt) || now.After(command.ExpiresAt) {
		return errors.New("authority command is outside its acceptance window")
	}
	return nil
}

// ReplayAuthorityFacts reconstructs authority from controller-authored facts
// alone. Commands explain and constrain acceptance, but retained verified facts
// are the sole canonical input required to rebuild authority state.
func ReplayAuthorityFacts(facts []AuthorityFact) (AuthorityProjection, error) {
	if len(facts) == 0 {
		return AuthorityProjection{}, errors.New("canonical authority fact chain is empty")
	}
	seenFacts := make(map[string]struct{}, len(facts))
	seenCommands := make(map[string]struct{}, len(facts))
	var projection AuthorityProjection
	for index, fact := range facts {
		if fact.ID == "" || fact.CommandID == "" || fact.CommandDigest == "" || fact.MandateID == "" || fact.AuthorityContextID == "" ||
			fact.Sequence != uint64(index+1) || fact.OccurredAt.IsZero() || fact.RecordedBy == "" || fact.Digest == "" || fact.Digest != AuthorityFactDigest(fact) {
			return AuthorityProjection{}, fmt.Errorf("authority fact %d is incomplete or invalid", index)
		}
		if _, duplicate := seenFacts[fact.ID]; duplicate {
			return AuthorityProjection{}, fmt.Errorf("authority fact %d is duplicated", index)
		}
		if _, duplicate := seenCommands[fact.CommandID]; duplicate {
			return AuthorityProjection{}, fmt.Errorf("authority fact %d reuses a command", index)
		}
		seenFacts[fact.ID] = struct{}{}
		seenCommands[fact.CommandID] = struct{}{}
		if index == 0 {
			if fact.From != "" || fact.To != AuthorityStateActive || fact.PreviousDigest != "" {
				return AuthorityProjection{}, errors.New("canonical authority facts must begin with activation")
			}
			projection.MandateID, projection.AuthorityContextID = fact.MandateID, fact.AuthorityContextID
		} else if fact.PreviousDigest != projection.SourceFactDigest || fact.From != projection.State || projection.State != AuthorityStateActive ||
			fact.MandateID != projection.MandateID || fact.AuthorityContextID != projection.AuthorityContextID || fact.OccurredAt.Before(projection.SourceOccurredAt) ||
			(fact.To != AuthorityStateRevoked && fact.To != AuthorityStateExpired) {
			return AuthorityProjection{}, fmt.Errorf("authority fact %d does not extend the verified projection", index)
		}
		projection.State = fact.To
		projection.SourceSequence = fact.Sequence
		projection.SourceFactID = fact.ID
		projection.SourceFactDigest = fact.Digest
		projection.SourceOccurredAt = fact.OccurredAt
	}
	projection.Digest = AuthorityProjectionDigest(projection)
	return projection, nil
}

// ReplayCanonicalAuthority verifies every command/fact pair before deriving a
// projection. The output depends only on canonical input; receipts and wall
// clock time are deliberately not inputs.
func ReplayCanonicalAuthority(commands []AuthorityCommand, facts []AuthorityFact) (AuthorityProjection, error) {
	if len(commands) == 0 || len(commands) != len(facts) {
		return AuthorityProjection{}, errors.New("canonical authority replay is incomplete")
	}
	factsProjection, err := ReplayAuthorityFacts(facts)
	if err != nil {
		return AuthorityProjection{}, err
	}
	seenCommands := make(map[string]struct{}, len(commands))
	seenFacts := make(map[string]struct{}, len(facts))
	var projection AuthorityProjection
	for index := range commands {
		command, fact := commands[index], facts[index]
		if err := ValidateAuthorityCommand(command); err != nil {
			return AuthorityProjection{}, fmt.Errorf("command %d: %w", index, err)
		}
		if _, ok := seenCommands[command.ID]; ok {
			return AuthorityProjection{}, fmt.Errorf("command %d is duplicated", index)
		}
		if _, ok := seenFacts[fact.ID]; ok {
			return AuthorityProjection{}, fmt.Errorf("fact %d is duplicated", index)
		}
		seenCommands[command.ID], seenFacts[fact.ID] = struct{}{}, struct{}{}
		if fact.ID == "" || fact.Sequence != uint64(index+1) || fact.Sequence != command.ExpectedSequence || fact.CommandID != command.ID ||
			fact.CommandDigest != command.Digest || fact.MandateID != command.MandateID || fact.AuthorityContextID != command.AuthorityContextID ||
			fact.OccurredAt.IsZero() || fact.RecordedBy == "" || fact.OccurredAt.Before(command.IssuedAt) || fact.OccurredAt.After(command.ExpiresAt) ||
			fact.Digest == "" || fact.Digest != AuthorityFactDigest(fact) {
			return AuthorityProjection{}, fmt.Errorf("fact %d does not prove its command", index)
		}
		if index == 0 {
			if command.Kind != AuthorityCommandActivate || fact.From != "" || fact.To != AuthorityStateActive || fact.PreviousDigest != "" {
				return AuthorityProjection{}, errors.New("canonical authority replay must begin with activation")
			}
			projection.MandateID, projection.AuthorityContextID = fact.MandateID, fact.AuthorityContextID
		} else {
			if command.Kind == AuthorityCommandActivate || command.ExpectedPreviousDigest != projection.SourceFactDigest || fact.PreviousDigest != projection.SourceFactDigest ||
				fact.From != projection.State || projection.State != AuthorityStateActive || fact.MandateID != projection.MandateID || fact.AuthorityContextID != projection.AuthorityContextID ||
				fact.OccurredAt.Before(projection.SourceOccurredAt) {
				return AuthorityProjection{}, fmt.Errorf("fact %d does not extend the verified projection", index)
			}
			expected := AuthorityStateRevoked
			if command.Kind == AuthorityCommandExpire {
				expected = AuthorityStateExpired
			}
			if fact.To != expected {
				return AuthorityProjection{}, fmt.Errorf("fact %d does not match command kind", index)
			}
		}
		projection.State = fact.To
		projection.SourceSequence = fact.Sequence
		projection.SourceFactID = fact.ID
		projection.SourceFactDigest = fact.Digest
		projection.SourceOccurredAt = fact.OccurredAt
	}
	projection.Digest = AuthorityProjectionDigest(projection)
	if projection.Digest != factsProjection.Digest {
		return AuthorityProjection{}, errors.New("command replay diverged from canonical facts")
	}
	return factsProjection, nil
}

func ValidateAuthorityReceipt(receipt AuthorityReceipt) error {
	if receipt.ID == "" || receipt.CommandID == "" || receipt.CommandDigest == "" || receipt.ReasonCode == "" || receipt.RecordedAt.IsZero() || receipt.RecordedBy == "" ||
		receipt.Digest == "" || receipt.Digest != AuthorityReceiptDigest(receipt) {
		return errors.New("authority receipt is incomplete or has an invalid digest")
	}
	if receipt.Accepted {
		if receipt.FactID == "" || receipt.FactDigest == "" || receipt.ProjectionDigest == "" {
			return errors.New("accepted authority receipt lacks canonical references")
		}
	} else if receipt.FactID != "" || receipt.FactDigest != "" || receipt.ProjectionDigest != "" {
		return errors.New("rejected authority receipt claims an authority result")
	}
	return nil
}
