// Package execution owns dispatch and runtime-turn state. It intentionally has
// no cross-domain aggregate and accepts authority only through immutable IDs,
// digests, and a fresh admission decision.
package execution

import (
	"context"
	"errors"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/reference"
)

const (
	GraphRunSchemaVersion      = "aegis.execution.graph-run.v1"
	LoopExecutionSchemaVersion = "aegis.execution.loop.v1"
	AttemptSchemaVersion       = "aegis.execution.attempt.v1"
)

type State string

const (
	StateRequested State = "requested"
	StateStarted   State = "started"
	StateSucceeded State = "succeeded"
	StateFailed    State = "failed"
	StateDenied    State = "denied"
	StateCancelled State = "cancelled"
	StateExpired   State = "expired"
)

// GraphRun is the stable parent execution identity for one accepted queue item.
type GraphRun struct {
	SchemaVersion string              `json:"schema_version"`
	GraphRunID    string              `json:"graph_run_id"`
	QueueItem     reference.DigestRef `json:"queue_item"`
	Snapshot      reference.DigestRef `json:"snapshot"`
	Authority     reference.DigestRef `json:"authority"`
	State         State               `json:"state"`
	CreatedAt     time.Time           `json:"created_at"`
	Digest        string              `json:"digest"`
}

// LoopExecution preserves child causality for one exact Graph node and Loop.
type LoopExecution struct {
	SchemaVersion   string                `json:"schema_version"`
	LoopExecutionID string                `json:"loop_execution_id"`
	GraphRunID      string                `json:"graph_run_id"`
	GraphNodeID     string                `json:"graph_node_id"`
	Loop            reference.RevisionRef `json:"loop"`
	Participant     reference.RevisionRef `json:"participant"`
	State           State                 `json:"state"`
	CreatedAt       time.Time             `json:"created_at"`
	Digest          string                `json:"digest"`
}

// Attempt is one bounded try; every retry receives a new identity and number.
type Attempt struct {
	SchemaVersion   string              `json:"schema_version"`
	AttemptID       string              `json:"attempt_id"`
	GraphRunID      string              `json:"graph_run_id"`
	LoopExecutionID string              `json:"loop_execution_id"`
	QueueItem       reference.DigestRef `json:"queue_item"`
	ClaimID         string              `json:"claim_id"`
	AttemptNumber   uint32              `json:"attempt_number"`
	State           State               `json:"state"`
	CreatedAt       time.Time           `json:"created_at"`
	Digest          string              `json:"digest"`
}

func CanTransition(from, to State) bool {
	switch from {
	case StateRequested:
		return to == StateStarted || to == StateDenied || to == StateCancelled || to == StateExpired
	case StateStarted:
		return to == StateSucceeded || to == StateFailed || to == StateDenied || to == StateCancelled || to == StateExpired
	default:
		return false
	}
}

// Dispatch is the controller-owned parent admission for a runtime session.
type Dispatch struct {
	ID                 string     `json:"id"`
	AuthorityContextID string     `json:"authority_context_id"`
	State              State      `json:"state"`
	RequestedAt        time.Time  `json:"requested_at"`
	StartedAt          *time.Time `json:"started_at,omitempty"`
	FinishedAt         *time.Time `json:"finished_at,omitempty"`
	Reason             string     `json:"reason,omitempty"`
}

// Turn is runtime evidence, not authorization and not a completion claim.
type Turn struct {
	ID                 string     `json:"id"`
	DispatchID         string     `json:"dispatch_id"`
	AuthorityContextID string     `json:"authority_context_id"`
	RuntimeSessionID   string     `json:"runtime_session_id,omitempty"`
	State              State      `json:"state"`
	RequestedAt        time.Time  `json:"requested_at"`
	StartedAt          *time.Time `json:"started_at,omitempty"`
	FinishedAt         *time.Time `json:"finished_at,omitempty"`
	Reason             string     `json:"reason,omitempty"`
}

func ValidateDispatch(dispatch Dispatch, context core.AuthorityContext) error {
	if dispatch.ID == "" || dispatch.AuthorityContextID != context.ID || dispatch.State != StateSucceeded ||
		dispatch.StartedAt == nil || dispatch.FinishedAt == nil || dispatch.RequestedAt.After(*dispatch.StartedAt) ||
		dispatch.StartedAt.After(*dispatch.FinishedAt) || !dispatch.FinishedAt.Before(context.ExpiresAt) {
		return errors.New("successful same-authority parent dispatch is required")
	}
	return nil
}

// LaunchContract is the complete immutable input a runtime adapter may inspect.
// It contains no graph, historical projection, policy selector, or model-created
// operation.
type LaunchContract struct {
	OwnerID          string
	Mandate          core.Mandate
	AuthorityContext core.AuthorityContext
	ParentDispatch   Dispatch
}

// AdmissionDecision is produced by an authoritative source immediately before
// a runtime effect. Allowed is valid only for this context digest and check time.
type AdmissionDecision struct {
	Allowed                bool
	AuthorityContextID     string
	AuthorityContextDigest string
	CheckedAt              time.Time
	Reason                 string
}

type AdmissionChecker interface {
	CheckRuntimeAdmission(context.Context, LaunchContract, time.Time) (AdmissionDecision, error)
}

func ValidateAdmission(contract LaunchContract, decision AdmissionDecision, at time.Time) error {
	if err := core.ValidateAuthorityContext(contract.AuthorityContext, contract.Mandate); err != nil {
		return err
	}
	if err := ValidateDispatch(contract.ParentDispatch, contract.AuthorityContext); err != nil {
		return err
	}
	if !decision.Allowed || decision.AuthorityContextID != contract.AuthorityContext.ID ||
		decision.AuthorityContextDigest != contract.AuthorityContext.Digest || decision.CheckedAt.After(at) ||
		at.Sub(decision.CheckedAt) > time.Second || !at.Before(contract.AuthorityContext.ExpiresAt) {
		return errors.New("fresh exact-context runtime admission is required")
	}
	return nil
}
