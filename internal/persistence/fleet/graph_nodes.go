package fleet

import (
	"context"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/queue"
	"time"
)

// GraphNodeHandoff identifies the exact verified predecessor, not merely equal bytes.
type GraphNodeHandoff struct {
	NodeID        string `json:"node_id"`
	ExecutionID   string `json:"execution_id"`
	AttemptID     string `json:"attempt_id"`
	ArtifactID    string `json:"artifact_id"`
	ContentDigest string `json:"content_digest"`
	DispositionID string `json:"disposition_id"`
	EdgeID        string `json:"edge_id"`
}

// GraphNodeAdmission is immutable. One outer lease covers both child attempts;
// MaxImplementationPasses is a cap, not permission to reset a Loop cursor.
type GraphNodeAdmission struct {
	SchemaVersion           string                  `json:"schema_version"`
	Claim                   queue.Claim             `json:"claim"`
	Execution               execution.LoopExecution `json:"execution"`
	Attempt                 execution.Attempt       `json:"attempt"`
	MaxImplementationPasses uint32                  `json:"max_implementation_passes"`
	Input                   *GraphNodeHandoff       `json:"input,omitempty"`
	AdmittedAt              time.Time               `json:"admitted_at"`
}

type GraphNodeCheckpoint struct {
	Admission  GraphNodeAdmission `json:"admission"`
	Completion Completion         `json:"completion"`
}

// GraphNodeRepository is additive: historical Repository implementations and
// single-node codecs retain their existing contract. Callers freshly admit
// runtime authority at each effect; these records grant no authority themselves.
type GraphNodeRepository interface {
	AdmitGraphNode(context.Context, GraphNodeAdmission, AuditFact) error
	CheckpointGraphNode(context.Context, GraphNodeCheckpoint, AuditFact, EvidenceReader) error
	GetGraphNode(context.Context, string, string) (GraphNodeAdmission, error)
	GetGraphNodeCheckpoint(context.Context, string, string) (GraphNodeCheckpoint, error)
	CompleteGraph(context.Context, Completion, AuditFact, EvidenceReader) error
}

const GraphNodeSchemaVersion = "aegis.execution.graph-node.v1"
