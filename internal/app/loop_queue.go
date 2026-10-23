package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"errors"
	"os"

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
	Reexecution    *LoopReexecution        `json:"reexecution,omitempty"`
	Agent          reference.RevisionRef   `json:"agent"`
	Loop           reference.RevisionRef   `json:"loop"`
	IdempotencyKey string                  `json:"idempotency_key"`
	QueueItemID    string                  `json:"queue_item_id,omitempty"`
	Activate       bool                    `json:"activate,omitempty"`
	Inputs         []graph.NormalizedInput `json:"inputs,omitempty"`
}
type QueueLoopResult struct {
	Readiness   *DoerCandidateReadiness `json:"readiness,omitempty"`
	Reexecution *LoopReexecutionFact    `json:"reexecution,omitempty"`
	Preparation *sessionPreparationFact `json:"preparation,omitempty"`
	Rejection   *queue.Rejection        `json:"rejection,omitempty"`
	Graph       reference.RevisionRef   `json:"graph"`
	QueueItemID string                  `json:"queue_item_id"`
	Reason      string                  `json:"reason"`
	Execution   *QueueExecutionView     `json:"execution,omitempty"`
	// RequestID is the stable, principal-scoped intent slot. A prerequisite
	// denial can be retried with the same payload and idempotency key after an
	// independently approved change; it is not a Queue item or an approval.
	RequestID string `json:"request_id,omitempty"`
	// RequiredCharter identifies the exact authority revision that blocked a
	// v4 Doer run. Never infer a new charter or provisioning approval from it.
	RequiredCharter *reference.RevisionRef `json:"required_charter,omitempty"`
	// RequiredAction names the next typed review boundary, not permission to
	// perform it. A plan preview is not an approval or a provisioning receipt.
	RequiredAction string `json:"required_action,omitempty"`
}

func (r *QueueLoopResult) blockedDoer(reason string, schema ...string) {
	priorReason, priorAction := r.Reason, r.RequiredAction
	r.Reason = reason
	r.RequiredAction = ""
	if len(schema) == 0 && priorReason == reason && priorAction != "" {
		r.RequiredAction = priorAction
		return
	}
	if r.Readiness != nil && r.Readiness.Reason == reason {
		r.RequiredAction = r.Readiness.RequiredAction
		return
	}
	switch reason {
	case "doer_host_approval_required", "doer_host_approval_absent", "doer_host_approval_invalid", "doer_host_approval_expired":
		r.RequiredAction = "review_exact_host_write_contract"
		if len(schema) > 0 && schema[0] == loop.DoerReusableSchemaVersion {
			r.RequiredAction = "exact_v5_host_review_adapter_unavailable"
		}
	case "doer_unsafe_workspace":
		r.RequiredAction = "review_exact_workspace_scope"
	case "doer_go_executable_untrusted", "doer_laya_executable_untrusted", "doer_laya_home_untrusted", "local_laya_unavailable":
		r.RequiredAction = "review_controller_dependency_integrity"
	case "doer_prerequisite_unavailable", "doer_runtime_unqualified", "doer_check_cancelled":
		r.RequiredAction = "controller_diagnostic_adapter_unavailable"
	case "doer_controller_unavailable", "doer_runtime_unconfigured":
		r.RequiredAction = "configure_controller_doer_runtime"
	case "provisioning_receipt_missing":
		r.RequiredAction = "plan_preview_exact_charter"
	case "doer_model_required":
		r.RequiredAction = "review_charter_successor_with_usable_model"
	case "doer_agent_credentials_denied", "doer_tool_free_authority_required":
		r.RequiredAction = "review_charter_successor_with_zero_agent_authority"
	case "doer_provider_auth_absent", "doer_provider_auth_invalid", "doer_provider_auth_expired", "doer_provider_auth_unauthorized", "doer_provider_auth_runtime_unqualified":
		r.RequiredAction = "review_controller_provider_authentication"
	case "session_selection_zero_authorized_matches":
		r.RequiredAction = "review_charter_successor_with_matching_authentication"
	}
}

