package managerplatform

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/reference"
)

type charterInput struct {
	Charter json.RawMessage `json:"charter"`
}
type planInput struct {
	ID          string           `json:"id"`
	Revision    uint64           `json:"revision"`
	Environment core.Environment `json:"environment"`
}
type debuggerInput struct {
	ID     string `json:"id"`
	Cursor string `json:"cursor,omitempty"`
}

func (a *Adapter) additional() {
	s := a.service
	// Read-only service methods without As variants remain behind the same fresh
	// authenticated adapter. No alternate store, credential extraction or shell.
	bind(a, "charter.list", "ListAgents", false, nil, func(_ context.Context, _ empty) ([]string, error) { return s.ListAgents() })
	bind(a, "charter.show", "GetCharter", false, exactRevision, func(_ context.Context, in object) (core.CanonicalCharter, error) {
		return s.GetCharter(in.ID, in.Revision)
	})
	bind(a, "charter.history", "ListCharters", false, requireID, func(_ context.Context, in object) ([]core.CanonicalCharter, error) { return s.ListCharters(in.ID) })
	normalize := func(_ context.Context, in *charterInput) error {
		if len(in.Charter) == 0 {
			return errors.New("missing_input: charter object required")
		}
		return nil
	}
	bind(a, "charter.validate", "ValidateCharterAs", false, normalize, func(ctx context.Context, in charterInput) (core.CanonicalCharter, error) {
		return s.ValidateCharterAs(ctx, a.subject, in.Charter)
	})
	bind(a, "charter.import", "ImportCharterAs", true, normalize, func(ctx context.Context, in charterInput) (core.CanonicalCharter, error) {
		return s.ImportCharterAs(ctx, a.subject, in.Charter)
	})
	bind(a, "plan.preview", "PreviewPlanAs", true, func(_ context.Context, in *planInput) error {
		if in.ID == "" || in.Revision == 0 {
			return errors.New("missing_input: exact charter id/revision and environment required")
		}
		return nil
	}, func(ctx context.Context, in planInput) (core.Review, error) {
		return s.PreviewPlanAs(ctx, a.subject, in.ID, in.Revision, in.Environment)
	})
	bind(a, "plan.list", "ListPlans", false, nil, func(_ context.Context, _ empty) ([]core.Plan, error) { return s.ListPlans() })
	bind(a, "plan.show", "GetPlan", false, requireID, func(_ context.Context, in object) (core.Plan, error) { return s.GetPlan(in.ID) })
	bind(a, "approval.list", "ListApprovals", false, nil, func(_ context.Context, _ empty) ([]core.Approval, error) { return s.ListApprovals() })
	bind(a, "approval.show", "GetApproval", false, requireID, func(_ context.Context, in object) (core.Approval, error) { return s.GetApproval(in.ID) })
	bind(a, "provision.receipts", "ListReceipts", false, nil, func(_ context.Context, _ empty) ([]core.Receipt, error) { return s.ListReceipts() })
	bind(a, "provision.receipt", "GetReceipt", false, requireID, func(_ context.Context, in object) (core.Receipt, error) { return s.GetReceipt(in.ID) })
	bind(a, "session.list", "ListSessions", false, nil, func(_ context.Context, _ empty) ([]core.Session, error) { return s.ListSessions() })
	bind(a, "session.show", "GetSession", false, requireID, func(_ context.Context, in object) (core.Session, error) { return s.GetSession(in.ID) })
	bind(a, "session.mandate", "GetMandate", false, requireID, func(_ context.Context, in object) (core.Mandate, error) { return s.GetMandate(in.ID) })
	bind(a, "queue.debugger", "ExecutionDebuggerAs", false, func(_ context.Context, in *debuggerInput) error {
		if in.ID == "" {
			return errors.New("missing_input: id required")
		}
		return nil
	}, func(ctx context.Context, in debuggerInput) (app.ExecutionDebugger, error) {
		return s.ExecutionDebuggerAs(ctx, a.subject, in.ID, in.Cursor)
	})
	bind(a, "queue.archive.status", "GetQueueArchiveAs", false, func(_ context.Context, in *reference.DigestRef) error { return in.Validate() }, func(ctx context.Context, in reference.DigestRef) (any, error) {
		return s.GetQueueArchiveAs(ctx, a.subject, in)
	})
	bind(a, "queue.archive.set", "SetQueueArchiveAs", true, func(_ context.Context, in *app.SetQueueArchiveInput) error {
		if err := in.QueueItem.Validate(); err != nil {
			return err
		}
		if in.RequestID == "" {
			in.RequestID = newID()
		}
		return nil
	}, func(ctx context.Context, in app.SetQueueArchiveInput) (any, error) {
		result, created, err := s.SetQueueArchiveAs(ctx, a.subject, in)
		return map[string]any{"archive": result, "created": created}, err
	})
	mutation(a, "queue.requeue", "RequeueQueueItemAs", func(_ context.Context, in *app.RequeueQueueItemInput) error {
		if err := in.QueueItem.Validate(); err != nil {
			return err
		}
		if in.IdempotencyKey == "" {
			in.IdempotencyKey = newID()
		}
		return nil
	}, s.RequeueQueueItemAs)
	bind(a, "loops.readiness", "ReadDoerCandidateReadinessAs", false, nil, func(ctx context.Context, in app.DoerCandidateReadinessInput) (app.DoerCandidateReadiness, error) {
		return s.ReadDoerCandidateReadinessAs(ctx, a.subject, in)
	})
	getID(a, "contribution.result", "GetContributionResultAs", s.GetContributionResultAs)
	getID(a, "contribution.continuity", "ReadContributionContinuityViewAs", s.ReadContributionContinuityViewAs)
	getID(a, "contribution.evidence", "ReadContributionCandidateEvidenceAs", s.ReadContributionCandidateEvidenceAs)
	getID(a, "contribution.delivery", "ReadContributionDeliveryHistoryAs", s.ReadContributionDeliveryHistoryAs)
}
