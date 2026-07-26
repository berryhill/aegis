// Package plumbing defines Aegis's participant-centric plumbing aggregate.
//
// The package is deliberately transport- and storage-neutral. Adapters may map
// runtime records into these types, but identity, stanza selection, mandates,
// authority, verification, and terminal disposition remain Aegis-owned facts.
package plumbing

import "time"

const SchemaVersion = "aegis.dev/plumbing/v1alpha1"

type ProvenanceProducer string

const (
	ProducerControlPlane   ProvenanceProducer = "aegis-control-plane"
	ProducerRuntimeAdapter ProvenanceProducer = "aegis-runtime-adapter"
	ProducerVerifier       ProvenanceProducer = "aegis-verifier"
)

type VerifierID string

const (
	VerifierArtifact VerifierID = "aegis-artifact-verifier"
)

type AttemptKind string

const (
	AttemptDispatch AttemptKind = "dispatch"
	AttemptSession  AttemptKind = "session"
)

type AttemptState string

const (
	AttemptRequested AttemptState = "requested"
	AttemptStarted   AttemptState = "started"
	AttemptSucceeded AttemptState = "succeeded"
	AttemptFailed    AttemptState = "failed"
	AttemptDenied    AttemptState = "denied"
	AttemptCancelled AttemptState = "cancelled"
	AttemptExpired   AttemptState = "expired"
)

type DeliveryState string

const (
	DeliveryPending   DeliveryState = "pending"
	DeliveryDelivered DeliveryState = "delivered"
	DeliveryFailed    DeliveryState = "failed"
	DeliveryDenied    DeliveryState = "denied"
	DeliveryCancelled DeliveryState = "cancelled"
)

type VerificationOutcome string

const (
	VerificationPassed VerificationOutcome = "passed"
	VerificationFailed VerificationOutcome = "failed"
)

type DispositionState string

const (
	DispositionSucceeded DispositionState = "succeeded"
	DispositionFailed    DispositionState = "failed"
	DispositionDenied    DispositionState = "denied"
	DispositionCancelled DispositionState = "cancelled"
	DispositionExpired   DispositionState = "expired"
)

// Provenance identifies the Aegis-controlled producer of a fact. SourceRef is
// an opaque adapter or evidence reference, never model narration.
type Provenance struct {
	OwnerID    string             `json:"owner_id"`
	Producer   ProvenanceProducer `json:"producer"`
	SourceRef  string             `json:"source_ref"`
	RecordedAt time.Time          `json:"recorded_at"`
}

type Authentication struct {
	EvidenceID      string    `json:"evidence_id"`
	Issuer          string    `json:"issuer"`
	Method          string    `json:"method"`
	AuthenticatedAt time.Time `json:"authenticated_at"`
	ExpiresAt       time.Time `json:"expires_at"`
	ClaimsDigest    string    `json:"claims_digest"`
}

type Participant struct {
	ID             string         `json:"id"`
	Kind           string         `json:"kind"`
	PrincipalID    string         `json:"principal_id,omitempty"`
	Authentication Authentication `json:"authentication"`
	Provenance     Provenance     `json:"provenance"`
}

// IngressFact contains trusted contact and channel observations established
// outside the model. EndpointRef is opaque and must not contain a secret.
type IngressFact struct {
	ID            string     `json:"id"`
	ParticipantID string     `json:"participant_id"`
	ContactID     string     `json:"contact_id"`
	ChannelID     string     `json:"channel_id"`
	ChannelKind   string     `json:"channel_kind"`
	EndpointRef   string     `json:"endpoint_ref"`
	ObservedAt    time.Time  `json:"observed_at"`
	Provenance    Provenance `json:"provenance"`
}

// StanzaDecision is authoritative only when produced by Aegis. Allowed is
// valid only for exactly one match and one selected stanza.
type StanzaDecision struct {
	ID               string     `json:"id"`
	ParticipantID    string     `json:"participant_id"`
	IngressFactID    string     `json:"ingress_fact_id"`
	AgentID          string     `json:"agent_id"`
	CharterRevision  uint64     `json:"charter_revision"`
	CharterDigest    string     `json:"charter_digest"`
	Allowed          bool       `json:"allowed"`
	MatchingCount    uint32     `json:"matching_count"`
	SelectedStanzaID string     `json:"selected_stanza_id,omitempty"`
	Reason           string     `json:"reason"`
	DecidedAt        time.Time  `json:"decided_at"`
	Provenance       Provenance `json:"provenance"`
}

// AuthorityContext is the immutable per-session authority projection. Its
// digest binds every field except Digest itself. Revocation and expiry are
// separate live facts; callers must never rewrite this record in place.
type AuthorityContext struct {
	ID               string     `json:"id"`
	MandateID        string     `json:"mandate_id"`
	DecisionID       string     `json:"decision_id"`
	ParticipantID    string     `json:"participant_id"`
	AgentID          string     `json:"agent_id"`
	StanzaID         string     `json:"stanza_id"`
	CharterRevision  uint64     `json:"charter_revision"`
	CharterDigest    string     `json:"charter_digest"`
	Runtime          string     `json:"runtime"`
	RuntimeVersion   string     `json:"runtime_version"`
	Capabilities     []string   `json:"capabilities"`
	Tools            []string   `json:"tools"`
	MemoryScopes     []string   `json:"memory_scopes"`
	CredentialScopes []string   `json:"credential_scopes"`
	IssuedAt         time.Time  `json:"issued_at"`
	ExpiresAt        time.Time  `json:"expires_at"`
	Digest           string     `json:"digest"`
	Provenance       Provenance `json:"provenance"`
}