func queueLoopRequestID(subject core.Subject, input QueueLoopInput) string {
	sum := sha256.Sum256([]byte(subject.PrincipalID + "\x00" + input.IdempotencyKey))
	return "loopq-" + hex.EncodeToString(sum[:16])
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

// QueueLoopAs separates serialized admission/authority preparation from bounded
// runtime work. Register worker ownership before releasing admission: an exact
// replay in the binding-to-claim window must read back, not prepare again.
func (s *Service) QueueLoopAs(ctx context.Context, subject core.Subject, input QueueLoopInput) (QueueLoopResult, error) {
	var work *orchestration.WorkRequest
	s.loopRunMu.Lock()
	result, err := s.admitQueueLoop(ctx, subject, input, &work)
	if err == nil && work != nil {
		if s.loopRunWorkers == nil {
			s.loopRunWorkers = make(map[string]bool)
		}
		s.loopRunWorkers[result.QueueItemID] = true
	}
	s.loopRunMu.Unlock()
	if err != nil || work == nil {
		return result, err
	}
	defer func() {
		s.loopRunMu.Lock()
		delete(s.loopRunWorkers, result.QueueItemID)
		s.loopRunMu.Unlock()
	}()
	ctx = context.WithValue(ctx, exactDoerRunKey{}, exactDoerRun{requestID: queueLoopRequestID(subject, input), candidate: input.Loop})
	_, err = s.ProcessQueueItemAs(ctx, subject, *work)
	if err != nil {
		r, readErr := s.queueLoopBlockedReadback(ctx, subject, result, "processing_failed")
		if readErr != nil {
			return r, readErr
		}
		// A durably committed failed attempt is an execution outcome, not an
		// admission-only HTTP denial. Return its authenticated exact readback;
		// never hide an executed task behind a generic 403 or imply success.
		if v := r.Execution; v != nil && v.Disposition != nil && v.Projection.State == queue.StateFailed && v.Disposition.State == "failed" && v.Projection.ActiveClaimID == "" && v.Disposition.QueueItem.ID == v.Item.ItemID && v.Disposition.QueueItem.Digest == v.Item.Digest && v.Disposition.GraphRunID == v.Item.GraphRunID {
			return r, nil
		}
		return r, err
	}
	return s.queueLoopBlockedReadback(ctx, subject, result, "processed")
}

// admitQueueLoop and prepareQueuedLoop run only with loopRunMu held. Neither
// releases it or executes the returned work; the caller owns that boundary.
func (s *Service) admitQueueLoop(ctx context.Context, subject core.Subject, input QueueLoopInput, work **orchestration.WorkRequest) (result QueueLoopResult, err error) {
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
	result.RequestID = key
	input, identity, normalizeErr := normalizeLoopRunIdentity(input)
	if normalizeErr != nil {
		return result, normalizeErr
	}
	result.QueueItemID = key + "-queue"
	lv, e := s.GetLoopViewAs(ctx, subject, input.Loop.ID, input.Loop.Revision)
	if e != nil {
		return result, e
	}
	if lv.Revision.Digest != input.Loop.Digest {
		return result, ErrDenied
	}
	// Bind the exact direct snapshot before preparation, retaining the canonical
	// v5 definition and references. Binding grants no host or runtime authority.
	ctx, e = bindDoerRun(ctx, input, lv.Revision)
	if e != nil {
		return result, e
	}

	if input.Reexecution != nil {
		result.Reexecution, e = s.sealLoopReexecution(ctx, subject, input, key, identity, lv.Revision)
		if e != nil {
			return result, e
		}
	}
	if input.QueueItemID != "" {
		// Explicit recovery selects only this request's exact Queue item.
		// Different keys must not adopt prior execution authority or history.
		if input.QueueItemID != result.QueueItemID {
			return result, fleet.ErrConflict
		}
		var reserved string
		if loadErr := s.Store.Load("queue-loop-intent", key, &reserved); loadErr == nil {
			if reserved != identity {
				return result, fleet.ErrConflict
			}
		} else if !errors.Is(loadErr, os.ErrNotExist) {
			return result, loadErr
		}
		prior, e := s.GetQueueItemAs(ctx, subject, input.QueueItemID)
		if e != nil {
			return result, e
		}
		snapshot, e := s.FleetRepository.GetGraphRunSnapshot(ctx, prior.Item.Snapshot.ID)
		if e != nil {
			return result, e
		}
		topology, e := s.Fleet.ResolveRunTopology(ctx, snapshot)
		if e != nil || len(topology.Nodes) != 1 || topology.Nodes[0].Participant != input.Agent || topology.Nodes[0].Loop != input.Loop {
			return result, fleet.ErrConflict
		}
		var candidate graph.GraphRunSnapshot
		if snapshot.SchemaVersion == graph.DirectRunSnapshotSchemaVersion {
			direct, buildErr := graph.NewDirectLoopSnapshot(snapshot.SnapshotID, topology.Nodes[0], input.Inputs)
			if buildErr != nil {
				return result, fleet.ErrConflict
			}
			candidate, e = graph.NewDirectRunSnapshot(direct)
		} else {
			g, loadErr := s.FleetRepository.GetGraphRevision(ctx, snapshot.Graph.ID, snapshot.Graph.Revision)
			if loadErr != nil {
				return result, loadErr
			}
			candidate, e = graph.NewRunSnapshot(snapshot.SnapshotID, g, input.Inputs)
		}
		if e != nil || candidate.Digest != snapshot.Digest || snapshot.Digest != prior.Item.Snapshot.Digest {
			return result, fleet.ErrConflict
		}
		result.QueueItemID, result.Graph, result.Execution = input.QueueItemID, snapshot.Graph, &prior
		if s.loopRunWorkers[result.QueueItemID] || len(prior.Attempts) > 0 || (!prior.Projection.State.IsPreparation() && prior.Projection.State != queue.StateQueued) {
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
		if lv.Revision.SchemaVersion == loop.DoerRevisionSchemaVersion || lv.Revision.SchemaVersion == loop.DoerReusableSchemaVersion {
			agent, loadErr := s.FleetRepository.GetAgentRevision(ctx, input.Agent.ID, input.Agent.Revision)
			if loadErr != nil || agent.Digest != input.Agent.Digest {
				return result, ErrDenied
			}
			result.RequiredCharter = &agent.Charter
		}
		ctx = context.WithValue(ctx, exactDoerRunKey{}, exactDoerRun{requestID: key, candidate: input.Loop})
		if reason, preflightErr := s.preflightDoerQueueResult(ctx, subject, input, lv, &result); preflightErr != nil {
			return result, preflightErr
		} else if reason != "" {
			result.blockedDoer(reason)
			return s.queueLoopBlockedReadback(ctx, subject, result, reason)
		}
		return s.prepareQueuedLoop(ctx, subject, input, lv, result, "recover-"+hex.EncodeToString(recoveryKey[:16]), work)
	}
	// Rejections are immutable blocked intent, never fabricated executable work.
	if prior, e := s.FleetRepository.GetRejection(ctx, key+"-reject"); e == nil {
		if prior.Reason != "request_sha256:"+identity {
			// A Graph-admission rejection keeps its diagnostic reason. Its
			// exact request identity is sealed in the same durable intent slot.
			var reserved string
			if loadErr := s.Store.Load("queue-loop-intent", key, &reserved); loadErr != nil || reserved != identity {
				return result, fleet.ErrConflict
			}
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
			if s.loopRunWorkers[result.QueueItemID] || len(prior.Attempts) > 0 || (!prior.Projection.State.IsPreparation() && prior.Projection.State != queue.StateQueued) {
				result.Reason = "existing_execution"
				return result, nil
			}
		} else if !errors.Is(e, fleet.ErrNotFound) {
			return result, e
		}
	} else if !errors.Is(historicalErr, fleet.ErrNotFound) {
		return result, historicalErr
	}
	// Historical direct readback, like historical Graph readback, does not
	// reactivate a disabled participant or acquire new execution authority.
	if prior, loadErr := s.GetQueueItemAs(ctx, subject, result.QueueItemID); loadErr == nil {
		var reserved string
		if err := s.Store.Load("queue-loop-intent", key, &reserved); err != nil || reserved != identity {
			return result, fleet.ErrConflict
		}
		result.Execution = &prior
		if s.loopRunWorkers[result.QueueItemID] || len(prior.Attempts) > 0 || (!prior.Projection.State.IsPreparation() && prior.Projection.State != queue.StateQueued) {
			result.Reason = "existing_execution"
			return result, nil
		}
	} else if !errors.Is(loadErr, fleet.ErrNotFound) {
		return result, loadErr
	}
	workspace, e := s.RegisteredAgentWorkspaceAs(ctx, subject, input.Agent.ID)
	if e != nil {
		return result, e
	}
	if workspace.Agent != input.Agent {
		return result, ErrDenied
	}
	// A preflight denial must still seal the exact request to this key. A
	// later approval cannot turn a substituted payload into the original run.
	ctx, e = s.reserveExactDoerRun(ctx, subject, input)
	if errors.Is(e, ErrConflict) {
		return result, fleet.ErrConflict
	}
	if e != nil {
		return result, e
	}
	if prior, loadErr := s.GetQueueItemAs(ctx, subject, result.QueueItemID); loadErr == nil {
		result.Execution = &prior
		if s.loopRunWorkers[result.QueueItemID] || len(prior.Attempts) > 0 || (!prior.Projection.State.IsPreparation() && prior.Projection.State != queue.StateQueued) {
			result.Reason = "existing_execution"
			return result, nil
		}
	} else if !errors.Is(loadErr, fleet.ErrNotFound) {
		return result, loadErr
	}
	ctx = context.WithValue(ctx, exactDoerRunKey{}, exactDoerRun{requestID: key, candidate: input.Loop})
	if lv.Revision.SchemaVersion == loop.DoerRevisionSchemaVersion || lv.Revision.SchemaVersion == loop.DoerReusableSchemaVersion {
		agent, loadErr := s.FleetRepository.GetAgentRevision(ctx, input.Agent.ID, input.Agent.Revision)
		if loadErr != nil || agent.Digest != input.Agent.Digest {
			return result, ErrDenied
		}
		result.RequiredCharter = &agent.Charter
	}
	// Preserve the durable activation-required rejection when activation was
	// not requested. An actual v4 activation or run must preflight before any
	// lifecycle, Graph, Queue, or runtime-preparation mutation.
	if input.Activate || (lv.Lifecycle.State == loop.LifecycleActive && lv.Lifecycle.ActiveDigest == input.Loop.Digest) {
		if reason, preflightErr := s.preflightDoerQueueResult(ctx, subject, input, lv, &result); preflightErr != nil {
			return result, preflightErr
		} else if reason != "" {
			if result.Execution != nil {
				result.blockedDoer(reason)
				return s.queueLoopBlockedReadback(ctx, subject, result, reason)
			}
			result.QueueItemID = ""
			result.blockedDoer(reason)
			return result, nil
		}
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
	// Historical synthetic Graphs are replayed exactly; new intents never
	// publish reusable definitions as an execution side effect.
	var decision orchestration.SubmissionDecision
	if historicalErr == nil {
		decision, e = s.submitGraphAs(ctx, subject, orchestration.SubmitGraphRequest{WorkspaceAgentID: input.Agent.ID, Graph: result.Graph, Inputs: input.Inputs, SubmissionID: key + "-submission", IdempotencyKey: key, SnapshotID: key + "-snapshot", QueueItemID: result.QueueItemID, GraphRunID: key + "-run", TransitionID: key + "-prepare", RejectionID: key + "-reject", MaxAttempts: 1})
	} else {
		decision, e = s.submitLoopAs(ctx, subject, orchestration.SubmitLoopRequest{SubmitGraphRequest: orchestration.SubmitGraphRequest{WorkspaceAgentID: input.Agent.ID, Inputs: input.Inputs, SubmissionID: key + "-submission", IdempotencyKey: key, SnapshotID: key + "-snapshot", QueueItemID: result.QueueItemID, GraphRunID: key + "-run", TransitionID: key + "-prepare", RejectionID: key + "-reject", MaxAttempts: 1}, Participant: input.Agent, Loop: input.Loop, NodeID: "loop-" + identity})
	}

	if e != nil {
		return result, e
	}
	if decision.Accepted == nil {
		if decision.Rejection == nil {
			return result, fleet.ErrConflict
		}
		result.QueueItemID = ""
		result.Rejection = decision.Rejection
		result.Reason = decision.Rejection.ReasonCode
		return result, nil
	}
	return s.prepareQueuedLoop(ctx, subject, input, lv, result, key, work)
}

// preflightDoerQueue reports missing foundational authority before any new
// v4 execution mutation. It neither issues a mandate nor grants host writes;
// runtime and effect admission repeat their independent checks.
func (s *Service) preflightDoerQueue(ctx context.Context, subject core.Subject, input QueueLoopInput, view LoopView) (string, error) {
	return s.preflightDoerQueueResult(ctx, subject, input, view, nil)
}
func (s *Service) preflightDoerQueueResult(ctx context.Context, subject core.Subject, input QueueLoopInput, view LoopView, result *QueueLoopResult) (string, error) {
	if view.Revision.SchemaVersion != loop.DoerRevisionSchemaVersion && view.Revision.SchemaVersion != loop.DoerReusableSchemaVersion {
		return "", nil
	}
	agent, err := s.FleetRepository.GetAgentRevision(ctx, input.Agent.ID, input.Agent.Revision)
	if err != nil {
		return "", err
	}
	if agent.Digest != input.Agent.Digest {
		return "", ErrDenied
	}
	readiness := newDoerReadiness()
	readiness.Subject = DoerPrerequisiteStatus{State: "ready"}
	readiness.AgentReference = DoerPrerequisiteStatus{State: "ready"}
	readiness.CanonicalCandidate = DoerPrerequisiteStatus{State: "ready"}
	readiness.Agent, readiness.Candidate = input.Agent, input.Loop
	if contract, e := queuedDoerContract(ctx, view.Revision); e == nil {
		readiness.ContractDigest, _ = contract.Digest()
	}
	if result != nil {
		result.Readiness = &readiness
		readiness.RequestID = result.RequestID
	}
	if err := s.validateReusableTaskHistory(ctx, subject, input, view.Revision); err != nil {
		readiness.Reason = "same_task_budget_link_required"
		return readiness.Reason, nil
	}
	s.checkDoerExecutionReadiness(ctx, subject, view.Revision, agent, true, &readiness)
	return readiness.Reason, nil
}

func (s *Service) queueLoopBlockedReadback(ctx context.Context, subject core.Subject, result QueueLoopResult, reason string) (QueueLoopResult, error) {
	result.blockedDoer(reason)
	v, err := s.GetQueueItemAs(ctx, subject, result.QueueItemID)
	if err != nil {
		return result, err
	}
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
		v, err = s.GetQueueItemAs(ctx, subject, result.QueueItemID)
	}
	result.Execution = &v
	return result, err
}

func (s *Service) prepareQueuedLoop(ctx context.Context, subject core.Subject, input QueueLoopInput, lv LoopView, result QueueLoopResult, key string, work **orchestration.WorkRequest) (QueueLoopResult, error) {
	ctx = context.WithValue(ctx, exactDoerRunKey{}, exactDoerRun{requestID: queueLoopRequestID(subject, input), candidate: input.Loop})
	readback := func(reason string) (QueueLoopResult, error) {
		result.blockedDoer(reason, lv.Revision.SchemaVersion)
		return s.queueLoopBlockedReadback(ctx, subject, result, reason)
	}
	agent, e := s.FleetRepository.GetAgentRevision(ctx, input.Agent.ID, input.Agent.Revision)
	if e != nil {
		return result, e
	}
	verified, receiptErr := s.hasVerifiedReceipt(agent.Charter.Digest)
	if receiptErr != nil {
		return readback("provisioning_receipt_unavailable")
	}
	ephemeralDoer := s.doerEphemeralPreparationAllowed(ctx, subject, lv.Revision, agent)
	if !verified && !ephemeralDoer {
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
	if e = s.validateQueuedDoerAdmission(ctx, lv.Revision, agent, false); e != nil {
		// Keep the legacy implementation admission contract outside the
		// selected-file Doer diagnostic surface unchanged.
		if lv.Revision.SchemaVersion != loop.DoerRevisionSchemaVersion && lv.Revision.SchemaVersion != loop.DoerReusableSchemaVersion {
			return readback("implementation_prerequisite_required")
		}
		result.blockedDoer(orchestration.DoerPrerequisiteReason(e), lv.Revision.SchemaVersion)
		return readback(result.Reason)
	}
	authority, e := s.fleetCommandAuthorityForAgent(ctx, subject, &agent)
	reservation := sha256.Sum256([]byte(subject.ID + "\x00" + agent.Digest))
	reservationID := hex.EncodeToString(reservation[:])
	reserved := false
	selectedReservation, fence, fenceErr := s.preparationReservation(reservationID)
	if fenceErr == nil {
		reservationID = selectedReservation
		if fence["queue_item_id"] != result.QueueItemID {
			next, nextErr := s.reserveNextLoopRun(ctx, subject, input, result, agent, reservationID, fence)
			if nextErr != nil {
				return readback("prior_run_terminal_authority_retirement_required")
			}
			reservationID, reserved = next, true
			// Never adopt the predecessor's authority under a new Run.
			authority, e = s.fleetCommandAuthorityForAgent(ctx, subject, &agent)
		}
		var prepared reference.DigestRef
		if !reserved && (e != nil || s.Store.Load("queue-session-prepared", reservationID, &prepared) != nil || prepared != authority.Authority) {
			next, proof, recoveryErr := s.recoverDoerSessionPreparation(ctx, subject, input, result, lv, agent, reservationID, fence)
			if recoveryErr != nil {
				return readback("session_preparation_in_progress_or_interrupted")
			}
			reservationID, result.Preparation, reserved = next, proof, true
			authority, e = s.fleetCommandAuthorityForAgent(ctx, subject, &agent)
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
				var retired sessionPreparationFact
				if s.Store.Load("queue-session-preparation-retired", old.ID, &retired) == nil && retired.AuthorityID == old.ID && retired.AgentDigest == agent.Digest && retired.SubjectID == subject.ID && retired.Digest == preparationFactDigest(retired) {
					admission, checkErr := s.AuthorityCommands.AuthorityAdmission(ctx, old.ID, old.Digest, s.Now())
					if checkErr != nil || admission.Admitted {
						return readback("prior_runtime_authority_requires_explicit_recovery")
					}
					continue
				}
				if s.completedProviderProbeAuthority(ctx, subject, agent, old) {
					continue
				}
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
		if !reserved {
			if reserveErr := s.Store.Create("queue-session-preparation", reservationID, map[string]string{"queue_item_id": result.QueueItemID, "agent_digest": agent.Digest}); reserveErr != nil {
				return readback("session_preparation_in_progress_or_interrupted")
			}
		}
		if ephemeralDoer && !verified {
			if !s.doerEphemeralPreparationAllowed(ctx, subject, lv.Revision, agent) {
				return readback("ephemeral_doer_preparation_denied")
			}
			if auditErr := s.audit(ctx, core.AuditEvent{Type: "doer.session.preparation", SubjectID: subject.ID, PrincipalID: subject.PrincipalID, AgentID: agent.AgentID, CharterDigest: charter.Digest, Outcome: "authorized", Reason: "zero_authority_ephemeral_doer", Metadata: map[string]string{"request_id": result.RequestID, "loop_digest": lv.Revision.Digest}}); auditErr != nil {
				return result, auditErr
			}
		}
		mandate, _, previewErr := s.previewSessionAs(ctx, subject, agent.Charter.ID, agent.Charter.Revision, "", core.Environment{Name: "local"}, ephemeralDoer)
		if previewErr != nil {
			return readback("session_mandate_preparation_failed")
		}
		if _, startErr := s.StartSessionAs(ctx, subject, mandate.ID); startErr != nil {
			return s.recordSessionPreparationFailure(ctx, subject, input, result, reservationID, startErr)
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
	_, _, e = s.bindQueueRuntimeAs(ctx, subject, BindQueueRuntimeInput{AgentID: input.Agent.ID, Authority: authority.Authority, QueueItemID: result.QueueItemID, BindingID: bindingID, TransitionID: transitionID})
	if e != nil {
		return readback("exact_runtime_binding_required")
	}
	*work = &orchestration.WorkRequest{Authority: authority.Authority, QueueItemID: result.QueueItemID, WorkerID: "exact-loop-controller", LoopExecutionID: key + "-loop", ClaimID: key + "-claim", AttemptID: key + "-attempt", ClaimTransitionID: key + "-claimed", TerminalTransitionID: key + "-terminal", DispositionID: key + "-disposition", ArtifactID: key + "-artifact", LeaseDuration: 6 * time.Minute}
	return result, nil
}
