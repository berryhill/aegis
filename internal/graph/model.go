// Package graph owns immutable, validated coordination definitions.
//
// Graph records bind exact Agent and Loop revisions but carry no authority,
// queue state, runtime state, artifacts, or disposition. Callers must resolve
// and revalidate every exact reference at admission and effect boundaries.
package graph

import (
	"encoding/json"

	"github.com/berryhill/aegis/internal/reference"
)

const (
	RevisionSchemaVersion   = "aegis.graph.revision.v1"
	ValidationSchemaVersion = "aegis.graph.validation.v1"
	SnapshotSchemaVersion   = "aegis.graph.run-snapshot.v1"
	ValidatorID             = "aegis.graph.validator"
	ValidatorVersion        = "1"

	MaxPorts              = 128
	MaxNodes              = 128
	MaxDependencies       = 512
	MaxMappings           = 1024
	MaxAdmissionRules     = 128
	MaxNormalizedInputs   = 128
	MaxResolvedReferences = 256
	MaxInputValueBytes    = 1 << 20
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

type Port struct {
	ID       string    `json:"id"`
	Type     ValueType `json:"type"`
	Required bool      `json:"required"`
}

// Node binds one coordination position to exact executable-participant and
// reusable-control-flow revisions. Ports are interface assertions that an
// admission resolver must compare with the pinned Loop revision.
type Node struct {
	ID          string                `json:"id"`
	Participant reference.RevisionRef `json:"participant"`
	Loop        reference.RevisionRef `json:"loop"`
	Inputs      []Port                `json:"inputs"`
	Outputs     []Port                `json:"outputs"`
}

type InputMapping struct {
	GraphInput string `json:"graph_input"`
	ToNodeID   string `json:"to_node_id"`
	ToPort     string `json:"to_port"`
}

type PortMapping struct {
	FromPort string `json:"from_port"`
	ToPort   string `json:"to_port"`
}

// Dependency is a typed, directed edge between two distinct Graph nodes.
type Dependency struct {
	ID         string        `json:"id"`
	FromNodeID string        `json:"from_node_id"`
	ToNodeID   string        `json:"to_node_id"`
	Mappings   []PortMapping `json:"mappings"`
}

type OutputMapping struct {
	FromNodeID  string `json:"from_node_id"`
	FromPort    string `json:"from_port"`
	GraphOutput string `json:"graph_output"`
}

// AdmissionRule is an opaque, bounded policy reference. It is data for an
// authorized admission service and is never evaluated from model narration.
type AdmissionRule struct {
	ID        string              `json:"id"`
	PolicyRef reference.DigestRef `json:"policy_ref"`
}

type ValidatorSpec struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

// GraphRevision is create-only. Digest covers its canonical representation
// with Digest omitted.
type GraphRevision struct {
	SchemaVersion  string          `json:"schema_version"`
	GraphID        string          `json:"graph_id"`
	Revision       uint64          `json:"revision"`
	PreviousDigest string          `json:"previous_digest,omitempty"`
	Inputs         []Port          `json:"inputs"`
	Outputs        []Port          `json:"outputs"`
	Nodes          []Node          `json:"nodes"`
	InputMappings  []InputMapping  `json:"input_mappings"`
	Dependencies   []Dependency    `json:"dependencies"`
	OutputMappings []OutputMapping `json:"output_mappings"`
	AdmissionRules []AdmissionRule `json:"admission_rules"`
	Validator      ValidatorSpec   `json:"validator"`
	Digest         string          `json:"digest"`
}

type ValidationIssue struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

// GraphValidationResult is immutable historical truth for one exact revision.
type GraphValidationResult struct {
	SchemaVersion  string            `json:"schema_version"`
	GraphID        string            `json:"graph_id"`
	Revision       uint64            `json:"revision"`
	RevisionDigest string            `json:"revision_digest"`
	Validator      ValidatorSpec     `json:"validator"`
	Outcome        ValidationOutcome `json:"outcome"`
	Issues         []ValidationIssue `json:"issues"`
	Digest         string            `json:"digest"`
}

// Lifecycle keeps mutable selection separate from immutable definitions.
type Lifecycle struct {
	GraphID        string         `json:"graph_id"`
	State          LifecycleState `json:"state"`
	ActiveRevision uint64         `json:"active_revision,omitempty"`
	ActiveDigest   string         `json:"active_digest,omitempty"`
}

// NormalizedInput contains canonical JSON for one typed Graph input.
type NormalizedInput struct {
	PortID string          `json:"port_id"`
	Type   ValueType       `json:"type"`
	Value  json.RawMessage `json:"value"`
}

// GraphRunSnapshot is a create-only composition record. It preserves resolved
// definition truth for a submission but cannot authorize or enqueue work.
type GraphRunSnapshot struct {
	SchemaVersion string                  `json:"schema_version"`
	SnapshotID    string                  `json:"snapshot_id"`
	Graph         reference.RevisionRef   `json:"graph"`
	Inputs        []NormalizedInput       `json:"inputs"`
	Participants  []reference.RevisionRef `json:"participants"`
	Loops         []reference.RevisionRef `json:"loops"`
	Digest        string                  `json:"digest"`
}
