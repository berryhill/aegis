package loop

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	PublicationProvenanceSchemaVersion = "aegis.loop.publication-provenance.v1"
	LifecycleEventSchemaVersion        = "aegis.loop.lifecycle-event.v1"
	provenanceRevisionSchemaVersion    = "aegis.reference.revision.v1"
	provenanceDigestSchemaVersion      = "aegis.reference.digest.v1"
)

// ProvenanceRevision and ProvenanceDigest are dependency-free wire values
// owned by the Loop record. Outer layers copy admitted canonical references
// into these values; the Loop domain validates and seals the resulting record.
type ProvenanceRevision struct {
	SchemaVersion string `json:"schema_version"`
	ID            string `json:"id"`
	Revision      uint64 `json:"revision"`
	Digest        string `json:"digest"`
}

func NewProvenanceRevision(id string, revision uint64, digest string) ProvenanceRevision {
	return ProvenanceRevision{SchemaVersion: provenanceRevisionSchemaVersion, ID: id, Revision: revision, Digest: digest}
}

func (value ProvenanceRevision) valid() bool {
	return value.SchemaVersion == provenanceRevisionSchemaVersion && validID(value.ID) && value.Revision > 0 && validDigest(value.Digest)
}

type ProvenanceDigest struct {
	SchemaVersion string `json:"schema_version"`
	ID            string `json:"id"`
	Digest        string `json:"digest"`
}

func NewProvenanceDigest(id, digest string) ProvenanceDigest {
	return ProvenanceDigest{SchemaVersion: provenanceDigestSchemaVersion, ID: id, Digest: digest}
}

func (value ProvenanceDigest) valid() bool {
	return value.SchemaVersion == provenanceDigestSchemaVersion && validID(value.ID) && validDigest(value.Digest)
}

type ProvenanceRuntime struct {
	Name           string   `json:"name"`
	Runtime        string   `json:"runtime"`
	Version        string   `json:"version"`
	Executable     string   `json:"executable"`
	Installation   string   `json:"installation"`
	AdapterVersion string   `json:"adapter_version"`
	Capabilities   []string `json:"capabilities"`
}

// PublicationProvenance is immutable proof of the exact Agent and admitted
// authority that published one exact validated Loop revision.
type PublicationProvenance struct {
	SchemaVersion    string             `json:"schema_version"`
	Loop             ProvenanceRevision `json:"loop"`
	PublisherAgent   ProvenanceRevision `json:"publisher_agent"`
	Authority        ProvenanceDigest   `json:"authority"`
	MandateID        string             `json:"mandate_id"`
	StanzaID         string             `json:"stanza_id"`
	Runtime          ProvenanceRuntime  `json:"runtime"`
	Charter          ProvenanceRevision `json:"charter"`
	ValidationDigest string             `json:"validation_digest"`
	AuthorityKind    string             `json:"authority_kind,omitempty"`
	OwnerID          string             `json:"owner_id,omitempty"`
	PrincipalID      string             `json:"principal_id,omitempty"`
	Digest           string             `json:"digest"`
}

func NewPublicationProvenance(value PublicationProvenance) (PublicationProvenance, error) {
	value.SchemaVersion = PublicationProvenanceSchemaVersion
	value.Digest = ""
	value.Runtime.Capabilities = append([]string(nil), value.Runtime.Capabilities...)
	if err := validatePublicationProvenance(value, false); err != nil {
		return PublicationProvenance{}, err
	}
	value.Digest = recordDigest(value)
	return value, nil
}

