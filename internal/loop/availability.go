package loop

import (
	"encoding/json"
	"errors"
	"time"
)

// Availability is logical discovery metadata, not execution lifecycle or authority.
type Availability string

const (
	AvailabilityPublished          Availability = "published"
	AvailabilityArchived           Availability = "archived"
	AvailabilityEventSchemaVersion              = "aegis.loop.availability-event.v1"
)

func (v Availability) Valid() bool { return v == AvailabilityPublished || v == AvailabilityArchived }

// AvailabilityEvent applies to every current and future revision of LoopID.
// Head records the exact publication observed when the decision was made.
type AvailabilityEvent struct {
	SchemaVersion  string             `json:"schema_version"`
	EventID        string             `json:"event_id"`
	LoopID         string             `json:"loop_id"`
	Sequence       uint64             `json:"sequence"`
	Head           ProvenanceRevision `json:"head"`
	ExpectedState  Availability       `json:"expected_state"`
	State          Availability       `json:"state"`
	PreviousDigest string             `json:"previous_digest,omitempty"`
	PublisherAgent ProvenanceRevision `json:"publisher_agent"`
	OwnerID        string             `json:"owner_id"`
	PrincipalID    string             `json:"principal_id"`
	Authority      ProvenanceDigest   `json:"authority"`
	OccurredAt     time.Time          `json:"occurred_at"`
	Digest         string             `json:"digest"`
}

// Availability includes a CAS digest to prevent stale-state ABA writes.
type AvailabilityStatus struct {
	LoopID   string       `json:"loop_id"`
	State    Availability `json:"state"`
	Sequence uint64       `json:"sequence"`
	Digest   string       `json:"digest,omitempty"`
}

func NewAvailabilityEvent(v AvailabilityEvent) (AvailabilityEvent, error) {
	v.SchemaVersion = AvailabilityEventSchemaVersion
	v.OccurredAt = v.OccurredAt.UTC()
	v.Digest = ""
	if err := validateAvailabilityEvent(v, false); err != nil {
		return AvailabilityEvent{}, err
	}
	v.Digest = recordDigest(v)
	return v, nil
}

// ValidateAvailabilityInput validates only caller-supplied event fields, before
// server-derived ownership, authority and occurrence metadata are attached.
func ValidateAvailabilityInput(v AvailabilityEvent) error {
	if !validID(v.EventID) || !validID(v.LoopID) || v.Sequence == 0 || !v.Head.valid() || v.Head.ID != v.LoopID || !v.ExpectedState.Valid() || !v.State.Valid() || v.State == v.ExpectedState {
		return errors.New("invalid Loop availability event")
	}
	if (v.Sequence == 1 && v.PreviousDigest != "") || (v.Sequence > 1 && !validDigest(v.PreviousDigest)) {
		return errors.New("invalid Loop availability predecessor")
	}
	return nil
}

func validateAvailabilityEvent(v AvailabilityEvent, sealed bool) error {
	if err := ValidateAvailabilityInput(v); err != nil {
		return err
	}
	if v.SchemaVersion != AvailabilityEventSchemaVersion || !v.PublisherAgent.valid() || !validID(v.OwnerID) || !validID(v.PrincipalID) || !v.Authority.valid() || v.OccurredAt.IsZero() {
		return errors.New("invalid Loop availability event")
	}
	if sealed {
		digest := v.Digest
		v.Digest = ""
		if digest != recordDigest(v) {
			return errors.New("Loop availability digest mismatch")
		}
	}
	return nil
}

func MarshalAvailabilityEvent(v AvailabilityEvent) ([]byte, error) {
	if err := validateAvailabilityEvent(v, true); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

func UnmarshalAvailabilityEvent(data []byte) (AvailabilityEvent, error) {
	v, err := decodeStrict[AvailabilityEvent](data)
	if err != nil {
		return AvailabilityEvent{}, err
	}
	return v, validateAvailabilityEvent(v, true)
}

// SameAvailabilityIntent ignores only server-assigned occurrence time and digest.
func SameAvailabilityIntent(a, b AvailabilityEvent) bool {
	a.OccurredAt, b.OccurredAt = time.Time{}, time.Time{}
	a.Digest, b.Digest = "", ""
	return a == b
}
