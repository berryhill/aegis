package orchestration

import (
	"context"
	"errors"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/disposition"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
)

type graphArtifactReader interface {
	GetQueueItem(context.Context, string) (queue.Item, error)
	GetQueueRuntimeBinding(context.Context, string) (queue.RuntimeBinding, error)
	GetGraphRun(context.Context, string) (execution.GraphRun, error)
	GetGraphRunSnapshot(context.Context, string) (graph.GraphRunSnapshot, error)
	GetGraphRevision(context.Context, string, uint64) (graph.GraphRevision, error)
	GetLoopExecution(context.Context, string) (execution.LoopExecution, error)
	GetAttempt(context.Context, string) (execution.Attempt, error)
	GetClaim(context.Context, string) (queue.Claim, error)
	GetDisposition(context.Context, string) (disposition.Record, error)
	GetLoopRevision(context.Context, string, uint64) (loop.LoopRevision, error)
	GetRuntimeArtifact(context.Context, string) (evidence.RuntimeArtifact, error)
	GetVerificationReceipt(context.Context, string) (evidence.VerificationReceipt, error)
}

// GraphArtifactRequest selects immutable producing facts, never host paths,
// mutable latest references, expected text, replacement bytes or authority data.
// Authority is only a reference; the owning worker freshly authenticates it.
type GraphArtifactRequest struct {
	Subject             core.Subject
	Authority           reference.DigestRef
	QueueItemID         string
	GraphRunID          string
	ProducerNodeID      string
	ConsumerNodeID      string
	EdgeID              string
	ProducerExecutionID string
	ProducerAttemptID   string
	ArtifactID          string
	DispositionID       string
}

// GraphArtifactProvenance preserves exact edge and producer causality separately
// from the content address. Equal bytes from another attempt are not this input.
type GraphArtifactProvenance struct {
	QueueItem         reference.DigestRef
	Snapshot          reference.DigestRef
	Graph             reference.RevisionRef
	GraphRunID        string
	ProducerNodeID    string
	ConsumerNodeID    string
	EdgeID            string
	FromPort          string
	ToPort            string
	ProducerExecution reference.DigestRef
	ProducerAttempt   reference.DigestRef
	ArtifactID        string
	ContentRef        string
	ReceiptID         string
	ReceiptRef        string
	Disposition       reference.DigestRef
	Authority         reference.DigestRef
}

// VerifiedGraphArtifactInput cannot be minted with model-authored bytes. Its
// accessors copy content and return value-only provenance. It is not a reusable
// execution capability: re-read/re-admit at the actual consumer effect boundary.
type VerifiedGraphArtifactInput struct {
	content    []byte
	provenance GraphArtifactProvenance
}

func (v VerifiedGraphArtifactInput) Content() []byte                     { return append([]byte(nil), v.content...) }
func (v VerifiedGraphArtifactInput) Provenance() GraphArtifactProvenance { return v.provenance }

// ReadGraphArtifactInput is the controller-owned handoff boundary used by the
// two-node raw worker after the durable producer checkpoint. It re-admits exact
// authority and independently reloads evidence before returning actual bytes.
func (worker *QueueWorker) ReadGraphArtifactInput(ctx context.Context, request GraphArtifactRequest) (VerifiedGraphArtifactInput, error) {
	if worker == nil || worker.service == nil || worker.repository == nil || worker.blobs == nil {
		return VerifiedGraphArtifactInput{}, ErrWorkerDenied
	}
	return readGraphArtifactInput(ctx, worker.repository, worker.blobs, request, func(ctx context.Context, plan *graph.ProducerConsumerPlan, ref reference.RevisionRef) error {
		if plan == nil {
			_, _, ready := worker.service.resolveAuthority(ctx, request.Subject, request.Authority)
			if ready.State != ReadinessReady {
				return ErrWorkerDenied
			}
			return nil
		}
		for _, node := range []graph.Node{plan.Producer, plan.Consumer} {
			ready := worker.service.Readiness(ctx, ReadinessRequest{Action: FleetActionRuntimeEffect, Subject: request.Subject, Authority: request.Authority, Agent: node.Participant, Loop: node.Loop, Graph: ref})
			if ready.State != ReadinessReady {
				return ErrWorkerDenied
			}
		}
		return nil
	})
}