func validatePublicationProvenance(value PublicationProvenance, sealed bool) error {
	if value.SchemaVersion != PublicationProvenanceSchemaVersion || !value.Loop.valid() || !value.PublisherAgent.valid() || !value.Authority.valid() || !value.Charter.valid() {
		return errors.New("publication provenance requires exact Loop, Agent, authority, and charter references")
	}
	if !validID(value.MandateID) || !validID(value.StanzaID) || value.ValidationDigest == "" || (value.Runtime.Runtime == "" && value.Runtime.Name == "") {
		return errors.New("publication provenance is incomplete")
	}
	if sealed {
		digest := value.Digest
		value.Digest = ""
		if digest == "" || digest != recordDigest(value) {
			return errors.New("publication provenance digest does not match")
		}
	}
	return nil
}

func MarshalPublicationProvenance(value PublicationProvenance) ([]byte, error) {
	if err := validatePublicationProvenance(value, true); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func UnmarshalPublicationProvenance(data []byte) (PublicationProvenance, error) {
	value, err := decodeStrict[PublicationProvenance](data)
	if err != nil {
		return PublicationProvenance{}, err
	}
	return value, validatePublicationProvenance(value, true)
}

// LifecycleEvent is append-only lifecycle history. Current lifecycle is a
// rebuildable projection over this chain; revisions are never overwritten.
type LifecycleEvent struct {
	SchemaVersion  string             `json:"schema_version"`
	EventID        string             `json:"event_id"`
	LoopID         string             `json:"loop_id"`
	State          LifecycleState     `json:"state"`
	Revision       ProvenanceRevision `json:"revision,omitempty"`
	PreviousDigest string             `json:"previous_digest,omitempty"`
	PublisherAgent ProvenanceRevision `json:"publisher_agent"`
	Authority      ProvenanceDigest   `json:"authority"`
	MandateID      string             `json:"mandate_id"`
	StanzaID       string             `json:"stanza_id"`
	OccurredAt     time.Time          `json:"occurred_at"`
	Digest         string             `json:"digest"`
}

type LifecycleRequest struct {
	Event                  LifecycleEvent `json:"event"`
	ExpectedPreviousDigest string         `json:"expected_previous_digest,omitempty"`
}

func NewLifecycleEvent(value LifecycleEvent) (LifecycleEvent, error) {
	value.SchemaVersion = LifecycleEventSchemaVersion
	value.OccurredAt = value.OccurredAt.UTC()
	value.Digest = ""
	if err := validateLifecycleEvent(value, false); err != nil {
		return LifecycleEvent{}, err
	}
	value.Digest = recordDigest(value)
	return value, nil
}

func validateLifecycleEvent(value LifecycleEvent, sealed bool) error {
	if value.SchemaVersion != LifecycleEventSchemaVersion || !validID(value.EventID) || !validID(value.LoopID) || !value.PublisherAgent.valid() || !value.Authority.valid() || !validID(value.MandateID) || !validID(value.StanzaID) || value.OccurredAt.IsZero() {
		return errors.New("Loop lifecycle provenance is incomplete")
	}
	switch value.State {
	case LifecycleActive:
		if !value.Revision.valid() || value.Revision.ID != value.LoopID {
			return errors.New("activation requires an exact Loop revision")
		}
	case LifecycleRetired:
		if value.Revision.ID != "" {
			return errors.New("retirement cannot substitute a Loop revision")
		}
	default:
		return errors.New("lifecycle event must activate or retire")
	}
	if sealed {
		digest := value.Digest
		value.Digest = ""
		if digest == "" || digest != recordDigest(value) {
			return errors.New("lifecycle event digest does not match")
		}
	}
	return nil
}

func MarshalLifecycleEvent(value LifecycleEvent) ([]byte, error) {
	if err := validateLifecycleEvent(value, true); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func UnmarshalLifecycleEvent(data []byte) (LifecycleEvent, error) {
	value, err := decodeStrict[LifecycleEvent](data)
	if err != nil {
		return LifecycleEvent{}, err
	}
	return value, validateLifecycleEvent(value, true)
}

func recordDigest(value any) string {
	wire, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("marshal canonical Loop record: %v", err))
	}
	sum := sha256.Sum256(wire)
	return "sha256:" + hex.EncodeToString(sum[:])
}

