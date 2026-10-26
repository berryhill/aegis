package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/berryhill/aegis/internal/disposition"
	"github.com/berryhill/aegis/internal/doerbinding"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/registry"
	"time"
)

type graphNodeFailure struct {
	State  execution.State
	Reason string
}

func (e *graphNodeFailure) Error() string { return e.Reason }

// processRawChain owns one outer claim and two immutable node-causal attempts.
// Raw v2 and exact managed v7 nodes retain distinct execution paths.
func (worker *QueueWorker) processRawChain(ctx context.Context, request WorkRequest, item queue.Item, snapshot graph.GraphRunSnapshot) (WorkResult, error) {
	nodes, ok := worker.repository.(fleet.GraphNodeRepository)
	if !ok || item.MaxAttempts != 1 {
		return WorkResult{}, ErrWorkerDenied
	}
	definition, err := worker.repository.GetGraphRevision(ctx, snapshot.Graph.ID, snapshot.Graph.Revision)
	if err != nil {
		return WorkResult{}, err
	}
	plan, err := graph.ResolveProducerConsumerPlan(snapshot, definition)
	if err != nil {
		return WorkResult{}, err
	}
	gates := map[string]LayaGate{}
	managed := []loop.DoerContract{}
	// Resolve BOTH exact executable contracts before creating any child/claim.
	for _, n := range []graph.Node{plan.Producer, plan.Consumer} {
		lr, e := worker.repository.GetLoopRevision(ctx, n.Loop.ID, n.Loop.Revision)
		if e != nil || lr.Digest != n.Loop.Digest || (lr.SchemaVersion != loop.RevisionSchemaVersion && lr.SchemaVersion != loop.ManagedDoerSchemaVersion) || len(lr.RequiredEvidence) != 1 || !graphLoopPortsEqual(n.Inputs, lr.Inputs) || !graphLoopPortsEqual(n.Outputs, lr.Outputs) {
			return WorkResult{}, ErrWorkerDenied
		}
		if lr.SchemaVersion == loop.ManagedDoerSchemaVersion {
			c := lr.ManagedDoer.Doer
			agent, e := worker.repository.GetAgentRevision(ctx, n.Participant.ID, n.Participant.Revision)
			if e != nil || agent.Digest != n.Participant.Digest {
				return WorkResult{}, ErrWorkerDenied
			}
			if e = worker.authorizeDoer(ctx, c, agent); e != nil {
				return WorkResult{}, e
			}
			if e = managedOutputAbsent(c); e != nil {
				return WorkResult{}, e
			}
			managed = append(managed, c)
			gate, e := worker.preclaimDoerGate(ctx, request, item, c, n.Participant, n.Loop, snapshot.Graph)
			if e != nil {
				return WorkResult{}, e
			}
			gates[n.ID] = gate
		}
		if _, e = executableAction(lr); e != nil {
			return WorkResult{}, e
		}
		if worker.service.Readiness(ctx, ReadinessRequest{Action: FleetActionClaim, Subject: request.Subject, Authority: request.Authority, Agent: n.Participant, Loop: n.Loop, Graph: snapshot.Graph}).State != ReadinessReady {
			return WorkResult{}, ErrWorkerDenied
		}
	}
	if len(managed) > 0 {
		if len(managed) != 2 || managedWorkspacesSeparate(managed[0], managed[1]) != nil {
			return WorkResult{}, ErrWorkerDenied
		}
		for _, c := range managed {
			if e := managedOutputAbsent(c); e != nil {
				return WorkResult{}, e
			}
		}
	}
	node := plan.Producer
	now := worker.now()
	// One LoopExecution identifies this immutable Graph node across all bounded
	// attempts. A retry may add an Attempt, but cannot substitute a child identity.
	var loopExecution execution.LoopExecution
	loopExecutions, err := worker.repository.ListLoopExecutions(ctx)
	if err != nil {
		return WorkResult{}, err
	}
	for _, candidate := range loopExecutions {
		if candidate.GraphRunID == item.GraphRunID && candidate.GraphNodeID == node.ID {
			if loopExecution.LoopExecutionID != "" || candidate.Loop != node.Loop || candidate.Participant != node.Participant || candidate.LoopExecutionID != request.LoopExecutionID {
				return WorkResult{}, fmt.Errorf("%w: exact Loop execution binding required", ErrWorkerDenied)
			}
			loopExecution = candidate
		}
	}
	if loopExecution.LoopExecutionID == "" {
		loopExecution, err = execution.NewLoopExecution(execution.LoopExecution{LoopExecutionID: request.LoopExecutionID, GraphRunID: item.GraphRunID, GraphNodeID: node.ID, Loop: node.Loop, Participant: node.Participant, CreatedAt: now})
		if err != nil {
			return WorkResult{}, err
		}
		if _, err = worker.repository.CreateLoopExecution(ctx, loopExecution, worker.service.auditFact("fleet.loop-execution.created", request.Subject, "created exact Loop execution", node.Participant.ID, "", "")); err != nil {
			return WorkResult{}, err
		}
	}
	claim, err := queue.NewClaim(queue.Claim{ClaimID: request.ClaimID, QueueItem: digestRef(item.ItemID, item.Digest), AttemptID: request.AttemptID, WorkerID: request.WorkerID, Authority: request.Authority, ClaimedAt: now, ExpiresAt: now.Add(request.LeaseDuration)})
	if err != nil {
		return WorkResult{}, err
	}
	attempt, err := execution.NewAttempt(execution.Attempt{AttemptID: request.AttemptID, GraphRunID: item.GraphRunID, LoopExecutionID: loopExecution.LoopExecutionID, QueueItem: claim.QueueItem, ClaimID: claim.ClaimID, AttemptNumber: 1, CreatedAt: now})
	if err != nil {
		return WorkResult{}, err
	}
	claimTransition, err := queue.NewTransition(queue.QueueTransition{TransitionID: request.ClaimTransitionID, QueueItemID: item.ItemID, From: queue.StateQueued, To: queue.StateClaimed, ClaimID: claim.ClaimID, Reason: "worker lease acquired", OccurredAt: now})
	if err != nil {
		return WorkResult{}, err
	}
	if err = worker.repository.ClaimQueueItem(ctx, claim, attempt, claimTransition, worker.service.auditFact("fleet.queue.claimed", request.Subject, "worker lease acquired", node.Participant.ID, "", "")); err != nil {
		return WorkResult{}, fmt.Errorf("raw chain claim: %w", err)
	}
	base := WorkResult{Claim: claim, Attempt: attempt}
	var predecessor *fleet.GraphNodeCheckpoint
	for index, node := range []graph.Node{plan.Producer, plan.Consumer} {
		nr := request
		inputs := plan.ProducerInputs
		var artifacts []VerifiedGraphArtifactInput
		var input *fleet.GraphNodeHandoff
		child := loopExecution
		childAttempt := attempt
		if index == 1 {
			// Suffixes are stable under the original request, never allocated on retry.
			nr.LoopExecutionID += "-consumer"
			nr.AttemptID += "-consumer"
			nr.ArtifactID += "-consumer"
			inputs = plan.ConsumerInputs
			v, e := worker.ReadGraphArtifactInput(ctx, GraphArtifactRequest{Subject: request.Subject, Authority: request.Authority, QueueItemID: item.ItemID, GraphRunID: item.GraphRunID, ProducerNodeID: plan.Producer.ID, ConsumerNodeID: node.ID, EdgeID: plan.Edge.ID, ProducerExecutionID: predecessor.Admission.Execution.LoopExecutionID, ProducerAttemptID: predecessor.Admission.Attempt.AttemptID, ArtifactID: predecessor.Completion.Artifact.ID, DispositionID: predecessor.Completion.Disposition.DispositionID})
			if e != nil {
				if worker.service.Readiness(ctx, ReadinessRequest{Action: FleetActionDisposition, Subject: request.Subject, Authority: request.Authority, Agent: node.Participant, Loop: node.Loop, Graph: snapshot.Graph}).State != ReadinessReady {
					return base, e
				}
				return worker.terminal(ctx, request, base, execution.StateDenied, "graph_handoff_denied", nil, nil)
			}
			artifacts = []VerifiedGraphArtifactInput{v}
			lr, e := worker.repository.GetLoopRevision(ctx, node.Loop.ID, node.Loop.Revision)
			if e != nil {
				return base, e
			}
			if lr.SchemaVersion == loop.ManagedDoerSchemaVersion {
				p := v.Provenance()
				raw, e := json.Marshal(loop.ManagedArtifactSource{ArtifactID: p.ArtifactID, AttemptID: p.ProducerAttempt.ID, RunID: p.ProducerExecution.ID, ContentRef: p.ContentRef})
				if e != nil {
					return base, e
				}
				inputs = []graph.NormalizedInput{{PortID: "source", Type: graph.TypeArtifact, Value: raw}}
			}
			p := v.Provenance()
			input = &fleet.GraphNodeHandoff{NodeID: p.ProducerNodeID, ExecutionID: p.ProducerExecution.ID, AttemptID: p.ProducerAttempt.ID, ArtifactID: p.ArtifactID, ContentDigest: p.ContentRef, DispositionID: p.Disposition.ID, EdgeID: p.EdgeID}
			child, err = execution.NewLoopExecution(execution.LoopExecution{LoopExecutionID: nr.LoopExecutionID, GraphRunID: item.GraphRunID, GraphNodeID: node.ID, Loop: node.Loop, Participant: node.Participant, CreatedAt: worker.now()})
			if err != nil {
				return base, err
			}
			childAttempt, err = execution.NewAttempt(execution.Attempt{AttemptID: nr.AttemptID, GraphRunID: item.GraphRunID, LoopExecutionID: child.LoopExecutionID, QueueItem: claim.QueueItem, ClaimID: claim.ClaimID, AttemptNumber: 1, CreatedAt: child.CreatedAt})
			if err != nil {
				return base, err
			}
		}
		max := uint32(2)
		lr, e := worker.repository.GetLoopRevision(ctx, node.Loop.ID, node.Loop.Revision)
		if e != nil {
			return base, e
		}
		if lr.SchemaVersion == loop.ManagedDoerSchemaVersion {
			max = uint32(lr.ManagedDoer.Doer.MaxAttempts)
		}
		admission := fleet.GraphNodeAdmission{SchemaVersion: fleet.GraphNodeSchemaVersion, Claim: claim, Execution: child, Attempt: childAttempt, MaxImplementationPasses: max, Input: input, AdmittedAt: childAttempt.CreatedAt}
		admissionFact := worker.service.auditFact("fleet.graph.node.admitted", request.Subject, "node admitted", node.Participant.ID, "", "")
		admissionFact.Event.SubjectID = node.ID
		if err = nodes.AdmitGraphNode(ctx, admission, admissionFact); err != nil {
			return base, fmt.Errorf("raw chain node admission: %w", err)
		}
		completion, e := worker.executeRawGraphNode(ctx, nr, item, snapshot, node, admission, inputs, artifacts, gates[node.ID])
		if e != nil {
			var failure *graphNodeFailure
			if errors.As(e, &failure) {
				terminalCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				defer cancel()
				return worker.terminal(terminalCtx, request, base, failure.State, failure.Reason, nil, nil)
			}
			if worker.service.Readiness(ctx, ReadinessRequest{Action: FleetActionDisposition, Subject: request.Subject, Authority: request.Authority, Agent: node.Participant, Loop: node.Loop, Graph: snapshot.Graph}).State != ReadinessReady {
				return base, e
			}
			return base, e
		}
		checkpoint := fleet.GraphNodeCheckpoint{Admission: admission, Completion: completion}
		checkpointFact := worker.service.auditFact("fleet.graph.node.checkpointed", request.Subject, "node verified", node.Participant.ID, "", "")
		checkpointFact.Event.SubjectID = node.ID
		if err = nodes.CheckpointGraphNode(ctx, checkpoint, checkpointFact, worker.blobs); err != nil {
			return base, err
		}
		predecessor = &checkpoint
	}
	c := predecessor.Completion
	// Fresh node-specific disposition admission after all checkpoint/evidence reads.
	for _, n := range []graph.Node{plan.Producer, plan.Consumer} {
		if worker.service.Readiness(ctx, ReadinessRequest{Action: FleetActionDisposition, Subject: request.Subject, Authority: request.Authority, Agent: n.Participant, Loop: n.Loop, Graph: snapshot.Graph}).State != ReadinessReady {
			return worker.terminal(ctx, request, base, execution.StateDenied, "graph_disposition_denied", nil, nil)
		}
	}
	c.Disposition.DispositionID = request.DispositionID
	c.Disposition.OccurredAt = worker.now()
	c.Disposition, err = disposition.New(c.Disposition)
	if err != nil {
		return base, err
	}
	c.Transition.TransitionID = request.TerminalTransitionID
	c.Transition.OccurredAt = c.Disposition.OccurredAt
	c.Transition, err = queue.NewTransition(c.Transition)
	if err != nil {
		return base, err
	}
	authority, mandate, ok := worker.service.authorityIdentity(ctx, request.Authority)
	if !ok {
		return base, ErrWorkerDenied
	}
	fact := worker.service.auditFact("fleet.disposition.recorded", request.Subject, c.Disposition.ReasonCode, authority.AgentID, authority.Authority.StanzaID, mandate.ID)
	fact.Event.Outcome = string(c.Disposition.State)
	fact.Event.Metadata = map[string]string{"disposition_id": c.Disposition.DispositionID, "transition_id": c.Transition.TransitionID, "terminal_at": c.Disposition.OccurredAt.UTC().Format(time.RFC3339Nano)}
	if err = nodes.CompleteGraph(ctx, c, fact, worker.blobs); err != nil {
		return base, err
	}
	return WorkResult{Claim: claim, Attempt: predecessor.Admission.Attempt, Artifact: c.Artifact, Receipts: c.Receipts, Disposition: c.Disposition}, nil
}

