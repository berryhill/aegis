package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
)

// QueueLoopInput grants one bounded foreground operation, not foundational authority.
type QueueLoopInput struct {
	Agent          reference.RevisionRef   `json:"agent"`
	Loop           reference.RevisionRef   `json:"loop"`
	IdempotencyKey string                  `json:"idempotency_key"`
	QueueItemID    string                  `json:"queue_item_id,omitempty"`
	Activate       bool                    `json:"activate,omitempty"`
	Inputs         []graph.NormalizedInput `json:"inputs,omitempty"`
}
type QueueLoopResult struct {
	Rejection   *queue.Rejection      `json:"rejection,omitempty"`
	Graph       reference.RevisionRef `json:"graph"`
	QueueItemID string                `json:"queue_item_id"`
	Reason      string                `json:"reason"`
	Execution   *QueueExecutionView   `json:"execution,omitempty"`
}

func loopLifecyclePrevious(view LoopView) string {
	if len(view.History) > 0 {
		return view.History[len(view.History)-1].Digest
	}
	return ""
}

func (s *Service) QueueLoop(ctx context.Context, input QueueLoopInput) (QueueLoopResult, error) {
	subject, err := s.Authenticate(ctx)
	if err != nil {
		return QueueLoopResult{}, err
	}
	return s.QueueLoopAs(ctx, subject, input)
}

