package orchestration

import (
	"context"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
)

// SubmitLoopRequest uses the existing immutable Queue envelope and intent bounds.
// Graph must be absent. Workspace admission does not authorize runtime effects.
type SubmitLoopRequest struct {
	SubmitGraphRequest
	Participant reference.RevisionRef `json:"participant"`
	Loop        reference.RevisionRef `json:"loop"`
	NodeID      string                `json:"node_id"`
}

func (service *FleetService) rejectPreparedRun(ctx context.Context, request SubmitGraphRequest, code, reason string) (SubmissionDecision, error) {
	rejection, err := queue.NewRejection(queue.Rejection{RejectionID: request.RejectionID, SubmissionID: request.SubmissionID, IdempotencyKey: request.IdempotencyKey, ReasonCode: code, Reason: reason, RejectedAt: service.now()})
	if err != nil {
		return SubmissionDecision{}, err
	}
	created, err := service.repository.RejectSubmission(ctx, rejection, service.auditFact("fleet.submission.rejected", request.Subject, reason, "", "", ""))
	return SubmissionDecision{Rejection: &rejection, Created: created}, err
}

// PrepareLoopRun resolves the exact registered participant and published Loop
// without consulting or publishing a Graph. The existing AcceptSubmission port
// atomically persists the snapshot, submission, run, item and initial transition.
func (service *FleetService) PrepareLoopRun(ctx context.Context, request SubmitLoopRequest) (SubmissionDecision, error) {
	deny := func(code, reason string) (SubmissionDecision, error) {
		return service.rejectPreparedRun(ctx, request.SubmitGraphRequest, code, reason)
	}
	if request.Graph != (reference.RevisionRef{}) || request.Participant.Validate() != nil || request.Loop.Validate() != nil {
		return deny("direct_reference_invalid", "exact direct participant and Loop references and no Graph are required")
	}
	// Runtime-session submission is deliberately not inferred from Graph approval.
	if request.Workspace == nil {
		return deny("direct_permission_required", "direct Loop submission requires exact registered-Agent workspace authority")
	}
	workspace, participant, err := service.resolveWorkspace(ctx, request.Subject, request.Authority, request.Workspace, WorkspaceSubmitGraphs)
	if err != nil || workspace.Agent != request.Participant {
		return deny("workspace_denied", "exact participant workspace authority denied")
	}
	revision, err := service.repository.GetLoopRevision(ctx, request.Loop.ID, request.Loop.Revision)
	if err != nil || revision.LoopID != request.Loop.ID || revision.Revision != request.Loop.Revision || revision.Digest != request.Loop.Digest {
		return deny("loop_unavailable", "exact published Loop revision unavailable")
	}
	lifecycle, err := service.currentLoopLifecycle(ctx, request.Loop.ID)
	if err != nil || lifecycle.State != loop.LifecycleActive || lifecycle.ActiveRevision != request.Loop.Revision || lifecycle.ActiveDigest != request.Loop.Digest {
		return deny("loop_inactive", "exact Loop revision is not active")
	}
	if participant.AgentID != request.Participant.ID || participant.Revision != request.Participant.Revision || participant.Digest != request.Participant.Digest {
		return deny("participant_unavailable", "exact registered participant unavailable")
	}
	node := graph.Node{ID: request.NodeID, Participant: request.Participant, Loop: request.Loop}
	for _, p := range revision.Inputs {
		node.Inputs = append(node.Inputs, graph.Port{ID: p.ID, Type: graph.ValueType(p.Type), Required: p.Required})
	}
	for _, p := range revision.Outputs {
		node.Outputs = append(node.Outputs, graph.Port{ID: p.ID, Type: graph.ValueType(p.Type), Required: p.Required})
	}
	execution, err := graph.NewDirectLoopSnapshot(request.SnapshotID, node, request.Inputs)
	if err != nil {
		return deny("invalid_inputs", "typed direct Loop inputs or topology are invalid")
	}
	snapshot, err := graph.NewDirectRunSnapshot(execution)
	if err != nil {
		return deny("invalid_snapshot", "direct Loop snapshot is invalid")
	}
	return service.acceptPreparedRun(ctx, request.SubmitGraphRequest, snapshot, workspace.Agent.ID, workspace.ID, "control-plane-only", core.AuthorityContext{})
}
