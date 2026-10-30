package managerplatform

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"time"
)

type registrationPreview struct {
	Charter  json.RawMessage `json:"charter"`
	Fixture  json.RawMessage `json:"fixture"`
	FleetID  string          `json:"fleet_id"`
	SourceID string          `json:"source_id"`
}
type approvalRequest struct {
	ID         string `json:"id"`
	TTLSeconds uint32 `json:"ttl_seconds"`
}
type sessionEnd struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}
type auditDelivery struct {
	Limit int `json:"limit"`
}

func (a *Adapter) repairs() {
	s := a.service
	getID(a, "contribution.execution.status", "ContributionExecutionStatusAs", s.ContributionExecutionStatusAs)
	getID(a, "contribution.execution.show", "ReadContributionExecutionAs", s.ReadContributionExecutionAs)
	getID(a, "contribution.control.show", "ReadContributionControlAs", s.ReadContributionControlAs)
	getID(a, "contribution.final", "GetFinalContributionResultAs", s.GetFinalContributionResultAs)
	getID(a, "contribution.provenance", "ReadContributionRunProvenanceAs", s.ReadContributionRunProvenanceAs)
	read(a, "fleet.surface", "FleetSurfaceAs", s.FleetSurfaceAs)
	get(a, "loops.definition", "GetLoopAs", true, s.GetLoopAs)
	bind(a, "loops.run.readiness", "ReadDoerReadinessAs", false, nil, func(ctx context.Context, in app.DoerReadinessInput) (app.DoerCandidateReadiness, error) {
		return s.ReadDoerReadinessAs(ctx, a.subject, in)
	})
	bind(a, "contribution.readiness", "ReadContributionReadinessAs", false, nil, func(ctx context.Context, in app.ContributionReadinessInput) (app.ContributionReadiness, error) {
		return s.ReadContributionReadinessAs(ctx, a.subject, in)
	})
	bind(a, "agents.prepare", "PrepareFleetAgentRegistrationAs", false, nil, func(ctx context.Context, in registrationPreview) (app.AgentRegistrationProposal, error) {
		return s.PrepareFleetAgentRegistrationAs(ctx, a.subject, in.Charter, in.Fixture, in.FleetID, in.SourceID)
	})
	bind(a, "approval.request", "RequestApprovalAs", true, func(_ context.Context, in *approvalRequest) error {
		if in.ID == "" || in.TTLSeconds == 0 || in.TTLSeconds > 3600 {
			return errors.New("missing_input: exact plan id and ttl_seconds 1..3600 required")
		}
		return nil
	}, func(ctx context.Context, in approvalRequest) (core.Approval, error) {
		return s.RequestApprovalAs(ctx, a.subject, in.ID, time.Duration(in.TTLSeconds)*time.Second)
	})
	bind(a, "runtime.discover", "Runtime", false, nil, func(ctx context.Context, _ empty) (core.RuntimeDescriptor, error) { return s.Runtime(ctx) })
	bind(a, "audit.delivery.status", "AuditDeliveryStatusAs", false, nil, func(_ context.Context, _ empty) (core.AuditDeliveryStatus, error) {
		return s.AuditDeliveryStatusAs(a.subject)
	})
	bind(a, "audit.projection.verify", "VerifyAuditProjectionAs", false, nil, func(_ context.Context, _ empty) (any, error) {
		err := s.VerifyAuditProjectionAs(a.subject)
		return map[string]bool{"verified": err == nil}, err
	})
	bind(a, "audit.projection.rebuild", "RebuildAuditProjectionAs", true, nil, func(ctx context.Context, _ empty) (core.AuditDeliveryStatus, error) {
		return s.RebuildAuditProjectionAs(ctx, a.subject)
	})
	bind(a, "audit.deliver", "DeliverAuditAs", true, func(_ context.Context, in *auditDelivery) error {
		if in.Limit < 1 || in.Limit > 100 {
			return errors.New("limit must be 1..100")
		}
		return nil
	}, func(ctx context.Context, in auditDelivery) (core.AuditDeliveryResult, error) {
		return s.DeliverAuditAs(ctx, a.subject, in.Limit)
	})
	for _, kind := range []string{"revoke", "terminate"} {
		bind(a, "session."+kind, "session exact lifecycle: "+kind, true, func(_ context.Context, in *sessionEnd) error {
			if in.ID == "" || in.Reason == "" {
				return errors.New("missing_input: exact session id and reason required")
			}
			_, err := s.GetSession(in.ID)
			return err
		}, func(ctx context.Context, in sessionEnd) (any, error) {
			var err error
			if kind == "revoke" {
				err = s.RevokeSessionAs(ctx, a.subject, in.ID, in.Reason)
			} else {
				err = s.TerminateSessionAs(ctx, a.subject, in.ID, in.Reason)
			}
			if err != nil {
				return nil, err
			}
			return s.GetSession(in.ID)
		})
	}
}