func (s *Service) QueueLoopAs(ctx context.Context, subject core.Subject, input QueueLoopInput) (result QueueLoopResult, err error) {
	if err = s.requireFleetPrincipal(subject); err != nil {
		return
	}
	if input.Agent.Validate() != nil || input.Loop.Validate() != nil || input.IdempotencyKey == "" || len(input.IdempotencyKey) > 256 {
		return result, errors.New("exact Agent, Loop and bounded idempotency key required")
	}
	// The graph ID is the durable idempotency slot. Its immutable digest detects
	// changed request payloads; phase IDs are recoverable without client files.
	sum := sha256.Sum256([]byte(subject.PrincipalID + "\x00" + input.IdempotencyKey))
	key := "loopq-" + hex.EncodeToString(sum[:16])
	input.Inputs = append([]graph.NormalizedInput(nil), input.Inputs...)
	for i := range input.Inputs {
		valueWire, err := graph.CanonicalInputJSON(input.Inputs[i].Value)
		if err != nil {
			return result, err
		}
		input.Inputs[i].Value = valueWire
	}
	sort.Slice(input.Inputs, func(i, j int) bool { return input.Inputs[i].PortID < input.Inputs[j].PortID })
	wire, marshalErr := json.Marshal(input)
	if marshalErr != nil {
		return result, marshalErr
	}
	payload := sha256.Sum256(wire)
	identity := hex.EncodeToString(payload[:])
	result.QueueItemID = key + "-queue"
	lv, e := s.GetLoopViewAs(ctx, subject, input.Loop.ID, input.Loop.Revision)
	if e != nil {
		return result, e
	}
	if lv.Revision.Digest != input.Loop.Digest {
		return result, ErrDenied
	}
	if input.QueueItemID != "" {
		prior, e := s.GetQueueItemAs(ctx, subject, input.QueueItemID)
		if e != nil {
			return result, e
		}
		snapshot, e := s.FleetRepository.GetGraphRunSnapshot(ctx, prior.Item.Snapshot.ID)
		if e != nil {
			return result, e
		}
		g, e := s.FleetRepository.GetGraphRevision(ctx, snapshot.Graph.ID, snapshot.Graph.Revision)
		if e != nil {
			return result, e
		}
		candidate, e := graph.NewRunSnapshot(snapshot.SnapshotID, g, input.Inputs)
		if e != nil || candidate.Digest != snapshot.Digest || snapshot.Digest != prior.Item.Snapshot.Digest || g.Digest != snapshot.Graph.Digest || len(g.Nodes) != 1 || g.Nodes[0].Participant != input.Agent || g.Nodes[0].Loop != input.Loop {
			return result, fleet.ErrConflict
		}
		result.QueueItemID, result.Graph, result.Execution = input.QueueItemID, snapshot.Graph, &prior
		if len(prior.Attempts) > 0 || (!prior.Projection.State.IsPreparation() && prior.Projection.State != queue.StateQueued) {
			result.Reason = "existing_execution"
			return result, nil
		}
		if lv.Lifecycle.State != loop.LifecycleActive || lv.Lifecycle.ActiveDigest != input.Loop.Digest {
			return result, ErrDenied
		}
		recoveryKey := sha256.Sum256([]byte(input.QueueItemID))
		workspace, e := s.RegisteredAgentWorkspaceAs(ctx, subject, input.Agent.ID)
		if e != nil || workspace.Agent != input.Agent {
			return result, ErrDenied
		}
		return s.prepareQueuedLoop(ctx, subject, input, lv, result, "recover-"+hex.EncodeToString(recoveryKey[:16]))
	}
	// Rejections are immutable blocked intent, never fabricated executable work.
	if prior, e := s.FleetRepository.GetRejection(ctx, key+"-reject"); e == nil {
		if prior.Reason != "request_sha256:"+identity {
			return result, fleet.ErrConflict
		}
		result.QueueItemID = ""
		result.Reason, result.Rejection = prior.ReasonCode, &prior
		return result, nil
	} else if !errors.Is(e, fleet.ErrNotFound) {
		return result, e
	}
	// Resolve historical identity before lifecycle mutations or publication.
	historical, historicalErr := s.FleetRepository.GetGraphRevision(ctx, key, 1)
	if historicalErr == nil {
		if len(historical.Nodes) != 1 || historical.Nodes[0].ID != "loop-"+identity || historical.Nodes[0].Participant != input.Agent || historical.Nodes[0].Loop != input.Loop {
			return result, fleet.ErrConflict
		}
		result.Graph = reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: key, Revision: 1, Digest: historical.Digest}
		if old, e := s.FleetRepository.GetGraphRunSnapshot(ctx, key+"-snapshot"); e == nil {
			candidate, e := graph.NewRunSnapshot(key+"-snapshot", historical, input.Inputs)
			if e != nil || candidate.Digest != old.Digest {
				return result, fleet.ErrConflict
			}
		} else if !errors.Is(e, fleet.ErrNotFound) {
			return result, e
		}
		if prior, e := s.GetQueueItemAs(ctx, subject, result.QueueItemID); e == nil {
			result.Execution = &prior
			if len(prior.Attempts) > 0 || (!prior.Projection.State.IsPreparation() && prior.Projection.State != queue.StateQueued) {
				result.Reason = "existing_execution"
				return result, nil
			}
		} else if !errors.Is(e, fleet.ErrNotFound) {
			return result, e
		}
	} else if !errors.Is(historicalErr, fleet.ErrNotFound) {
		return result, historicalErr
	}
	workspace, e := s.RegisteredAgentWorkspaceAs(ctx, subject, input.Agent.ID)
	if e != nil {
		return result, e
	}
	if workspace.Agent != input.Agent {
		return result, ErrDenied
	}
	if lv.Lifecycle.State != loop.LifecycleActive || lv.Lifecycle.ActiveDigest != input.Loop.Digest {
		if !input.Activate {
			result.QueueItemID = ""
			result.Reason = "exact_loop_activation_required"
			rejection, e := queue.NewRejection(queue.Rejection{RejectionID: key + "-reject", SubmissionID: key + "-submission", IdempotencyKey: key, ReasonCode: result.Reason, Reason: "request_sha256:" + identity, RejectedAt: s.Now()})
			if e != nil {
				return result, e
			}
			_, e = s.FleetRepository.RejectSubmission(ctx, rejection, fleet.AuditFact{Event: core.AuditEvent{Type: "fleet.submission.rejected", Outcome: "denied", Reason: result.Reason}})
			if e != nil {
				return result, e
			}
			stored, e := s.FleetRepository.GetRejection(ctx, rejection.RejectionID)
			result.Rejection = &stored
			return result, e
		}
		_, e = s.SetLoopLifecycleAs(ctx, subject, input.Loop.ID, SetLoopLifecycleInput{AgentID: input.Agent.ID, Loop: input.Loop, State: loop.LifecycleActive, EventID: key + "-activate", ExpectedPreviousDigest: loopLifecyclePrevious(lv)})
		if e != nil {
			return result, e
		}
	}
	g := graph.GraphRevision{GraphID: key, Revision: 1, Nodes: []graph.Node{{ID: "loop-" + identity, Participant: input.Agent, Loop: input.Loop}}}
	for _, p := range lv.Revision.Inputs {
		port := graph.Port{ID: p.ID, Type: graph.ValueType(p.Type), Required: p.Required}
		g.Inputs = append(g.Inputs, port)
		g.Nodes[0].Inputs = append(g.Nodes[0].Inputs, port)
		g.InputMappings = append(g.InputMappings, graph.InputMapping{GraphInput: p.ID, ToNodeID: g.Nodes[0].ID, ToPort: p.ID})
	}
	for _, p := range lv.Revision.Outputs {
		port := graph.Port{ID: p.ID, Type: graph.ValueType(p.Type), Required: p.Required}
		g.Outputs = append(g.Outputs, port)
		g.Nodes[0].Outputs = append(g.Nodes[0].Outputs, port)
		g.OutputMappings = append(g.OutputMappings, graph.OutputMapping{FromNodeID: g.Nodes[0].ID, FromPort: p.ID, GraphOutput: p.ID})
	}
	// Publication's idempotency binding includes the complete request identity.
	published := PublishedGraph{Revision: historical}
	if errors.Is(historicalErr, fleet.ErrNotFound) {
		published, e = s.PublishGraphAs(ctx, subject, PublishGraphInput{AgentID: input.Agent.ID, Revision: g, IdempotencyKey: key + "-" + identity})
		if e != nil {
			return result, e
		}
	}
	result.Graph = reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: key, Revision: 1, Digest: published.Revision.Digest}
	// A submission's canonical input digest supplies a second conflict check.
	if old, e := s.FleetRepository.GetGraphRunSnapshot(ctx, key+"-snapshot"); e == nil {
		candidate, e := graph.NewRunSnapshot(key+"-snapshot", published.Revision, input.Inputs)
		if e != nil || candidate.Digest != old.Digest {
			return result, fleet.ErrConflict
		}
	} else if !errors.Is(e, fleet.ErrNotFound) {
		return result, e
	}
	if prior, e := s.GetQueueItemAs(ctx, subject, result.QueueItemID); e == nil {
		result.Execution = &prior
		// Never reclaim or duplicate an already started execution on HTTP retry.
		if len(prior.Attempts) > 0 || (!prior.Projection.State.IsPreparation() && prior.Projection.State != queue.StateQueued) {
			result.Reason = "existing_execution"
			return result, nil
		}
	} else if !errors.Is(e, fleet.ErrNotFound) {
		return result, e
	}
	decision, e := s.SubmitGraphAs(ctx, subject, orchestration.SubmitGraphRequest{WorkspaceAgentID: input.Agent.ID, Graph: result.Graph, Inputs: input.Inputs, SubmissionID: key + "-submission", IdempotencyKey: key, SnapshotID: key + "-snapshot", QueueItemID: result.QueueItemID, GraphRunID: key + "-run", TransitionID: key + "-prepare", RejectionID: key + "-reject", MaxAttempts: 1})
	if e != nil {
		return result, e
	}
	if decision.Accepted == nil {
		result.Reason = "submission_denied"
		return result, nil
	}
	return s.prepareQueuedLoop(ctx, subject, input, lv, result, key)
}

