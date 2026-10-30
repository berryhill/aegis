package managerplatform

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/manager"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

func read[R any](a *Adapter, name, service string, method func(context.Context, core.Subject) (R, error)) {
	bind(a, name, service, false, nil, func(ctx context.Context, _ empty) (R, error) { return method(ctx, a.subject) })
}
func get[R any](a *Adapter, name, service string, exact bool, method func(context.Context, core.Subject, string, uint64) (R, error)) {
	normalize := requireID
	if exact {
		normalize = exactRevision
	}
	bind(a, name, service, false, normalize, func(ctx context.Context, in object) (R, error) { return method(ctx, a.subject, in.ID, in.Revision) })
}
func getID[R any](a *Adapter, name, service string, method func(context.Context, core.Subject, string) (R, error)) {
	bind(a, name, service, false, requireID, func(ctx context.Context, in object) (R, error) { return method(ctx, a.subject, in.ID) })
}
func mutation[T any, R any](a *Adapter, name, service string, normalize func(context.Context, *T) error, method func(context.Context, core.Subject, T) (R, error)) {
	bind(a, name, service, true, normalize, func(ctx context.Context, in T) (R, error) { return method(ctx, a.subject, in) })
}
func (a *Adapter) register() {
	s := a.service
	if s == nil {
		return
	}
	a.additional()
	a.repairs()
	read(a, "agents.list", "ListFleetAgentsAs", s.ListFleetAgentsAs)
	get(a, "agents.show", "GetFleetAgentAs", false, s.GetFleetAgentAs)
	getID(a, "agents.history", "ListFleetAgentRevisionsAs", s.ListFleetAgentRevisionsAs)
	bind(a, "agents.register", "RegisterFleetAgentAs", true, func(_ context.Context, in *app.RegisterFleetAgentInput) error {
		if len(in.Fixture) == 0 || in.Identity.FleetID == "" || in.Identity.SourceID == "" {
			return errors.New("missing_input: fixture and exact identity.fleet_id/kind/source_id required")
		}
		return nil
	}, func(ctx context.Context, in app.RegisterFleetAgentInput) (any, error) {
		agent, created, err := s.RegisterFleetAgentAs(ctx, a.subject, in)
		return map[string]any{"agent": agent, "created": created}, err
	})
	for _, state := range []string{"enabled", "disabled", "retired"} {
		name := map[string]string{"enabled": "agents.enable", "disabled": "agents.disable", "retired": "agents.retire"}[state]
		bind(a, name, "SetAgentLifecycleAs", true, func(ctx context.Context, in *app.SetAgentLifecycleInput) error {
			if in.Expected.ID == "" {
				return errors.New("missing_input: expected.id required")
			}
			// Resolve current only before confirmation, never at execution time.
			if in.Expected.Revision == 0 && in.Expected.Digest == "" {
				current, err := s.GetFleetAgentAs(ctx, a.subject, in.Expected.ID, 0)
				if err != nil {
					return err
				}
				in.Expected = reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: current.Revision.AgentID, Revision: current.Revision.Revision, Digest: current.Revision.Digest}
			}
			in.Lifecycle = registry.Lifecycle(state)
			return in.Expected.Validate()
		}, func(ctx context.Context, in app.SetAgentLifecycleInput) (app.FleetAgent, error) {
			return s.SetAgentLifecycleAs(ctx, a.subject, in.Expected.ID, in)
		})
	}
	read(a, "loops.list", "ListLoopsAs", s.ListLoopsAs)
	read(a, "loops.inventory", "ListLoopInventoryAs", s.ListLoopInventoryAs)
	get(a, "loops.show", "GetLoopViewAs", true, s.GetLoopViewAs)
	getID(a, "loops.availability", "GetLoopAvailabilityAs", s.GetLoopAvailabilityAs)
	normalizeLoop := func(_ context.Context, in *app.PublishLoopInput) error {
		if in.AgentID == "" || in.Revision.LoopID == "" || in.Revision.Revision == 0 {
			return errors.New("missing_input: agent_id and canonical revision required")
		}
		if in.IdempotencyKey == "" {
			in.IdempotencyKey = newID()
		}
		return nil
	}
	bind(a, "loops.validate", "ValidateLoopAs", false, normalizeLoop, func(ctx context.Context, in app.PublishLoopInput) (app.ValidatedLoop, error) {
		return s.ValidateLoopAs(ctx, a.subject, in)
	})
	mutation(a, "loops.publish", "PublishLoopAs", normalizeLoop, s.PublishLoopAs)
	for _, state := range []loop.LifecycleState{loop.LifecycleActive, loop.LifecycleRetired} {
		name := "loops.activate"
		if state == loop.LifecycleRetired {
			name = "loops.retire"
		}
		bind(a, name, "SetLoopLifecycleAs", true, func(_ context.Context, in *app.SetLoopLifecycleInput) error {
			if in.AgentID == "" {
				return errors.New("missing_input: agent_id required")
			}
			if err := in.Loop.Validate(); err != nil {
				return err
			}
			in.State = state
			if in.EventID == "" {
				in.EventID = newID()
			}
			return nil
		}, func(ctx context.Context, in app.SetLoopLifecycleInput) (app.LoopLifecycleResult, error) {
			return s.SetLoopLifecycleAs(ctx, a.subject, in.Loop.ID, in)
		})
	}
	for _, state := range []loop.Availability{loop.AvailabilityArchived, loop.AvailabilityPublished} {
		name := "loops.archive"
		if state == loop.AvailabilityPublished {
			name = "loops.restore"
		}
		bind(a, name, "SetLoopAvailabilityAs", true, func(_ context.Context, in *app.SetLoopAvailabilityInput) error {
			if in.AgentID == "" {
				return errors.New("missing_input: agent_id required")
			}
			if err := in.Head.Validate(); err != nil {
				return err
			}
			in.State = state
			if in.EventID == "" {
				in.EventID = newID()
			}
			return nil
		}, func(ctx context.Context, in app.SetLoopAvailabilityInput) (app.LoopAvailabilityResult, error) {
			return s.SetLoopAvailabilityAs(ctx, a.subject, in.Head.ID, in)
		})
	}
	read(a, "graphs.list", "ListGraphsAs", s.ListGraphsAs)
	get(a, "graphs.show", "GetGraphAs", true, s.GetGraphAs)
	getID(a, "graphs.lifecycle", "GetGraphLifecycleAs", s.GetGraphLifecycleAs)
	mutation(a, "graphs.publish", "PublishGraphAs", func(_ context.Context, in *app.PublishGraphInput) error {
		if in.AgentID == "" || in.Revision.GraphID == "" || in.Revision.Revision == 0 {
			return errors.New("missing_input: agent_id and canonical revision required")
		}
		if in.IdempotencyKey == "" {
			in.IdempotencyKey = newID()
		}
		return nil
	}, s.PublishGraphAs)
	mutation(a, "graphs.submit", "SubmitGraphAs", func(_ context.Context, in *app.SubmitGraphInput) error {
		if in.WorkspaceAgentID == "" {
			return errors.New("missing_input: agent_id required (workspace selector, not authority)")
		}
		if err := in.Graph.Validate(); err != nil {
			return err
		}
		if in.IdempotencyKey == "" {
			in.IdempotencyKey = newID()
		}
		if in.SubmissionID == "" {
			in.SubmissionID = stableSubmissionID(a.subject.PrincipalID, in.IdempotencyKey, "SubmissionID")
		}
		if in.SnapshotID == "" {
			in.SnapshotID = stableSubmissionID(a.subject.PrincipalID, in.IdempotencyKey, "SnapshotID")
		}
		if in.QueueItemID == "" {
			in.QueueItemID = stableSubmissionID(a.subject.PrincipalID, in.IdempotencyKey, "QueueItemID")
		}
		if in.GraphRunID == "" {
			in.GraphRunID = stableSubmissionID(a.subject.PrincipalID, in.IdempotencyKey, "GraphRunID")
		}
		if in.TransitionID == "" {
			in.TransitionID = stableSubmissionID(a.subject.PrincipalID, in.IdempotencyKey, "TransitionID")
		}
		if in.RejectionID == "" {
			in.RejectionID = stableSubmissionID(a.subject.PrincipalID, in.IdempotencyKey, "RejectionID")
		}
		if in.MaxAttempts == 0 {
			return errors.New("missing_input: bounded positive max_attempts required")
		}
		return nil
	}, s.SubmitGraphAs)
	read(a, "submissions.list", "ListSubmissionHistoryAs", s.ListSubmissionHistoryAs)
	read(a, "queue.list", "ListExecutableQueueAs", s.ListExecutableQueueAs)
	read(a, "queue.preparations", "ListPreparationsAs", s.ListPreparationsAs)
	getID(a, "queue.show", "GetQueueItemAs", s.GetQueueItemAs)
	getID(a, "queue.result", "GetQueueResultAs", s.GetQueueResultAs)
	mutation(a, "loops.queue", "QueueLoopAs", func(_ context.Context, in *app.QueueLoopInput) error {
		if err := in.Agent.Validate(); err != nil {
			return err
		}
		if err := in.Loop.Validate(); err != nil {
			return err
		}
		if in.IdempotencyKey == "" {
			in.IdempotencyKey = newID()
		}
		return nil
	}, s.QueueLoopAs)
	for _, name := range []string{"queue.cancel", "queue.revoke", "queue.expire", "queue.exhaust"} {
		method := s.CancelQueueItemAs
		reason := orchestration.ReasonOperatorCancelled
		switch name {
		case "queue.revoke":
			method = s.RevokeQueueItemAs
			reason = orchestration.ReasonAuthorityRevoked
		case "queue.expire":
			method = s.ExpireQueueItemAs
			reason = orchestration.ReasonExecutionExpired
		case "queue.exhaust":
			method = s.ExhaustQueueItemAs
			reason = orchestration.ReasonRetryExhausted
		}
		mutation(a, name, "QueueWorker terminal application service", func(ctx context.Context, in *app.TerminalQueueItemInput) error {
			if in.QueueItemID == "" {
				return errors.New("missing_input: queue_item_id required")
			}
			if in.WorkspaceAgentID == "" {
				authority, err := s.FleetCommandAuthorityAs(ctx, a.subject)
				if err != nil {
					return err
				}
				in.Authority = authority.Authority
			}
			in.ReasonCode = reason
			if in.CancellationID == "" {
				in.CancellationID = newID()
			}
			if in.TransitionID == "" {
				in.TransitionID = newID()
			}
			return nil
		}, method)
	}
	mutation(a, "queue.retry", "RetryQueueItemAs (expired-lease reclaim only)", func(ctx context.Context, in *app.RetryQueueItemInput) error {
		if in.QueueItemID == "" {
			return errors.New("missing_input: queue_item_id required")
		}
		if !in.Reclaimed {
			return errors.New("ordinary_retry_unsupported: durable runtime-stop acknowledgement unavailable")
		}
		in.ReasonCode = orchestration.ReasonLeaseReclaimed
		if in.WorkspaceAgentID == "" {
			auth, err := s.FleetCommandAuthorityAs(ctx, a.subject)
			if err != nil {
				return err
			}
			in.Authority = auth.Authority
		}
		if in.RetryID == "" {
			in.RetryID = newID()
		}
		if in.TransitionID == "" {
			in.TransitionID = newID()
		}
		return nil
	}, s.RetryQueueItemAs)
	read(a, "queue.preferences", "GetQueuePreferencesAs", s.GetQueuePreferencesAs)
	mutation(a, "queue.preferences.set", "SetQueuePreferencesAs", nil, func(ctx context.Context, sub core.Subject, in app.SetQueuePreferencesInput) (any, error) {
		value, created, err := s.SetQueuePreferencesAs(ctx, sub, in)
		return map[string]any{"preferences": value, "created": created}, err
	})
	getID(a, "loops.task.show", "ReadDoerDraftAs", s.ReadDoerDraftAs)
	mutation(a, "loops.task.save", "SaveDoerDraftAs", nil, s.SaveDoerDraftAs)
	mutation(a, "loops.task.execute", "ExecuteDoerTaskAs", nil, s.ExecuteDoerTaskAs)
	read(a, "controller-codex.show", "ReadControllerCodexAs", s.ReadControllerCodexAs)
	bind(a, "audit.verify", "VerifyAuditAs", false, nil, func(_ context.Context, _ empty) (any, error) {
		err := s.VerifyAuditAs(a.subject)
		return map[string]any{"verified": err == nil}, err
	})
	bind(a, "audit.query", "AuditEventsAs", false, nil, func(_ context.Context, _ empty) ([]core.AuditEvent, error) { return s.AuditEventsAs(a.subject) })
}

// Stable keys preserve generated durable identities across new conversations.
// Semantic payload changes remain conflicts in the owning admission service.
func stableSubmissionID(principal, key, field string) string {
	sum := sha256.Sum256([]byte("manager.graphs.submit.v1\x00" + principal + "\x00" + key + "\x00" + field))
	return fmt.Sprintf("%x", sum[:16])
}

// Compile-time anchors document the supported canonical result domains.
var _ graph.GraphRevision
var _ queue.Item
var _ manager.Platform = (*Adapter)(nil)
