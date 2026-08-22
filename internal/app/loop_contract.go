package app

import (
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
)

// Loop command adapters use these application-layer aliases so transports do
// not bypass the app boundary to depend directly on the Loop domain package.
type (
	LoopCandidate           = loop.LoopRevision
	LoopValidation          = loop.LoopValidationResult
	LoopPort                = loop.Port
	LoopValueType           = loop.ValueType
	LoopStep                = loop.Step
	LoopStepKind            = loop.StepKind
	LoopRetryPolicy         = loop.RetryPolicy
	LoopGateDefinition      = loop.GateDefinition
	LoopTerminalDefinition  = loop.TerminalDefinition
	LoopTerminalOutcome     = loop.TerminalOutcome
	LoopPortMapping         = loop.PortMapping
	LoopEvidenceClaim       = loop.EvidenceClaim
	LoopTransition          = loop.Transition
	LoopEvidenceRequirement = loop.EvidenceRequirement
	LoopLifecycleState      = loop.LifecycleState
	LoopValidatorSpec       = loop.ValidatorSpec
)

const (
	LoopMaxPorts       = loop.MaxPorts
	LoopMaxSteps       = loop.MaxSteps
	LoopMaxEvidence    = loop.MaxEvidence
	LoopMaxTransitions = loop.MaxTransitions

	LoopStepGate     = loop.StepGate
	LoopStepTerminal = loop.StepTerminal

	LoopLifecycleActive  = loop.LifecycleActive
	LoopLifecycleRetired = loop.LifecycleRetired
	LoopValidationValid  = loop.ValidationValid
)

func NewLoopRevision(candidate LoopCandidate) (LoopCandidate, LoopValidation, error) {
	return loop.NewRevision(candidate)
}

func RevisionReference(id string, revision uint64, digest string) reference.RevisionRef {
	return reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: id, Revision: revision, Digest: digest}
}