// AuthorityRevocation is append-only and does not mutate AuthorityContext.
type AuthorityRevocation struct {
	ID                 string     `json:"id"`
	AuthorityContextID string     `json:"authority_context_id"`
	Reason             string     `json:"reason"`
	RevokedAt          time.Time  `json:"revoked_at"`
	Provenance         Provenance `json:"provenance"`
}

type Attempt struct {
	ID                 string       `json:"id"`
	Kind               AttemptKind  `json:"kind"`
	ParentAttemptID    string       `json:"parent_attempt_id,omitempty"`
	AuthorityContextID string       `json:"authority_context_id"`
	RuntimeAttemptID   string       `json:"runtime_attempt_id,omitempty"`
	State              AttemptState `json:"state"`
	RequestedAt        time.Time    `json:"requested_at"`
	StartedAt          *time.Time   `json:"started_at,omitempty"`
	FinishedAt         *time.Time   `json:"finished_at,omitempty"`
	Reason             string       `json:"reason,omitempty"`
	Provenance         Provenance   `json:"provenance"`
}

// Operation names a closed, versioned Aegis operation. ParametersDigest binds
// canonical typed parameters while ParametersRef points at controlled storage.
type Operation struct {
	ID                 string     `json:"id"`
	AuthorityContextID string     `json:"authority_context_id"`
	SessionAttemptID   string     `json:"session_attempt_id"`
	Type               string     `json:"type"`
	SchemaVersion      string     `json:"schema_version"`
	ParametersDigest   string     `json:"parameters_digest"`
	ParametersRef      string     `json:"parameters_ref,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	Provenance         Provenance `json:"provenance"`
}

type Request struct {
	ID              string     `json:"id"`
	OperationID     string     `json:"operation_id"`
	ParentRequestID string     `json:"parent_request_id,omitempty"`
	PayloadDigest   string     `json:"payload_digest"`
	PayloadRef      string     `json:"payload_ref,omitempty"`
	Deadline        time.Time  `json:"deadline"`
	CreatedAt       time.Time  `json:"created_at"`
	Provenance      Provenance `json:"provenance"`
}

type Artifact struct {
	ID         string     `json:"id"`
	RequestID  string     `json:"request_id"`
	OwnerID    string     `json:"owner_id"`
	Kind       string     `json:"kind"`
	Revision   uint64     `json:"revision"`
	Digest     string     `json:"digest"`
	ContentRef string     `json:"content_ref"`
	MediaType  string     `json:"media_type"`
	CreatedAt  time.Time  `json:"created_at"`
	Provenance Provenance `json:"provenance"`
}

type Delivery struct {
	ID                 string        `json:"id"`
	RequestID          string        `json:"request_id"`
	ArtifactID         string        `json:"artifact_id"`
	AuthorityContextID string        `json:"authority_context_id"`
	Destination        string        `json:"destination"`
	State              DeliveryState `json:"state"`
	AttemptedAt        time.Time     `json:"attempted_at"`
	FinishedAt         *time.Time    `json:"finished_at,omitempty"`
	Reason             string        `json:"reason,omitempty"`
	Provenance         Provenance    `json:"provenance"`
}

type VerificationEvidence struct {
	ID          string              `json:"id"`
	SubjectKind string              `json:"subject_kind"`
	SubjectID   string              `json:"subject_id"`
	Verifier    VerifierID          `json:"verifier"`
	Outcome     VerificationOutcome `json:"outcome"`
	Digest      string              `json:"digest"`
	EvidenceRef string              `json:"evidence_ref"`
	ObservedAt  time.Time           `json:"observed_at"`
	Provenance  Provenance          `json:"provenance"`
}

type TerminalDisposition struct {
	ID          string           `json:"id"`
	State       DispositionState `json:"state"`
	Reason      string           `json:"reason"`
	EvidenceIDs []string         `json:"evidence_ids"`
	DecidedAt   time.Time        `json:"decided_at"`
	Provenance  Provenance       `json:"provenance"`
}

// Aggregate is one causal lifecycle. It owns references and metadata, not
// arbitrary payload bodies or secrets. Revision is monotonic in persistence.
type Aggregate struct {
	SchemaVersion string                 `json:"schema_version"`
	ID            string                 `json:"id"`
	Revision      uint64                 `json:"revision"`
	OwnerID       string                 `json:"owner_id"`
	Participant   Participant            `json:"participant"`
	Ingress       IngressFact            `json:"ingress"`
	Decision      StanzaDecision         `json:"decision"`
	Authority     *AuthorityContext      `json:"authority,omitempty"`
	Revocations   []AuthorityRevocation  `json:"revocations,omitempty"`
	Attempts      []Attempt              `json:"attempts,omitempty"`
	Operations    []Operation            `json:"operations,omitempty"`
	Requests      []Request              `json:"requests,omitempty"`
	Artifacts     []Artifact             `json:"artifacts,omitempty"`
	Deliveries    []Delivery             `json:"deliveries,omitempty"`
	Evidence      []VerificationEvidence `json:"evidence,omitempty"`
	Disposition   *TerminalDisposition   `json:"disposition,omitempty"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
}
