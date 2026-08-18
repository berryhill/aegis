// Package fleet defines the narrow durable boundary for fleet-control facts.
// Domain records remain owned by registry, loop, graph, and core; implementations
// must not reinterpret their canonical codecs or grant runtime authority.
package fleet

import (
	"context"
	"errors"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/disposition"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	queue "github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/registry"
)

var (
	ErrNotFound = errors.New("fleet record not found")
	ErrConflict = errors.New("fleet immutable record conflict")
	ErrCorrupt  = errors.New("fleet record is corrupt")
	ErrClosed   = errors.New("fleet repository is closed")
)

// AuditFact is metadata supplied by an authenticated application boundary.
// The repository assigns chain position, event identity, occurrence time, and
// digest in the same transaction as the consequential fleet mutation.
type AuditFact struct {
	Event core.AuditEvent
}

// AcceptedSubmission is one all-or-nothing admission mutation.
type AcceptedSubmission struct {
	Snapshot          graph.GraphRunSnapshot `json:"snapshot"`
	Submission        queue.Submission       `json:"submission"`
	QueueItem         queue.Item             `json:"queue_item"`
	GraphRun          execution.GraphRun     `json:"graph_run"`
	InitialTransition queue.QueueTransition  `json:"initial_transition"`
}

// Completion is one all-or-nothing evidence-gated terminal mutation.
type Completion struct {
	Claim       queue.Claim                    `json:"claim"`
	Artifact    *evidence.RuntimeArtifact      `json:"artifact,omitempty"`
	Receipts    []evidence.VerificationReceipt `json:"receipts"`
	Disposition disposition.Record             `json:"disposition"`
	Transition  queue.QueueTransition          `json:"transition"`
}

type RetryMutation struct {
	Retry      queue.Retry
	Transition queue.QueueTransition
}

type CancellationMutation struct {
	Cancellation queue.Cancellation
	Transition   queue.QueueTransition
}

// Repository exposes create-only definition persistence. It deliberately has
// no generic put, update, delete, latest-substitution, or engine transaction API.
type Repository interface {
	RegisterAgent(context.Context, registry.AgentRegistration, registry.AgentRevision, AuditFact) (bool, error)
	PublishAgentRevision(context.Context, registry.AgentRevision, AuditFact) error
	GetAgentRegistration(context.Context, string) (registry.AgentRegistration, error)
	GetAgentRegistrationBySource(context.Context, registry.FleetSource) (registry.AgentRegistration, error)
	GetAgentRevision(context.Context, string, uint64) (registry.AgentRevision, error)
	LatestAgentRevision(context.Context, string) (registry.AgentRevision, error)
	ListAgentRegistrations(context.Context) ([]registry.AgentRegistration, error)
	ListAgentRevisions(context.Context, string) ([]registry.AgentRevision, error)

	PublishLoop(context.Context, loop.PublishRequest, AuditFact) (loop.PublicationDecision, error)
	GetLoopRevision(context.Context, string, uint64) (loop.LoopRevision, error)
	GetLoopValidation(context.Context, string, uint64, string) (loop.LoopValidationResult, error)
	GetLoopPublicationProvenance(context.Context, string, uint64) (loop.PublicationProvenance, error)
	ListLoopRevisions(context.Context) ([]loop.LoopRevision, error)
	ListLoopValidations(context.Context) ([]loop.LoopValidationResult, error)
	ListLoopPublicationProvenance(context.Context) ([]loop.PublicationProvenance, error)
	AppendLoopLifecycle(context.Context, loop.LifecycleRequest, AuditFact) (loop.LifecycleEvent, bool, error)
	ListLoopLifecycleEvents(context.Context) ([]loop.LifecycleEvent, error)

	PublishGraph(context.Context, graph.PublishRequest, AuditFact) (graph.PublicationDecision, error)
	GetGraphRevision(context.Context, string, uint64) (graph.GraphRevision, error)
	GetGraphValidation(context.Context, string, uint64, string) (graph.GraphValidationResult, error)
	ListGraphRevisions(context.Context) ([]graph.GraphRevision, error)
	ListGraphValidations(context.Context) ([]graph.GraphValidationResult, error)
	GetGraphLifecycle(context.Context, string) (graph.Lifecycle, error)
	ListGraphLifecycles(context.Context) ([]graph.Lifecycle, error)
	CreateGraphRunSnapshot(context.Context, graph.GraphRunSnapshot, AuditFact) (bool, error)
	GetGraphRunSnapshot(context.Context, string) (graph.GraphRunSnapshot, error)

	AcceptSubmission(context.Context, AcceptedSubmission, AuditFact) (bool, error)
	RejectSubmission(context.Context, queue.Rejection, AuditFact) (bool, error)
	GetSubmission(context.Context, string) (queue.Submission, error)
	GetRejection(context.Context, string) (queue.Rejection, error)
	ListSubmissions(context.Context) ([]queue.Submission, error)
	ListRejections(context.Context) ([]queue.Rejection, error)
	GetQueueItem(context.Context, string) (queue.Item, error)
	ListQueueItems(context.Context) ([]queue.Item, error)
	GetGraphRun(context.Context, string) (execution.GraphRun, error)
	ListGraphRuns(context.Context) ([]execution.GraphRun, error)
	CreateLoopExecution(context.Context, execution.LoopExecution, AuditFact) (bool, error)
	GetLoopExecution(context.Context, string) (execution.LoopExecution, error)
	ListLoopExecutions(context.Context) ([]execution.LoopExecution, error)
	ClaimQueueItem(context.Context, queue.Claim, execution.Attempt, queue.QueueTransition, AuditFact) error
	RetryQueueItem(context.Context, RetryMutation, AuditFact) error
	CancelQueueItem(context.Context, CancellationMutation, AuditFact) error
	GetClaim(context.Context, string) (queue.Claim, error)
	GetQueueProjection(context.Context, string) (queue.Projection, error)
	GetAttempt(context.Context, string) (execution.Attempt, error)
	ListAttempts(context.Context) ([]execution.Attempt, error)
	CompleteQueueItem(context.Context, Completion, AuditFact) error
	GetRuntimeArtifact(context.Context, string) (evidence.RuntimeArtifact, error)
	GetVerificationReceipt(context.Context, string) (evidence.VerificationReceipt, error)
	GetDisposition(context.Context, string) (disposition.Record, error)

	AuditEvents(context.Context) ([]core.AuditEvent, error)
	Close() error
}
