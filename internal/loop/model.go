// Package loop owns immutable, typed, reusable control-flow definitions.
// Loop definitions carry no session or runtime authority; admission must bind an
// exact published revision and validation record outside this package.
package loop

const (
	RevisionSchemaVersion   = "aegis.loop.revision.v2"
	ValidationSchemaVersion = "aegis.loop.validation.v2"
	ValidatorID             = "aegis.loop.validator"
	ValidatorVersion        = "1"

	MaxPorts       = 128
	MaxSteps       = 256
	MaxTransitions = 1024
	MaxEvidence    = 128
	MaxAttempts    = 10
	MaxTraversals  = 1000
)

type ValueType string

const (
	TypeString   ValueType = "string"
	TypeBoolean  ValueType = "boolean"
	TypeInteger  ValueType = "integer"
	TypeNumber   ValueType = "number"
	TypeObject   ValueType = "object"
	TypeArray    ValueType = "array"
	TypeArtifact ValueType = "artifact"
)

type StepKind string

const (
	StepAction   StepKind = "action"
	StepGate     StepKind = "gate"
	StepJoin     StepKind = "join"
	StepTerminal StepKind = "terminal"
)

type TerminalOutcome string

const (
	OutcomeSucceeded TerminalOutcome = "succeeded"
	OutcomeFailed    TerminalOutcome = "failed"
)

type ValidationOutcome string

const (
	ValidationValid   ValidationOutcome = "valid"
	ValidationInvalid ValidationOutcome = "invalid"
)

type LifecycleState string

const (
	LifecycleDraft   LifecycleState = "draft"
	LifecycleActive  LifecycleState = "active"
	LifecycleRetired LifecycleState = "retired"
)

// Port is a named value at a Loop or step boundary.
type Port struct {
	ID       string    `json:"id"`
	Type     ValueType `json:"type"`
	Required bool      `json:"required"`
}

// PortMapping connects one output port to one input port. At a terminal step,
// TargetPort names a Loop output rather than another step input.
type PortMapping struct {
	SourcePort string `json:"source_port"`
	TargetPort string `json:"target_port"`
}

type RetryPolicy struct {
	MaxAttempts uint16 `json:"max_attempts"`
}

// GateDefinition declares an exclusive branch. Branch conditions are opaque
// policy labels evaluated by a future authorized worker, never expressions.
type GateDefinition struct {
	Mode string `json:"mode"`
}

type TerminalDefinition struct {
	Outcome        TerminalOutcome `json:"outcome"`
	OutputMappings []PortMapping   `json:"output_mappings"`
}

type EvidenceClaim struct {
	Claim          string `json:"claim"`
	MediaType      string `json:"media_type"`
	ExpectedDigest string `json:"expected_digest"`
	VerifierID     string `json:"verifier_id"`
	PolicyVersion  string `json:"policy_version"`
}

type EvidenceRequirement struct {
	Claim          string `json:"claim"`
	ProducerStepID string `json:"producer_step_id"`
}

type Step struct {
	ID             string              `json:"id"`
	Kind           StepKind            `json:"kind"`
	InputPorts     []Port              `json:"input_ports"`
	OutputPorts    []Port              `json:"output_ports"`
	Retry          RetryPolicy         `json:"retry"`
	Gate           *GateDefinition     `json:"gate,omitempty"`
	Terminal       *TerminalDefinition `json:"terminal,omitempty"`
	EvidenceClaims []EvidenceClaim     `json:"evidence_claims"`
}

type Transition struct {
	ID            string        `json:"id"`
	FromStepID    string        `json:"from_step_id"`
	ToStepID      string        `json:"to_step_id"`
	Condition     string        `json:"condition,omitempty"`
	Mappings      []PortMapping `json:"mappings"`
	MaxTraversals uint16        `json:"max_traversals,omitempty"`
}

type ValidatorSpec struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

// LoopRevision is a create-only definition. Digest covers its canonical form
// with Digest omitted, avoiding a self-referential hash.
type LoopRevision struct {
	SchemaVersion    string                `json:"schema_version"`
	LoopID           string                `json:"loop_id"`
	Revision         uint64                `json:"revision"`
	PreviousDigest   string                `json:"previous_digest,omitempty"`
	Inputs           []Port                `json:"inputs"`
	Outputs          []Port                `json:"outputs"`
	EntryStepID      string                `json:"entry_step_id"`
	Steps            []Step                `json:"steps"`
	Transitions      []Transition          `json:"transitions"`
	RequiredEvidence []EvidenceRequirement `json:"required_evidence"`
	Validator        ValidatorSpec         `json:"validator"`
	Digest           string                `json:"digest"`
}

type ValidationIssue struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

// LoopValidationResult is immutable historical truth for one exact revision. A
// later validator run creates another result; it never rewrites this record.
type LoopValidationResult struct {
	SchemaVersion  string            `json:"schema_version"`
	LoopID         string            `json:"loop_id"`
	Revision       uint64            `json:"revision"`
	RevisionDigest string            `json:"revision_digest"`
	Validator      ValidatorSpec     `json:"validator"`
	Outcome        ValidationOutcome `json:"outcome"`
	Issues         []ValidationIssue `json:"issues"`
	Digest         string            `json:"digest"`
}

// Lifecycle records mutable selection separately from immutable revisions.
type Lifecycle struct {
	LoopID         string         `json:"loop_id"`
	State          LifecycleState `json:"state"`
	ActiveRevision uint64         `json:"active_revision,omitempty"`
	ActiveDigest   string         `json:"active_digest,omitempty"`
}