// The helper deliberately uses only existing authoritative reads. No canonical
// records or approval facts are synthesized to make a handoff eligible.
func readGraphArtifactInput(ctx context.Context, records graphArtifactReader, blobs evidence.OutputBlobReader, request GraphArtifactRequest, admit func(context.Context, *graph.ProducerConsumerPlan, reference.RevisionRef) error) (VerifiedGraphArtifactInput, error) {
	denied := errors.Join(ErrWorkerDenied, errors.New("verified Graph artifact input denied"))
	fail := func() (VerifiedGraphArtifactInput, error) { return VerifiedGraphArtifactInput{}, denied }
	if ctx.Err() != nil {
		return VerifiedGraphArtifactInput{}, ctx.Err()
	}
	if records == nil || blobs == nil || admit == nil || request.Authority.Validate() != nil || request.QueueItemID == "" || request.GraphRunID == "" || request.ProducerNodeID == "" || request.ConsumerNodeID == "" || request.EdgeID == "" || request.ProducerExecutionID == "" || request.ProducerAttemptID == "" || request.ArtifactID == "" || request.DispositionID == "" {
		return fail()
	}
	if err := admit(ctx, nil, reference.RevisionRef{}); err != nil {
		return fail()
	}
	item, err := records.GetQueueItem(ctx, request.QueueItemID)
	if err != nil || item.ItemID != request.QueueItemID || item.GraphRunID != request.GraphRunID {
		return fail()
	}
	if _, err := queue.MarshalItem(item); err != nil {
		return fail()
	}
	itemRef := digestRef(item.ItemID, item.Digest)
	if item.Authority != request.Authority {
		binding, err := records.GetQueueRuntimeBinding(ctx, item.ItemID)
		if err != nil || binding.QueueItem != itemRef || binding.Authority != request.Authority || binding.Submission != item.Submission {
			return fail()
		}
	}
	run, err := records.GetGraphRun(ctx, item.GraphRunID)
	if err != nil || run.GraphRunID != item.GraphRunID || run.QueueItem != itemRef || run.Snapshot != item.Snapshot || run.Authority != item.Authority {
		return fail()
	}
	if _, err := execution.MarshalGraphRun(run); err != nil {
		return fail()
	}
	snapshot, err := records.GetGraphRunSnapshot(ctx, item.Snapshot.ID)
	if err != nil || snapshot.SnapshotID != item.Snapshot.ID || snapshot.Digest != item.Snapshot.Digest {
		return fail()
	}
	definition, err := records.GetGraphRevision(ctx, snapshot.Graph.ID, snapshot.Graph.Revision)
	if err != nil {
		return fail()
	}
	plan, err := graph.ResolveProducerConsumerPlan(snapshot, definition)
	if err != nil || plan.Producer.ID != request.ProducerNodeID || plan.Consumer.ID != request.ConsumerNodeID || plan.Edge.ID != request.EdgeID {
		return fail()
	}
	if err := admit(ctx, &plan, snapshot.Graph); err != nil {
		return fail()
	}
	producer, err := records.GetLoopExecution(ctx, request.ProducerExecutionID)
	if err != nil || producer.LoopExecutionID != request.ProducerExecutionID || producer.GraphRunID != item.GraphRunID || producer.GraphNodeID != plan.Producer.ID || producer.Loop != plan.Producer.Loop || producer.Participant != plan.Producer.Participant {
		return fail()
	}
	if _, err := execution.MarshalLoopExecution(producer); err != nil {
		return fail()
	}
	attempt, err := records.GetAttempt(ctx, request.ProducerAttemptID)
	if err != nil || attempt.AttemptID != request.ProducerAttemptID || attempt.GraphRunID != item.GraphRunID || attempt.LoopExecutionID != producer.LoopExecutionID || attempt.QueueItem != itemRef || attempt.AttemptNumber > item.MaxAttempts {
		return fail()
	}
	if _, err := execution.MarshalAttempt(attempt); err != nil {
		return fail()
	}
	claim, err := records.GetClaim(ctx, attempt.ClaimID)
	if err != nil || claim.ClaimID != attempt.ClaimID || claim.AttemptID != attempt.AttemptID || claim.QueueItem != itemRef || claim.Authority != request.Authority {
		return fail()
	}
	if _, err := queue.MarshalClaim(claim); err != nil {
		return fail()
	}
	nodeRecords, ok := records.(interface {
		GetGraphNodeCheckpoint(context.Context, string, string) (fleet.GraphNodeCheckpoint, error)
	})
	if !ok {
		return fail()
	}
	checkpoint, err := nodeRecords.GetGraphNodeCheckpoint(ctx, item.GraphRunID, plan.Producer.ID)
	if err != nil || checkpoint.Admission.Claim != claim || checkpoint.Admission.Attempt != attempt || checkpoint.Admission.Execution != producer {
		return fail()
	}
	outcome := checkpoint.Completion.Disposition
	if err != nil || outcome.DispositionID != request.DispositionID || outcome.GraphRunID != item.GraphRunID || outcome.LoopExecutionID != producer.LoopExecutionID || outcome.AttemptID != attempt.AttemptID || outcome.QueueItem != itemRef || outcome.Authority != request.Authority || outcome.State != execution.StateSucceeded || len(outcome.ArtifactIDs) != 1 || outcome.ArtifactIDs[0] != request.ArtifactID || len(outcome.ReceiptIDs) != 1 || !outcome.OccurredAt.Before(claim.ExpiresAt) {
		return fail()
	}
	if _, err := disposition.Marshal(outcome); err != nil {
		return fail()
	}
	revision, err := records.GetLoopRevision(ctx, producer.Loop.ID, producer.Loop.Revision)
	if err != nil || revision.Digest != producer.Loop.Digest || (revision.SchemaVersion != loop.RevisionSchemaVersion && revision.SchemaVersion != loop.ManagedDoerSchemaVersion) || len(revision.RequiredEvidence) != 1 || loop.ValidateRevision(revision).Outcome != loop.ValidationValid || !graphLoopPortsEqual(plan.Producer.Inputs, revision.Inputs) || !graphLoopPortsEqual(plan.Producer.Outputs, revision.Outputs) {
		return fail()
	}
	requirement := revision.RequiredEvidence[0]
	var policy evidence.VerificationPolicy
	found := false
	for _, step := range revision.Steps {
		if step.ID != requirement.ProducerStepID {
			continue
		}
		for _, claim := range step.EvidenceClaims {
			if claim.Claim != requirement.Claim {
				continue
			}
			if found {
				return fail()
			}
			found = true
			policy = evidence.VerificationPolicy{VerifierID: claim.VerifierID, PolicyVersion: claim.PolicyVersion, Claim: claim.Claim, MediaType: claim.MediaType, ExpectedDigest: claim.ExpectedDigest}
		}
	}
	if !found {
		return fail()
	}
	artifact, err := records.GetRuntimeArtifact(ctx, request.ArtifactID)
	if err != nil {
		return fail()
	}
	receipt, err := records.GetVerificationReceipt(ctx, outcome.ReceiptIDs[0])
	if err != nil || receipt.ID != outcome.ReceiptIDs[0] {
		return fail()
	}
	data, err := evidence.ReadVerifiedOutput(ctx, blobs, artifact, []evidence.VerificationReceipt{receipt}, evidence.VerifiedOutputBinding{ArtifactID: request.ArtifactID, AttemptID: attempt.AttemptID, RunID: producer.LoopExecutionID, ActionID: requirement.ProducerStepID, OwnerID: producer.Participant.ID, AuthorityContextID: request.Authority.ID, AuthorityContextDigest: request.Authority.Digest, Policy: policy})
	if err != nil {
		return fail()
	}
	// A valid observation is not transferable through revocation/expiry. Repeat
	// controller admission after all potentially blocking evidence reads.
	if err := admit(ctx, &plan, snapshot.Graph); err != nil {
		return fail()
	}
	if ctx.Err() != nil {
		return VerifiedGraphArtifactInput{}, ctx.Err()
	}
	return VerifiedGraphArtifactInput{content: data, provenance: GraphArtifactProvenance{QueueItem: itemRef, Snapshot: item.Snapshot, Graph: snapshot.Graph, GraphRunID: item.GraphRunID, ProducerNodeID: producer.GraphNodeID, ConsumerNodeID: plan.Consumer.ID, EdgeID: plan.Edge.ID, FromPort: plan.Edge.Mappings[0].FromPort, ToPort: plan.Edge.Mappings[0].ToPort, ProducerExecution: digestRef(producer.LoopExecutionID, producer.Digest), ProducerAttempt: digestRef(attempt.AttemptID, attempt.Digest), ArtifactID: artifact.ID, ContentRef: artifact.ContentRef, ReceiptID: receipt.ID, ReceiptRef: receipt.EvidenceRef, Disposition: digestRef(outcome.DispositionID, outcome.Digest), Authority: request.Authority}}, nil
}

func graphLoopPortsEqual(asserted []graph.Port, exact []loop.Port) bool {
	if len(asserted) != len(exact) {
		return false
	}
	for _, port := range asserted {
		found := false
		for _, candidate := range exact {
			if port.ID == candidate.ID && port.Type == graph.ValueType(candidate.Type) && port.Required == candidate.Required {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