type PublishRequest struct {
	Revision               LoopRevision          `json:"revision"`
	Validation             LoopValidationResult  `json:"validation"`
	Provenance             PublicationProvenance `json:"provenance"`
	ExpectedPreviousDigest string                `json:"expected_previous_digest,omitempty"`
	IdempotencyKey         string                `json:"idempotency_key"`
}

type PublicationDecision struct {
	Idempotent bool `json:"idempotent"`
}

// ValidatePublication enforces create-only revision publication. previous is
// the exact prior revision, while existing is the already-published record at
// the candidate revision number. Persistence must evaluate these in one atomic
// transaction; this pure function deliberately performs no storage mutation.
func ValidatePublication(request PublishRequest, previous, existing *LoopRevision) (PublicationDecision, error) {
	if !validID(request.IdempotencyKey) {
		return PublicationDecision{}, errors.New("publication idempotency key is malformed")
	}
	if issues := validateRevision(request.Revision, true); len(issues) != 0 {
		return PublicationDecision{}, errors.New("publication revision is invalid")
	}
	if err := validateLoopValidationResult(request.Validation); err != nil {
		return PublicationDecision{}, err
	}
	if request.Validation.LoopID != request.Revision.LoopID ||
		request.Validation.Revision != request.Revision.Revision ||
		request.Validation.RevisionDigest != request.Revision.Digest ||
		request.Validation.Outcome != ValidationValid {
		return PublicationDecision{}, errors.New("publication requires a valid exact-revision validation result")
	}
	if err := validatePublicationProvenance(request.Provenance, true); err != nil || request.Provenance.Loop.ID != request.Revision.LoopID || request.Provenance.Loop.Revision != request.Revision.Revision || request.Provenance.Loop.Digest != request.Revision.Digest || request.Provenance.ValidationDigest != request.Validation.Digest {
		return PublicationDecision{}, errors.New("publication requires exact immutable provenance")
	}
	if existing != nil {
		return PublicationDecision{}, errors.New("published Loop revision conflict")
	}
	if request.Revision.Revision == 1 {
		if previous != nil || request.ExpectedPreviousDigest != "" || request.Revision.PreviousDigest != "" {
			return PublicationDecision{}, errors.New("first Loop revision cannot name a predecessor")
		}
		return PublicationDecision{}, nil
	}
	if previous == nil || previous.LoopID != request.Revision.LoopID ||
		previous.Revision+1 != request.Revision.Revision || previous.Digest != request.ExpectedPreviousDigest ||
		request.Revision.PreviousDigest != previous.Digest {
		return PublicationDecision{}, errors.New("Loop revision predecessor does not match expected exact digest")
	}
	return PublicationDecision{}, nil
}

func Activate(current Lifecycle, revision LoopRevision) (Lifecycle, error) {
	if issues := validateRevision(revision, true); len(issues) != 0 || current.LoopID != revision.LoopID || current.State == LifecycleRetired {
		return Lifecycle{}, errors.New("cannot activate invalid, foreign, or retired Loop revision")
	}
	if current.State == LifecycleActive && current.ActiveRevision == revision.Revision && current.ActiveDigest == revision.Digest {
		return current, nil
	}
	return Lifecycle{LoopID: revision.LoopID, State: LifecycleActive, ActiveRevision: revision.Revision, ActiveDigest: revision.Digest}, nil
}

func Retire(current Lifecycle) (Lifecycle, error) {
	if !validID(current.LoopID) || (current.State != LifecycleDraft && current.State != LifecycleActive && current.State != LifecycleRetired) {
		return Lifecycle{}, errors.New("invalid Loop lifecycle")
	}
	if current.State == LifecycleRetired {
		return Lifecycle{}, errors.New("retired Loop lifecycle is terminal")
	}
	return Lifecycle{LoopID: current.LoopID, State: LifecycleRetired}, nil
}