func (worker *QueueWorker) executeRawGraphNode(ctx context.Context, request WorkRequest, item queue.Item, snapshot graph.GraphRunSnapshot, node graph.Node, a fleet.GraphNodeAdmission, inputs []graph.NormalizedInput, artifacts []VerifiedGraphArtifactInput, gate LayaGate) (fleet.Completion, error) {
	var completed fleet.Completion
	participant, err := worker.repository.GetAgentRevision(ctx, node.Participant.ID, node.Participant.Revision)
	if err != nil || participant.Digest != node.Participant.Digest || participant.Lifecycle != registry.LifecycleEnabled {
		return completed, ErrWorkerDenied
	}
	loopRevision, err := worker.repository.GetLoopRevision(ctx, node.Loop.ID, node.Loop.Revision)
	if err != nil || loopRevision.Digest != node.Loop.Digest {
		return completed, ErrWorkerDenied
	}
	actionID, err := executableAction(loopRevision)
	if err != nil {
		return completed, err
	}
	claim, attempt, loopExecution := a.Claim, a.Attempt, a.Execution
	now := worker.now()
	finish := func(state execution.State, reason string, artifact *evidence.RuntimeArtifact, receipts []evidence.VerificationReceipt) (fleet.Completion, error) {
		if state != execution.StateSucceeded {
			// A failed child is a separate immutable checkpoint, not a success
			// artifact/receipt and not the root disposition. Fresh authority is
			// still required; detachment never renews an expired mandate.
			commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			if worker.service.Readiness(commitCtx, ReadinessRequest{Action: FleetActionDisposition, Subject: request.Subject, Authority: request.Authority, Agent: node.Participant, Loop: node.Loop, Graph: snapshot.Graph}).State != ReadinessReady {
				return completed, ErrWorkerDenied
			}
			d, e := disposition.New(disposition.Record{DispositionID: request.DispositionID + "-" + node.ID, GraphRunID: item.GraphRunID, LoopExecutionID: loopExecution.LoopExecutionID, AttemptID: attempt.AttemptID, QueueItem: claim.QueueItem, Authority: request.Authority, State: state, ReasonCode: reason, ArtifactIDs: []string{}, ReceiptIDs: []string{}, OccurredAt: worker.now()})
			if e != nil {
				return completed, e
			}
			tr, e := queue.NewTransition(queue.QueueTransition{TransitionID: request.TerminalTransitionID + "-" + node.ID, QueueItemID: item.ItemID, From: queue.StateClaimed, To: executionQueueState(state), ClaimID: claim.ClaimID, Reason: reason, OccurredAt: d.OccurredAt})
			if e != nil {
				return completed, e
			}
			failed := fleet.GraphNodeCheckpoint{Admission: a, Completion: fleet.Completion{Claim: claim, Disposition: d, Transition: tr}}
			fact := worker.service.auditFact("fleet.graph.node.checkpointed", request.Subject, reason, node.Participant.ID, "", "")
			fact.Event.SubjectID = node.ID
			fact.Event.Outcome = string(state)
			if e = worker.repository.(fleet.GraphNodeRepository).CheckpointGraphNode(commitCtx, failed, fact, worker.blobs); e != nil {
				return completed, e
			}
			return completed, &graphNodeFailure{State: state, Reason: reason}
		}
		if ctx.Err() != nil {
			return completed, ctx.Err()
		}
		if worker.service.Readiness(ctx, ReadinessRequest{Action: FleetActionDisposition, Subject: request.Subject, Authority: request.Authority, Agent: node.Participant, Loop: node.Loop, Graph: snapshot.Graph}).State != ReadinessReady {
			return completed, ErrWorkerDenied
		}
		ids := make([]string, len(receipts))
		for i, r := range receipts {
			ids[i] = r.ID
		}
		d, e := disposition.New(disposition.Record{DispositionID: request.DispositionID + "-" + node.ID, GraphRunID: item.GraphRunID, LoopExecutionID: loopExecution.LoopExecutionID, AttemptID: attempt.AttemptID, QueueItem: claim.QueueItem, Authority: request.Authority, State: state, ReasonCode: reason, ArtifactIDs: []string{artifact.ID}, ReceiptIDs: ids, OccurredAt: worker.now()})
		if e != nil {
			return completed, e
		}
		tr, e := queue.NewTransition(queue.QueueTransition{TransitionID: request.TerminalTransitionID + "-" + node.ID, QueueItemID: item.ItemID, From: queue.StateClaimed, To: queue.StateSucceeded, ClaimID: claim.ClaimID, Reason: reason, OccurredAt: d.OccurredAt})
		if e != nil {
			return completed, e
		}
		proof, e := worker.verifier.AuthorizeCompletion(ctx, *artifact, receipts)
		if e != nil {
			return completed, e
		}
		return fleet.Completion{Claim: claim, Artifact: artifact, Receipts: receipts, Disposition: d, Transition: tr, Provenance: proof}, nil
	}
	if readiness := worker.service.Readiness(ctx, ReadinessRequest{Action: FleetActionRuntimeEffect, Subject: request.Subject, Authority: request.Authority, Agent: node.Participant, Loop: node.Loop, Graph: snapshot.Graph}); readiness.State != ReadinessReady {
		return finish(execution.StateDenied, "runtime_admission_denied", nil, nil)
	}
	authority, mandate, runtimeReadiness := worker.service.resolveAuthority(ctx, request.Subject, request.Authority)
	if runtimeReadiness.State != ReadinessReady || !agentMatchesAuthority(participant, authority, mandate) {
		return finish(execution.StateDenied, "runtime_binding_denied", nil, nil)
	}
	finished := now
	launch := execution.LaunchContract{
		OwnerID:          participant.Ownership.OwnerID,
		Mandate:          mandate,
		AuthorityContext: authority,
		ParentDispatch:   execution.Dispatch{ID: request.AttemptID, AuthorityContextID: authority.ID, State: execution.StateSucceeded, RequestedAt: now, StartedAt: &now, FinishedAt: &finished},
	}
	// Runtime custody is bounded by the immutable claim lease. WithDeadline also
	// preserves an earlier caller deadline, so neither bound can be extended.
	runtimeCtx, cancelRuntime := context.WithDeadline(ctx, claim.ExpiresAt)
	defer cancelRuntime()
	runtimeRequest := RuntimeRequest{
		GraphRunID: item.GraphRunID, LoopExecutionID: loopExecution.LoopExecutionID, GraphNodeID: node.ID,
		Authority: request.Authority, Inputs: inputs, Participant: participant, Launch: launch, LoopRevision: loopRevision,
		Admission: fleetRuntimeAdmission{service: worker.service, subject: request.Subject, authority: request.Authority, agent: node.Participant, loop: node.Loop, graph: snapshot.Graph},
	}
	runtimeRequest.ArtifactInputs = artifacts
	if loopRevision.SchemaVersion == loop.ManagedDoerSchemaVersion {
		contract, source, e := doerbinding.BindManagedDoerNode(loopRevision, node, inputs)
		if e != nil {
			return completed, e
		}
		input, recheck, e := worker.managedNodeSource(ctx, request, item, runtimeRequest, source, contract)
		if e != nil {
			return finish(execution.StateDenied, "graph_source_admission_denied", nil, nil)
		}
		_, e = worker.runSelectedDoer(runtimeCtx, request, WorkResult{Claim: claim, Attempt: attempt}, runtimeRequest, contract, gate, &input, recheck, func(_ context.Context, state execution.State, reason string, artifact *evidence.RuntimeArtifact, receipts []evidence.VerificationReceipt, _ ...evidence.CompletionProvenance) (WorkResult, error) {
			var err error
			completed, err = finish(state, reason, artifact, receipts)
			return WorkResult{}, err
		})
		return completed, e
	}
	if err = runtimeCtx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return finish(execution.StateExpired, "runtime_lease_expired", nil, nil)
		}
		return finish(execution.StateCancelled, "runtime_cancelled", nil, nil)
	}
	runtimeResult, runtimeErr := worker.adapter.Execute(runtimeCtx, runtimeRequest)
	if runtimeCtx.Err() != nil {
		if errors.Is(runtimeCtx.Err(), context.DeadlineExceeded) {
			return finish(execution.StateExpired, "runtime_lease_expired", nil, nil)
		}
		return finish(execution.StateCancelled, "runtime_cancelled", nil, nil)
	}
	if runtimeErr != nil {
		state, reason := execution.StateFailed, "runtime_effect_failed"
		var failure *RuntimeFailure
		if errors.As(runtimeErr, &failure) && executionQueueState(failure.State) != "" && failure.Reason != "" {
			state, reason = failure.State, failure.Reason
		}
		return finish(state, reason, nil, nil)
	}
	if len(runtimeResult.Output) == 0 || runtimeResult.MediaType == "" {
		return finish(execution.StateFailed, "runtime_output_invalid", nil, nil)
	}
	contentRef, err := worker.blobs.PutBlob(runtimeResult.Output)
	if err != nil {
		return finish(execution.StateFailed, "artifact_persistence_failed", nil, nil)
	}
	artifact := evidence.RuntimeArtifact{ID: request.ArtifactID, AttemptID: attempt.AttemptID, OwnerID: node.Participant.ID, ActionID: actionID, RunID: loopExecution.LoopExecutionID, AuthorityContextID: request.Authority.ID, AuthorityContextDigest: request.Authority.Digest, Digest: contentRef, ContentRef: contentRef, MediaType: runtimeResult.MediaType, CreatedAt: worker.now()}
	if err = artifact.Validate(); err != nil {
		return fleet.Completion{}, err
	}
	receipts := make([]evidence.VerificationReceipt, 0, len(loopRevision.RequiredEvidence))
	claims := evidenceClaims(loopRevision, actionID)
	for _, requirement := range loopRevision.RequiredEvidence {
		if readiness := worker.service.Readiness(ctx, ReadinessRequest{Action: FleetActionEvidenceVerify, Subject: request.Subject, Authority: request.Authority, Agent: node.Participant, Loop: node.Loop, Graph: snapshot.Graph}); readiness.State != ReadinessReady {
			return finish(execution.StateDenied, "evidence_admission_denied", nil, nil)
		}
		claim, found := claims[requirement.Claim]
		if !found {
			return finish(execution.StateFailed, "evidence_policy_missing", nil, nil)
		}
		policy := evidence.VerificationPolicy{VerifierID: claim.VerifierID, PolicyVersion: claim.PolicyVersion, Claim: claim.Claim, MediaType: claim.MediaType, ExpectedDigest: claim.ExpectedDigest}
		receipt, verifyErr := worker.verifier.Verify(ctx, artifact, policy)
		validReceipt := receipt.Validate() == nil && receipt.AttemptID == artifact.AttemptID && receipt.ArtifactID == artifact.ID && receipt.ActionID == artifact.ActionID && receipt.RunID == artifact.RunID && receipt.OwnerID == artifact.OwnerID && receipt.AuthorityContextID == artifact.AuthorityContextID && receipt.AuthorityContextDigest == artifact.AuthorityContextDigest && receipt.VerifierID == policy.VerifierID && receipt.PolicyVersion == policy.PolicyVersion && receipt.Claim == policy.Claim && receipt.MediaType == policy.MediaType && receipt.ExpectedDigest == policy.ExpectedDigest && receipt.ObservedDigest == artifact.Digest
		if validReceipt {
			receipts = append(receipts, receipt)
		}
		if verifyErr != nil || !validReceipt || receipt.Outcome != evidence.Passed {
			return finish(execution.StateFailed, "evidence_verification_failed", &artifact, receipts)
		}
	}
	return finish(execution.StateSucceeded, "evidence_satisfied", &artifact, receipts)
}