func (s *Service) prepareQueuedLoop(ctx context.Context, subject core.Subject, input QueueLoopInput, lv LoopView, result QueueLoopResult, key string) (QueueLoopResult, error) {
	readback := func(reason string) (QueueLoopResult, error) {
		result.Reason = reason
		v, e := s.GetQueueItemAs(ctx, subject, result.QueueItemID)
		if e == nil {
			if v.Projection.State.IsPreparation() {
				sink, ok := s.FleetRepository.(interface {
					RecordPreparationDiagnostic(context.Context, queue.QueueTransition, fleet.AuditFact) error
				})
				if !ok {
					return result, errors.New("preparation diagnostic custody unavailable")
				}
				sum := sha256.Sum256([]byte(v.Projection.LastTransitionID + "\x00" + reason))
				tr, trErr := queue.NewTransition(queue.QueueTransition{TransitionID: "diagnostic-" + hex.EncodeToString(sum[:16]), QueueItemID: result.QueueItemID, From: v.Projection.State, To: v.Projection.State, Reason: reason, OccurredAt: s.Now()})
				if trErr != nil {
					return result, trErr
				}
				if len(v.Transitions) == 0 || v.Transitions[len(v.Transitions)-1].Reason != reason {
					if trErr = sink.RecordPreparationDiagnostic(ctx, tr, fleet.AuditFact{Event: core.AuditEvent{Type: "fleet.preparation.blocked", Outcome: "denied", Reason: reason, SubjectID: subject.ID}}); trErr != nil {
						return result, trErr
					}
				}
				v, e = s.GetQueueItemAs(ctx, subject, result.QueueItemID)
			}
			result.Execution = &v
		}
		return result, e
	}
	agent, e := s.FleetRepository.GetAgentRevision(ctx, input.Agent.ID, input.Agent.Revision)
	if e != nil {
		return result, e
	}
	verified, receiptErr := s.hasVerifiedReceipt(agent.Charter.Digest)
	if receiptErr != nil {
		return readback("provisioning_receipt_unavailable")
	}
	if !verified {
		return readback("provisioning_receipt_missing")
	}
	runtimeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	runtime, runtimeErr := s.Runtime(runtimeCtx)
	cancel()
	if runtimeErr != nil {
		return readback("runtime_unavailable_or_unsupported")
	}
	charter, charterErr := s.GetCharter(agent.Charter.ID, agent.Charter.Revision)
	if charterErr != nil || charter.Digest != agent.Charter.Digest {
		return readback("exact_charter_unavailable")
	}
	if runtimeSatisfies(runtime.Version, charter.Charter.Runtime.VersionConstraint) != nil {
		return readback("runtime_version_unsupported")
	}
	if e = s.QueueWorker.ValidateLoopAdmission(lv.Revision, agent); e != nil {
		return readback("implementation_prerequisite_required")
	}
	authority, e := s.fleetCommandAuthorityForAgent(ctx, subject, &agent)
	reservation := sha256.Sum256([]byte(subject.ID + "\x00" + agent.Digest))
	reservationID := hex.EncodeToString(reservation[:])
	var fence map[string]string
	if fenceErr := s.Store.Load("queue-session-preparation", reservationID, &fence); fenceErr == nil {
		var prepared reference.DigestRef
		if e != nil || s.Store.Load("queue-session-prepared", reservationID, &prepared) != nil || prepared != authority.Authority {
			return readback("session_preparation_in_progress_or_interrupted")
		}
	} else if !errors.Is(fenceErr, os.ErrNotExist) {
		return readback("session_preparation_in_progress_or_interrupted")
	}
	var missing *runtimeAuthorityDiagnostic
	if errors.As(e, &missing) && missing.reason == "no_live_exact_runtime_session" {
		// Never replace expired/revoked/interrupted authority implicitly. A
		// fresh queue request does not authorize resurrection of old sessions.
		contexts, listErr := s.Authority.ListAuthorityContexts(ctx)
		if listErr != nil {
			return readback("runtime_authority_unavailable")
		}
		for _, old := range contexts {
			if old.SubjectID == subject.ID && old.AgentID == agent.AgentID && old.CharterDigest == agent.Charter.Digest {
				return readback("prior_runtime_authority_requires_explicit_recovery")
			}
		}
		decision, selectErr := s.Select(charter, subject, "", core.Environment{Name: "local"})
		if selectErr != nil {
			return readback("session_selection_" + decision.Reason)
		}
		// Atomic create-only reservation prevents duplicate launches even if
		// interrupted between session launch and queue binding. It is only a
		// denial fence; it never supplies authority to the runtime.
		if reserveErr := s.Store.Create("queue-session-preparation", reservationID, map[string]string{"queue_item_id": result.QueueItemID, "agent_digest": agent.Digest}); reserveErr != nil {
			return readback("session_preparation_in_progress_or_interrupted")
		}
		mandate, _, previewErr := s.PreviewSessionAs(ctx, subject, agent.Charter.ID, agent.Charter.Revision, "", core.Environment{Name: "local"})
		if previewErr != nil {
			return readback("session_mandate_preparation_failed")
		}
		if _, startErr := s.StartSessionAs(ctx, subject, mandate.ID); startErr != nil {
			return readback("session_start_preparation_failed")
		}
		authority, e = s.fleetCommandAuthorityForAgent(ctx, subject, &agent)
		if e == nil {
			if receiptErr := s.Store.Create("queue-session-prepared", reservationID, authority.Authority); receiptErr != nil {
				return readback("session_preparation_in_progress_or_interrupted")
			}
		}
	}
	if e != nil || authority.Publisher != input.Agent {
		var diagnostic *runtimeAuthorityDiagnostic
		if errors.As(e, &diagnostic) {
			return readback(diagnostic.reason)
		}
		return readback("runtime_authority_unavailable")
	}
	// Active authority alone is not proof that Launch materialized a session.
	canonicalAuthority, e := s.Authority.GetAuthorityContext(ctx, authority.Authority.ID)
	if e != nil || canonicalAuthority.Digest != authority.Authority.Digest {
		return readback("runtime_authority_unavailable")
	}
	session, e := s.GetSession(canonicalAuthority.SessionID)
	if e != nil || session.ID != canonicalAuthority.SessionID || session.Mandate.ID != canonicalAuthority.MandateID || core.ValidateAuthorityContext(canonicalAuthority, session.Mandate) != nil || session.Status != "running" || session.RuntimeSessionID == "" || session.RuntimePID <= 0 || session.ProcessStart == "" {
		return readback("session_preparation_in_progress_or_interrupted")
	}
	bindingID, transitionID := key+"-binding", key+"-ready"
	if prior, priorErr := s.FleetRepository.GetQueueRuntimeBinding(ctx, result.QueueItemID); priorErr == nil {
		transitions, listErr := s.FleetRepository.ListQueueTransitions(ctx, result.QueueItemID)
		if listErr != nil {
			return result, listErr
		}
		transitionID = ""
		for _, tr := range transitions {
			if tr.QueueItemID == result.QueueItemID && tr.From.IsPreparation() && tr.To == queue.StateQueued && tr.OccurredAt.Equal(prior.BoundAt) {
				if transitionID != "" {
					return result, fleet.ErrConflict
				}
				transitionID = tr.TransitionID
			}
		}
		if transitionID == "" {
			return result, fleet.ErrConflict
		}
		bindingID = prior.BindingID
	} else if !errors.Is(priorErr, fleet.ErrNotFound) {
		return result, priorErr
	}
	// BindRuntime verifies scope, ownership and authority before ID reuse.
	_, _, e = s.BindQueueRuntimeAs(ctx, subject, BindQueueRuntimeInput{AgentID: input.Agent.ID, Authority: authority.Authority, QueueItemID: result.QueueItemID, BindingID: bindingID, TransitionID: transitionID})
	if e != nil {
		return readback("exact_runtime_binding_required")
	}
	_, e = s.ProcessQueueItemAs(ctx, subject, orchestration.WorkRequest{Authority: authority.Authority, QueueItemID: result.QueueItemID, WorkerID: "exact-loop-controller", LoopExecutionID: key + "-loop", ClaimID: key + "-claim", AttemptID: key + "-attempt", ClaimTransitionID: key + "-claimed", TerminalTransitionID: key + "-terminal", DispositionID: key + "-disposition", ArtifactID: key + "-artifact", LeaseDuration: 6 * time.Minute})
	if e != nil {
		r, readErr := readback("processing_failed")
		if readErr != nil {
			return r, readErr
		}
		return r, e
	}
	return readback("processed")
}
